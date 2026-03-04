const net = require('net');

function redisTarget(urlValue) {
  const fallback = { host: 'host.docker.internal', port: 6379 };
  if (!urlValue) return fallback;

  try {
    const parsed = new URL(urlValue);
    return {
      host: parsed.hostname || fallback.host,
      port: Number(parsed.port) || fallback.port
    };
  } catch {
    return fallback;
  }
}

function encodeResp(args) {
  let out = `*${args.length}\r\n`;
  for (const arg of args) {
    const str = String(arg);
    out += `$${Buffer.byteLength(str)}\r\n${str}\r\n`;
  }
  return out;
}

function parseResp(input) {
  const frame = parseFrame(input, 0);
  return frame.value;
}

function parseFrame(input, start) {
  if (start >= input.length) {
    throw new Error('incomplete frame');
  }

  const type = input[start];
  const lineEnd = input.indexOf('\r\n', start);
  if (lineEnd < 0) {
    throw new Error('incomplete line');
  }

  if (type === '+') {
    return { value: input.slice(start + 1, lineEnd), next: lineEnd + 2 };
  }
  if (type === '-') {
    throw new Error(input.slice(start + 1, lineEnd));
  }
  if (type === ':') {
    return { value: Number(input.slice(start + 1, lineEnd)), next: lineEnd + 2 };
  }
  if (type === '$') {
    const len = Number(input.slice(start + 1, lineEnd));
    if (len < 0) {
      return { value: null, next: lineEnd + 2 };
    }
    const payloadStart = lineEnd + 2;
    const payloadEnd = payloadStart + len;
    if (payloadEnd + 2 > input.length) {
      throw new Error('incomplete bulk string');
    }
    return {
      value: input.slice(payloadStart, payloadEnd),
      next: payloadEnd + 2
    };
  }
  if (type === '*') {
    const count = Number(input.slice(start + 1, lineEnd));
    if (count < 0) {
      return { value: null, next: lineEnd + 2 };
    }
    const items = [];
    let cursor = lineEnd + 2;
    for (let i = 0; i < count; i += 1) {
      const parsed = parseFrame(input, cursor);
      items.push(parsed.value);
      cursor = parsed.next;
    }
    return { value: items, next: cursor };
  }

  throw new Error(`unsupported RESP frame: ${type}`);
}

async function redisCommand(redisUrl, args) {
  const { host, port } = redisTarget(redisUrl);
  return new Promise((resolve, reject) => {
    const socket = net.createConnection({ host, port });
    const timeout = setTimeout(() => {
      socket.destroy(new Error('redis command timeout'));
    }, 1500);
    let buffer = '';
    let settled = false;

    socket.on('connect', () => {
      socket.write(encodeResp(args));
    });

    socket.on('data', (chunk) => {
      if (settled) return;
      buffer += chunk.toString('utf8');
      try {
        const parsed = parseResp(buffer);
        settled = true;
        clearTimeout(timeout);
        socket.end();
        resolve(parsed);
      } catch {
        // Wait for more data.
      }
    });

    socket.on('error', (err) => {
      if (settled) return;
      settled = true;
      clearTimeout(timeout);
      reject(err);
    });

    socket.on('close', () => {
      if (settled) return;
      settled = true;
      clearTimeout(timeout);
      reject(new Error('redis connection closed before response'));
    });
  });
}

exports.handler = async (event) => {
  const imageId = event?.pathParameters?.imageId || 'unknown';
  const namespace = process.env.CACHE_NAMESPACE || 'image:asset';
  const secretName = process.env.SHARED_SECRET_NAME || 'unset';
  const bucketName = process.env.ARTIFACT_BUCKET || 'unset';

  const statusKey = `${namespace}:${imageId}:status`;
  const indexedKey = `${namespace}:${imageId}:indexed`;
  const manifestKeyRef = `${namespace}:${imageId}:manifest`;

  let status = null;
  let indexed = null;
  let manifestKey = null;
  let redisError = null;

  try {
    const [statusRaw, indexedRaw, manifestRaw] = await Promise.all([
      redisCommand(process.env.REDIS_URL, ['GET', statusKey]),
      redisCommand(process.env.REDIS_URL, ['GET', indexedKey]),
      redisCommand(process.env.REDIS_URL, ['GET', manifestKeyRef])
    ]);

    if (statusRaw) {
      try {
        status = JSON.parse(statusRaw);
      } catch {
        status = { raw: statusRaw };
      }
    }
    if (indexedRaw) {
      try {
        indexed = JSON.parse(indexedRaw);
      } catch {
        indexed = indexedRaw;
      }
    }
    if (manifestRaw) {
      manifestKey = manifestRaw;
    }
  } catch (err) {
    redisError = err instanceof Error ? err.message : String(err);
  }

  console.log(
    JSON.stringify({
      type: 'image-status',
      imageId,
      statusKey,
      indexedKey,
      redisError
    })
  );

  return {
    statusCode: 200,
    headers: {
      'content-type': 'application/json'
    },
    body: JSON.stringify({
      imageId,
      status: status || { state: 'pending' },
      indexed: indexed || { indexed: false },
      manifestKey,
      storage: bucketName,
      configSecret: secretName,
      redisError
    })
  };
};
