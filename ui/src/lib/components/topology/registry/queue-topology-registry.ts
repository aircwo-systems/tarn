import { createStandardTopologyRegistry } from "./default-topology-registry";

export const queueTopologyRegistry = createStandardTopologyRegistry(
  "queue",
  "queues",
);
