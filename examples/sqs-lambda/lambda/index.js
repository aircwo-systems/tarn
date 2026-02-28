exports.handler = async (event) => {
  const records = Array.isArray(event.Records) ? event.Records : [];

  const processed = records.map((record) => {
    let body = record.body;

    try {
      body = JSON.parse(record.body);
    } catch {
      // Keep the raw body when it is not JSON.
    }

    console.log(
      JSON.stringify({
        messageId: record.messageId,
        body,
      })
    );

    return {
      messageId: record.messageId,
      body,
    };
  });

  return {
    statusCode: 200,
    processedCount: processed.length,
    records: processed,
    timestamp: new Date().toISOString(),
  };
};
