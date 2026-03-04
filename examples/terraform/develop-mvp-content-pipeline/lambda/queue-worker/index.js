exports.handler = async (event) => {
  const records = Array.isArray(event?.Records) ? event.Records : [];
  const secretName = process.env.SHARED_SECRET_NAME || 'unset';
  const bucketName = process.env.ARTIFACT_BUCKET || 'unset';

  for (const record of records) {
    console.log(
      JSON.stringify({
        type: 'campaign-job',
        messageId: record.messageId,
        body: record.body,
        source: record.eventSource,
        secretName,
        bucketName
      })
    );
  }

  return {
    processed: records.length,
    secretName,
    bucketName
  };
};
