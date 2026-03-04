exports.handler = async (event) => {
  const records = Array.isArray(event?.Records) ? event.Records : [];
  const secretName = process.env.SHARED_SECRET_NAME || 'unset';

  for (const record of records) {
    const s3 = record?.s3 || {};
    const bucket = s3?.bucket?.name || 'unknown';
    const key = s3?.object?.key || 'unknown';

    console.log(
      JSON.stringify({
        type: 'artifact-created',
        bucket,
        key,
        eventName: record.eventName,
        secretName
      })
    );
  }

  return {
    observed: records.length,
    secretName
  };
};
