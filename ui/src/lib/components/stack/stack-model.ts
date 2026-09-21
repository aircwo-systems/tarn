import type { ConnectionNode, NodeKind } from "$lib/components/topology/types";

/**
 * Stack view model: turns the topology graph into independent groups of
 * resources, each laid out as a trace-style waterfall.
 *
 * A group is a connected component of the graph ignoring shared kinds
 * (secrets, the secrets cache extension, external infra). Shared nodes are
 * copied into every group that uses them and become the links between groups.
 */

type Edge = { from: ConnectionNode; to: ConnectionNode };

export const SHARED_KINDS: ReadonlySet<NodeKind> = new Set(["secret", "extension", "infra"]);

/** Waterfall layer a kind sits on when it starts a chain. */
const KIND_RANK: Record<NodeKind, number> = {
  gateway: 0,
  eventbridge: 0,
  bucket: 1,
  topic: 1,
  queue: 2,
  dynamodb: 3,
  function: 3,
  extension: 4,
  secret: 5,
  infra: 5,
};

export interface StackRow {
  node: ConnectionNode;
  depth: number;
  /** Bar end column (exclusive): depth + height of the subtree below it. */
  end: number;
  parentIndex: number | null;
  /** Children already placed under another parent (extra inbound edges). */
  refs: number[];
  shared: boolean;
  /** Other groups that also use this shared node. */
  sharedWith: string[];
}

export interface StackGroup {
  id: string;
  title: string;
  rows: StackRow[];
  depth: number;
  kinds: Partial<Record<NodeKind, number>>;
  /** Linked group id → names of shared nodes that link them. */
  links: Map<string, string[]>;
}

export function buildStackGroups(nodes: ConnectionNode[], edges: Edge[]): StackGroup[] {
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const out = new Map<string, ConnectionNode[]>();
  for (const e of edges) {
    if (e.from.id === e.to.id) continue;
    (out.get(e.from.id) ?? out.set(e.from.id, []).get(e.from.id)!).push(e.to);
  }

  // Union-find over non-shared nodes.
  const parent = new Map<string, string>();
  const find = (id: string): string => {
    let root = id;
    while (parent.get(root) !== root) root = parent.get(root)!;
    parent.set(id, root);
    return root;
  };
  for (const n of nodes) if (!SHARED_KINDS.has(n.kind)) parent.set(n.id, n.id);
  for (const e of edges) {
    if (parent.has(e.from.id) && parent.has(e.to.id)) parent.set(find(e.from.id), find(e.to.id));
  }

  const members = new Map<string, Set<string>>();
  for (const id of parent.keys()) {
    const root = find(id);
    (members.get(root) ?? members.set(root, new Set()).get(root)!).add(id);
  }

  // Singletons are noise as separate cards; pool them.
  const loose = new Set<string>();
  for (const [root, set] of members) {
    const hasShared = [...set].some((id) => (out.get(id) ?? []).some((t) => SHARED_KINDS.has(t.kind)));
    if (set.size === 1 && !hasShared) {
      loose.add([...set][0]);
      members.delete(root);
    }
  }

  // Pull in shared nodes reachable from each group.
  const sharedUsers = new Map<string, Set<string>>();
  for (const [root, set] of members) {
    const stack = [...set];
    while (stack.length) {
      for (const t of out.get(stack.pop()!) ?? []) {
        if (!SHARED_KINDS.has(t.kind) || set.has(t.id)) continue;
        set.add(t.id);
        stack.push(t.id);
        (sharedUsers.get(t.id) ?? sharedUsers.set(t.id, new Set()).get(t.id)!).add(root);
      }
    }
  }

  const groups: StackGroup[] = [];
  for (const [root, set] of members) groups.push(layoutGroup(root, set, byId, out));
  if (loose.size) {
    const g = layoutGroup("loose", loose, byId, out);
    g.title = "Unlinked resources";
    groups.push(g);
  }

  const titleOf = new Map(groups.map((g) => [g.id, g.title]));
  for (const g of groups) {
    for (const row of g.rows) {
      if (!row.shared) continue;
      row.sharedWith = [...(sharedUsers.get(row.node.id) ?? [])].filter((id) => id !== g.id).map((id) => titleOf.get(id) ?? id);
      for (const other of sharedUsers.get(row.node.id) ?? []) {
        if (other === g.id) continue;
        const via = g.links.get(other) ?? [];
        if (!via.includes((row.node.fullLabel ?? row.node.label))) via.push((row.node.fullLabel ?? row.node.label));
        g.links.set(other, via);
      }
    }
  }

  return groups.sort((a, b) => (a.id === "loose" ? 1 : b.id === "loose" ? -1 : b.rows.length - a.rows.length || a.title.localeCompare(b.title)));
}

