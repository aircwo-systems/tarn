import type { NodeKind, NodeSize, NodeView } from "../types";
import { bucketTopologyRegistry } from "./bucket-topology-registry";
import { dynamodbTopologyRegistry } from "./dynamodb-topology-registry";
import { eventbridgeTopologyRegistry } from "./eventbridge-topology-registry";
import { extensionTopologyRegistry } from "./extension-topology-registry";
import { functionTopologyRegistry } from "./function-topology-registry";
import { gatewayTopologyRegistry } from "./gateway-topology-registry";
import { infraTopologyRegistry } from "./infra-topology-registry";
import { queueTopologyRegistry } from "./queue-topology-registry";
import { secretTopologyRegistry } from "./secret-topology-registry";
import { topicTopologyRegistry } from "./topic-topology-registry";
import type { TopologyNodeRegistry, TopologyNodeViewDefinition } from "./types";

const standardTopologyRegistry: TopologyNodeRegistry = {
  kind: "gateway",
  defaultView: "standard",
  supportedSizes: ["small"],
  views: [
    {
      id: "standard",
      label: "Standard",
      supportedSizes: ["small"],
    },
  ],
};

const topologyRegistryByKind = {
  gateway: gatewayTopologyRegistry,
  eventbridge: eventbridgeTopologyRegistry,
  topic: topicTopologyRegistry,
  queue: queueTopologyRegistry,
  dynamodb: dynamodbTopologyRegistry,
  bucket: bucketTopologyRegistry,
  function: functionTopologyRegistry,
  secret: secretTopologyRegistry,
  extension: extensionTopologyRegistry,
  infra: infraTopologyRegistry,
} satisfies Record<NodeKind, TopologyNodeRegistry>;

export function getTopologyNodeRegistry(kind: NodeKind): TopologyNodeRegistry {
  return topologyRegistryByKind[kind] ?? standardTopologyRegistry;
}

export function getTopologyNodeViews(
  kind: NodeKind,
  size: NodeSize = "small",
): TopologyNodeViewDefinition[] {
  return getTopologyNodeRegistry(kind).views.filter((view) =>
    view.supportedSizes.includes(size),
  );
}

export function getTopologyNodeSupportedSizes(kind: NodeKind): readonly NodeSize[] {
  return getTopologyNodeRegistry(kind).supportedSizes;
}

export function resolveTopologyNodeSize(
  kind: NodeKind,
  requestedSize: NodeSize | undefined,
): NodeSize {
  const supportedSizes = getTopologyNodeSupportedSizes(kind);
  if (requestedSize && supportedSizes.includes(requestedSize)) {
    return requestedSize;
  }
  return supportedSizes[0] ?? "small";
}

export function resolveTopologyNodeView(
  kind: NodeKind,
  requestedView: NodeView | undefined,
  size: NodeSize = "small",
): NodeView {
  const registry = getTopologyNodeRegistry(kind);
  const supportedViews = getTopologyNodeViews(kind, size);

  if (requestedView && supportedViews.some((view) => view.id === requestedView)) {
    return requestedView;
  }

  const defaultView = registry.views.find((view) => view.id === registry.defaultView);
  if (defaultView && defaultView.supportedSizes.includes(size)) {
    return defaultView.id;
  }

  return supportedViews[0]?.id ?? registry.defaultView;
}

export function getTopologyNodeConfigTab(kind: NodeKind): string | null {
  return getTopologyNodeRegistry(kind).configTab ?? null;
}
