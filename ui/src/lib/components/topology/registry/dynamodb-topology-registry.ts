import { createStandardTopologyRegistry } from "./default-topology-registry";

export const dynamodbTopologyRegistry = createStandardTopologyRegistry(
  "dynamodb",
  "dynamodb",
  ["small", "medium", "large"],
);
