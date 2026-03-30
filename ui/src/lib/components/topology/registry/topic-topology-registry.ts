import { createStandardTopologyRegistry } from "./default-topology-registry";

export const topicTopologyRegistry = createStandardTopologyRegistry(
  "topic",
  "sns",
);
