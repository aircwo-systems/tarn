exports.handler = async (event) => ({ arch: process.arch, mode: process.env.MODE, event });
