exports.handler = async (event) => {
  for (const record of event.Records) {
    const body = JSON.parse(record.body);
    console.log('[payment-processor] received payment:', JSON.stringify(body));
  }
  return { statusCode: 200 };
};
