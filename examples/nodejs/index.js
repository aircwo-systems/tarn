exports.handler = async (event, context) => {
  console.log("Event:", JSON.stringify(event));
  console.log("Context:", JSON.stringify({
    functionName: context.functionName,
    functionVersion: context.functionVersion,
    memoryLimitInMB: context.memoryLimitInMB,
  }));

  return {
    statusCode: 200,
    body: JSON.stringify({
      message: "Hello from OpenStack Lambda!",
      input: event,
      runtime: process.version,
      timestamp: new Date().toISOString(),
    }),
  };
};
