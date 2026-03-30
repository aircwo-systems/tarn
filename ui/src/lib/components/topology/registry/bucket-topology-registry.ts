import type { TopologyNodeRegistry } from "./types";

export const bucketTopologyRegistry: TopologyNodeRegistry = {
  kind: "bucket",
  configTab: "storage",
  defaultView: "standard",
  supportedSizes: ["small", "medium", "large"],
  views: [
    {
      id: "standard",
      label: "Standard",
      supportedSizes: ["small", "medium", "large"],
    },
    {
      id: "recent-artifacts",
      label: "Recent artifacts",
      supportedSizes: ["medium", "large"],
    },
    {
      id: "artifact-grid",
      label: "Artifact grid",
      supportedSizes: ["medium", "large"],
    },
  ],
};
