import { createStandardTopologyRegistry } from "./default-topology-registry";

export const functionTopologyRegistry = createStandardTopologyRegistry(
  "function",
  "functions",
  ["small", "medium", "large"],
);
