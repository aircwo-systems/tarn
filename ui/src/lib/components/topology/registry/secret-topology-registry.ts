import { createStandardTopologyRegistry } from "./default-topology-registry";

export const secretTopologyRegistry = createStandardTopologyRegistry(
  "secret",
  "secrets",
  ["small"],
);