function layoutGroup(
  id: string,
  set: Set<string>,
  byId: Map<string, ConnectionNode>,
  out: Map<string, ConnectionNode[]>,
): StackGroup {
  const order = (a: ConnectionNode, b: ConnectionNode) => KIND_RANK[a.kind] - KIND_RANK[b.kind] || (a.fullLabel ?? a.label).localeCompare(b.fullLabel ?? b.label);
  const children = (nid: string) => (out.get(nid) ?? []).filter((t) => set.has(t.id)).sort(order);

  const incoming = new Set<string>();
  for (const nid of set) for (const t of children(nid)) incoming.add(t.id);
  const nodes = [...set].map((nid) => byId.get(nid)!).filter(Boolean).sort(order);
  const roots = nodes.filter((n) => !incoming.has(n.id));

  const rows: StackRow[] = [];
  const placed = new Set<string>();
  const visit = (node: ConnectionNode, depth: number, parentIndex: number | null): number => {
    placed.add(node.id);
    const index = rows.length;
    rows.push({ node, depth, end: depth + 1, parentIndex, refs: [], shared: SHARED_KINDS.has(node.kind), sharedWith: [] });
    let end = depth + 1;
    for (const child of children(node.id)) {
      if (placed.has(child.id)) continue;
      end = Math.max(end, visit(child, depth + 1, index));
    }
    rows[index].end = end;
    return end;
  };
  for (const r of roots) visit(r, 0, null);
  // Anything left sits on a cycle; start from the earliest-ranked node.
  for (const n of nodes) if (!placed.has(n.id)) visit(n, 0, null);

  const indexOf = new Map(rows.map((r, i) => [r.node.id, i]));
  rows.forEach((row, i) => {
    for (const child of children(row.node.id)) {
      const ci = indexOf.get(child.id)!;
      if (rows[ci].parentIndex !== i && !row.refs.includes(ci)) row.refs.push(ci);
    }
  });

  const kinds: StackGroup["kinds"] = {};
  for (const n of nodes) kinds[n.kind] = (kinds[n.kind] ?? 0) + 1;

  const lead = roots.find((r) => !SHARED_KINDS.has(r.kind)) ?? nodes[0];
  return {
    id,
    title: lead ? (lead.fullLabel ?? lead.label) : id,
    rows,
    depth: Math.max(1, ...rows.map((r) => r.end)),
    kinds,
    links: new Map(),
  };
}

export const KIND_LABEL: Record<NodeKind, string> = {
  gateway: "API",
  eventbridge: "Rule",
  topic: "SNS",
  queue: "SQS",
  dynamodb: "DynamoDB",
  bucket: "S3",
  function: "Lambda",
  secret: "Secret",
  extension: "Cache",
  infra: "External",
};

export const KIND_TAB: Partial<Record<NodeKind, string>> = {
  gateway: "gateways",
  eventbridge: "eventbridge",
  topic: "sns",
  queue: "queues",
  dynamodb: "dynamodb",
  bucket: "storage",
  function: "functions",
  secret: "secrets",
  infra: "services",
};

export function kindVar(kind: NodeKind): string {
  return `var(--stack-${kind})`;
}
