import type { OverviewResponse } from "$lib/types";
import { getVisibleInfra } from "$lib/state.svelte";
import { buildTopologyGraph } from "$lib/components/topology/topology-connection-model";
import { buildStackGroups, type StackGroup } from "./stack-model";

/** The overview's resources as stacks: the topology graph, grouped. */
export function stackGroupsFromOverview(d: OverviewResponse | null | undefined): StackGroup[] {
  if (!d) return [];
  const graph = buildTopologyGraph({
    gateways: d.gateways ?? [],
    functions: d.functions ?? [],
    queues: d.queues ?? [],
    dynamodbTables: d.dynamodbTables ?? [],
    topics: d.topics ?? [],
    buckets: d.buckets ?? [],
    secrets: d.secrets ?? [],
    infra: getVisibleInfra(d.infrastructure ?? []),
    infraConnections: d.connections ?? [],
    eventSourceMappings: d.eventSourceMappings ?? [],
    eventBridgeRules: d.eventBridgeRules ?? [],
    infraOrderIds: [],
  });
  return buildStackGroups(graph.allNodes, graph.allEdges);
}
