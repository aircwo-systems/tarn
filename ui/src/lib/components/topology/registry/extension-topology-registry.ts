import { createStandardTopologyRegistry } from "./default-topology-registry";

export const extensionTopologyRegistry = createStandardTopologyRegistry(
  "extension",
  "secrets",
  ["small", "medium"],
);
