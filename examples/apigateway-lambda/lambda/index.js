exports.handler = async (event) => {
  const query = event && event.queryStringParameters ? event.queryStringParameters : {};
  const params = event && event.pathParameters ? event.pathParameters : {};
  const requestContext = event && event.requestContext ? event.requestContext : {};
  const http = requestContext && requestContext.http ? requestContext.http : {};

  const name = query.name || 'world';
  const pathId = params.id || null;

  return {
    statusCode: 200,
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({
      service: 'apigateway-lambda-example',
      method: http.method || 'UNKNOWN',
      routeKey: event && event.routeKey ? event.routeKey : '',
      rawPath: event && event.rawPath ? event.rawPath : '',
      name,
      pathId
    })
  };
};
