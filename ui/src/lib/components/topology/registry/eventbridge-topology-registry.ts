import { createStandardTopologyRegistry } from "./default-topology-registry";

export const eventbridgeTopologyRegistry = createStandardTopologyRegistry(
  "eventbridge",
  "eventbridge",
  ["small", "medium", "large"],
);
