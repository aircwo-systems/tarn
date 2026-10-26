export const ssr = false;

export async function load() {
  const { unlockAWSOwnerFromLocation } = await import("#lib/aws-connections");
  await unlockAWSOwnerFromLocation();
  return {};
}
