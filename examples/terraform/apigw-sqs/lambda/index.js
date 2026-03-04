exports.handler = async (event) => {
  const records = Array.isArray(event.Records) ? event.Records : [];

  for (const record of records) {
    let body = record.body;
    try {
      body = JSON.parse(record.body);
    } catch {
      // keep raw string when body is not JSON
    }

    console.log(
      JSON.stringify({
        messageId: record.messageId,
        queue: record.eventSourceARN,
        body,
        receivedAt: new Date().toISOString(),
      })
    );
  }

  return { processedCount: records.length };
};
