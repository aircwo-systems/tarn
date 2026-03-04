exports.handler = async (event) => {
  const campaignId = event?.pathParameters?.campaignId || 'unknown';
  const secretName = process.env.SHARED_SECRET_NAME || 'unset';
  const bucketName = process.env.ARTIFACT_BUCKET || 'unset';

  console.log(
    JSON.stringify({
      type: 'campaign-status-check',
      campaignId,
      secretName,
      bucketName,
      requestId: event?.requestContext?.requestId || 'n/a'
    })
  );

  return {
    statusCode: 200,
    headers: {
      'content-type': 'application/json'
    },
    body: JSON.stringify({
      campaignId,
      state: 'queued-or-processing',
      storage: bucketName,
      configSecret: secretName
    })
  };
};
