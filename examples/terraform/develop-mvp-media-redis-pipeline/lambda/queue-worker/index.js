const http = require('http');
const https = require('https');
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

function normalizeOpenStackEndpoint(raw) {
  const fallback = 'http://host.docker.internal:4566';
  if (!raw) return fallback;

  try {
    const parsed = new URL(raw);
    if (parsed.hostname === 'localhost' || parsed.hostname === '127.0.0.1' || parsed.hostname === '0.0.0.0') {
      parsed.hostname = 'host.docker.internal';
    }
    return parsed.toString().replace(/\/$/, '');
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

function putObject(endpoint, bucket, key, body, contentType) {
  const encodedKey = key
    .split('/')
    .map((part) => encodeURIComponent(part))
    .join('/');
  const target = new URL(`/_s3/${bucket}/${encodedKey}`, endpoint);
  const client = target.protocol === 'https:' ? https : http;
  const payload = Buffer.isBuffer(body) ? body : Buffer.from(String(body), 'utf8');

  return new Promise((resolve, reject) => {
    const req = client.request(
      target,
      {
        method: 'PUT',
        headers: {
          'content-type': contentType,
          'content-length': payload.length
        }
      },
      (res) => {
        const chunks = [];
        res.on('data', (chunk) => chunks.push(chunk));
        res.on('end', () => {
          if ((res.statusCode || 500) >= 400) {
            reject(new Error(`s3 put failed: ${res.statusCode} ${Buffer.concat(chunks).toString('utf8')}`));
            return;
          }
          resolve();
        });
      }
    );

    req.on('error', reject);
    req.write(payload);
    req.end();
  });
}

function parseImageMessage(record) {
  let parsed = {};
  try {
    parsed = JSON.parse(record?.body || '{}');
  } catch {
    parsed = { rawBody: record?.body || '' };
  }

  const fallback = (record?.messageId || 'image').slice(0, 10);
  const imageId = String(parsed.imageId || parsed.assetId || fallback);
  const sourceKey = parsed.sourceKey || `uploads/${imageId}.svg`;
  const title = parsed.title || `Image ${imageId}`;
  return { imageId, sourceKey, title, requestedBy: parsed.requestedBy || 'unknown' };
}

function buildRenditionSvg(imageId, title, width, height, accent) {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}"><rect width="${width}" height="${height}" fill="${accent}"/><text x="14" y="${Math.floor(height / 2)}" font-size="16" fill="white" font-family="Arial">${imageId}</text><text x="14" y="${Math.floor(height / 2) + 24}" font-size="12" fill="white" font-family="Arial">${title}</text></svg>`;
}

exports.handler = async (event) => {
  const records = Array.isArray(event?.Records) ? event.Records : [];
  const namespace = process.env.CACHE_NAMESPACE || 'image:asset';
  const bucketName = process.env.ARTIFACT_BUCKET || 'unset';
  const endpoint = normalizeOpenStackEndpoint(process.env.OPENSTACK_ENDPOINT);
  const secretName = process.env.SHARED_SECRET_NAME || 'unset';

  const processed = [];

  for (const record of records) {
    const { imageId, sourceKey, title, requestedBy } = parseImageMessage(record);
    const now = new Date().toISOString();
    const webKey = `images/${imageId}/renditions/web.svg`;
    const thumbKey = `images/${imageId}/renditions/thumb.svg`;
    const manifestKey = `images/${imageId}/manifest.json`;

    const webSvg = buildRenditionSvg(imageId, title, 1280, 720, '#0f766e');
    const thumbSvg = buildRenditionSvg(imageId, title, 320, 180, '#1d4ed8');
    const manifest = {
      imageId,
      state: 'processed',
      sourceKey,
      renditions: {
        web: webKey,
        thumb: thumbKey
      },
      requestedBy,
      processedAt: now
    };

    await putObject(endpoint, bucketName, webKey, webSvg, 'image/svg+xml');
    await putObject(endpoint, bucketName, thumbKey, thumbSvg, 'image/svg+xml');
    await putObject(endpoint, bucketName, manifestKey, JSON.stringify(manifest), 'application/json');

    await redisCommand(process.env.REDIS_URL, [
      'SET',
      `${namespace}:${imageId}:status`,
      JSON.stringify(manifest),
      'EX',
      '3600'
    ]);
    await redisCommand(process.env.REDIS_URL, [
      'SET',
      `${namespace}:${imageId}:manifest`,
      manifestKey,
      'EX',
      '3600'
    ]);

    processed.push(imageId);
    console.log(
      JSON.stringify({
        type: 'image-job-processed',
        imageId,
        sourceKey,
        manifestKey,
        messageId: record.messageId,
        secretName
      })
    );
  }

  return {
    processed: processed.length,
    imageIds: processed,
    bucketName
  };
};
