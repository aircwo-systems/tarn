import type { NodeKind, NodeSize, NodeView } from "../types";

export interface TopologyNodeViewDefinition {
  id: NodeView;
  label: string;
  supportedSizes: readonly NodeSize[];
}

export interface TopologyNodeRegistry {
  kind: NodeKind;
  configTab?: string;
  defaultView: NodeView;
  supportedSizes: readonly NodeSize[];
  views: readonly TopologyNodeViewDefinition[];
}
