exports.handler = async (event) => {
  const http = (event && event.requestContext && event.requestContext.http) || {};
  const orderId = event && event.pathParameters ? event.pathParameters.id || null : null;

  return {
    statusCode: 200,
    headers: {
      'content-type': 'application/json'
    },
    body: JSON.stringify({
      feature: 'r10',
      component: 'api',
      method: http.method || 'GET',
      path: event && event.rawPath ? event.rawPath : '/',
      orderId,
      tags: {
        feature: 'r10',
        component: 'api',
        surface: 'public'
      }
    })
  };
};
