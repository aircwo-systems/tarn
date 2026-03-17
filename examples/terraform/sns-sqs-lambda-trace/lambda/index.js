exports.handler = async (event) => {
  const records = Array.isArray(event?.Records) ? event.Records : [];

  for (const record of records) {
    const rawBody = typeof record.body === "string" ? record.body : "";
    let parsedBody = null;

    try {
      parsedBody = JSON.parse(rawBody);
    } catch {
      // Keep the body as a string when it is not JSON.
    }

    // Handle both raw-message delivery and SNS envelope delivery.
    const messagePayload =
      parsedBody && typeof parsedBody === "object" && typeof parsedBody.Message === "string"
        ? safeJSONParse(parsedBody.Message)
        : parsedBody ?? rawBody;

    const messageId =
      (messagePayload && typeof messagePayload === "object" && messagePayload.id) ||
      record.messageId ||
      "unknown";

    console.log(
      JSON.stringify(
        {
          stage: "sns-sqs-lambda",
          action: "processed",
          lambda: process.env.AWS_LAMBDA_FUNCTION_NAME || "sns-worker",
          queueMessageId: record.messageId,
          messageId,
          payload: messagePayload,
        },
        null,
        2,
      ),
    );
  }

  return {
    statusCode: 200,
    body: JSON.stringify({
      processed: records.length,
    }),
  };
};

function safeJSONParse(value) {
  try {
    return JSON.parse(value);
  } catch {
    return value;
  }
}
