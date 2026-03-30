import { createStandardTopologyRegistry } from "./default-topology-registry";

export const infraTopologyRegistry = createStandardTopologyRegistry(
  "infra",
  undefined,
  ["small", "medium"],
);
