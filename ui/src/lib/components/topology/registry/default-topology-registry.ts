import type { NodeKind, NodeSize } from "../types";
import type { TopologyNodeRegistry } from "./types";

export function createStandardTopologyRegistry(
  kind: NodeKind,
  configTab?: string,
  supportedSizes: readonly NodeSize[] = ["small"],
): TopologyNodeRegistry {
  return {
    kind,
    configTab,
    defaultView: "standard",
    supportedSizes,
    views: [
      {
        id: "standard",
        label: "Standard",
        supportedSizes,
      },
    ],
  };
}
