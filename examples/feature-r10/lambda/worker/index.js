exports.handler = async (event) => {
  return {
    feature: 'r10',
    component: 'worker',
    received: event,
    tags: {
      feature: 'r10',
      component: 'worker',
      role: 'background'
    }
  };
};
