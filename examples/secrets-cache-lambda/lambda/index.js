const http = require('http');

function fetchSecret(secretId) {
  return new Promise((resolve, reject) => {
    const req = http.request(
      {
        hostname: 'localhost',
        port: 2773,
        path: `/secretsmanager/get?secretId=${encodeURIComponent(secretId)}`,
        method: 'GET',
        headers: {
          'X-Aws-Parameters-Secrets-Token': process.env.AWS_SESSION_TOKEN || ''
        }
      },
      (res) => {
        let data = '';
        res.setEncoding('utf8');
        res.on('data', (chunk) => {
          data += chunk;
        });
        res.on('end', () => {
          if (res.statusCode && res.statusCode >= 400) {
            reject(new Error(`secrets extension error (${res.statusCode}): ${data}`));
            return;
          }

          try {
            resolve(JSON.parse(data));
          } catch {
            reject(new Error(`invalid JSON from secrets extension: ${data}`));
          }
        });
      }
    );

    req.on('error', reject);
    req.end();
  });
}

exports.handler = async (event = {}) => {
  const secretId = event.secretId || 'test';
  const result = await fetchSecret(secretId);
  const value = result.SecretString ?? '';

  console.log(`[cache-extension-lambda-test] fetched secret "${secretId}": ${value}`);

  return {
    function: 'cache-extension-lambda-test',
    secretId,
    secretValue: value
  };
};
