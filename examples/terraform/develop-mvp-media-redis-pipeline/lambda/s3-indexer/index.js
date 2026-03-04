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

function extractImageID(key) {
  if (!key) return '';
  const parts = key.split('/').filter(Boolean);
  if (parts.length === 0) return '';

  if (parts[0] === 'uploads' && parts.length >= 2) {
    const filename = parts[1];
    const lastDot = filename.lastIndexOf('.');
    return lastDot > 0 ? filename.slice(0, lastDot) : filename;
  }

  if (parts[0] === 'images' && parts.length >= 2) {
    return parts[1];
  }

  return '';
}

exports.handler = async (event) => {
  const records = Array.isArray(event?.Records) ? event.Records : [];
  const namespace = process.env.CACHE_NAMESPACE || 'image:asset';
  const secretName = process.env.SHARED_SECRET_NAME || 'unset';
  const indexed = [];

  for (const record of records) {
    const bucket = record?.s3?.bucket?.name || 'unknown';
    const rawKey = record?.s3?.object?.key || 'unknown';
    const key = decodeURIComponent(String(rawKey).replace(/\+/g, ' '));
    const imageId = extractImageID(key);

    if (!imageId) {
      console.log(
        JSON.stringify({
          type: 'image-index-skip',
          reason: 'unsupported-key',
          bucket,
          key
        })
      );
      continue;
    }

    await redisCommand(process.env.REDIS_URL, [
      'SET',
      `${namespace}:${imageId}:indexed`,
      JSON.stringify({
        indexed: true,
        imageId,
        lastObjectKey: key,
        updatedAt: new Date().toISOString()
      }),
      'EX',
      '3600'
    ]);

    indexed.push(imageId);
    console.log(
      JSON.stringify({
        type: 'image-indexed',
        imageId,
        bucket,
        key,
        eventName: record.eventName,
        secretName
      })
    );
  }

  return {
    observed: records.length,
    indexed
  };
};
