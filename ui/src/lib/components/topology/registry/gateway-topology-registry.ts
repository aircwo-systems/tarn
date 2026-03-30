import { createStandardTopologyRegistry } from "./default-topology-registry";

export const gatewayTopologyRegistry = createStandardTopologyRegistry(
  "gateway",
  "gateways",
);
