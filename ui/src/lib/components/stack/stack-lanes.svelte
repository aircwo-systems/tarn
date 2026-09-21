<script lang="ts">
  import { ArrowSquareOutIcon, CaretRightIcon, MagnifyingGlassIcon, PushPinIcon, XIcon } from "phosphor-svelte";
  import type { NodeKind } from "$lib/components/topology/types";
  import { KIND_LABEL, KIND_TAB, kindVar, type StackGroup } from "./stack-model";

  let {
    groups,
    onNavigate = (_tab: string) => {},
    detail = false,
  }: {
    groups: StackGroup[];
    onNavigate?: (tab: string) => void;
    /** Drilled into a single stack: show it whole, no shelf, no collapsing. */
    detail?: boolean;
  } = $props();

  // Columns read left→right in request order; the last two form the shared rail.
  const COLUMNS: { title: string; kinds: NodeKind[] }[] = [
    { title: "Entry", kinds: ["gateway", "eventbridge", "bucket", "topic"] },
    { title: "Queue", kinds: ["queue"] },
    { title: "Compute", kinds: ["function"] },
    { title: "Data", kinds: ["dynamodb"] },
    { title: "Cache · External", kinds: ["extension", "infra"] },
    { title: "Secrets", kinds: ["secret"] },
  ];
  const SHARED_FROM = 4;

  const NODE_H = 28;
  const PITCH = 36;
  const LANE_HEAD = 34;
  const LANE_PAD = 12;
  const LANE_GAP = 10;
  const COLLAPSED_H = 34;
  const GUTTER = 44;
  const RADIUS = 7;

  const colOf = (kind: NodeKind) => COLUMNS.findIndex((c) => c.kinds.includes(kind));

  type Placed = { id: string; kind: NodeKind; label: string; sub: string; col: number; y: number; lanes: Set<string> };
  type Link = { id: string; from: string; to: string; lane: string };
  type Lane = { id: string; title: string; y: number; h: number; collapsed: boolean; kinds: [NodeKind, number][]; total: number };
  type ShelfNode = { id: string; kind: NodeKind; label: string; sub: string };
  type Shelf = { id: string; title: string; nodes: ShelfNode[]; shared: ShelfNode[]; kinds: [NodeKind, number][] };

  /** Lanes this small say everything they have to say on one line. */
  const SHELF_MAX = 2;
  const SHARED_ORDER: NodeKind[] = ["infra", "extension", "secret"];

  // ─── Collapsed lanes (persisted) ───
  const COLLAPSE_KEY = "tarn-ui-stack-collapsed";
  let collapsed = $state<Set<string>>(readCollapsed());
  function readCollapsed(): Set<string> {
    try {
      const raw = localStorage.getItem(COLLAPSE_KEY);
      return new Set(raw ? (JSON.parse(raw) as string[]) : ["loose"]);
    } catch {
      return new Set(["loose"]);
    }
  }
  function toggle(id: string) {
    const next = new Set(collapsed);
    if (!next.delete(id)) next.add(id);
    collapsed = next;
    localStorage.setItem(COLLAPSE_KEY, JSON.stringify([...next]));
  }

  let width = $state(0);

  const usedCols = $derived.by(() => {
    const used = new Set<number>();
    for (const g of groups) for (const r of g.rows) used.add(colOf(r.node.kind));
    return [...used].filter((c) => c >= 0).sort((a, b) => a - b);
  });

  const layout = $derived.by(() => {
    const nodes = new Map<string, Placed>();
    const links: Link[] = [];
    const lanes: Lane[] = [];
    const shelf: Shelf[] = [];
    const seen = new Set<string>();
    let y = 0;

    for (const g of groups) {
      const kinds = Object.entries(g.kinds) as [NodeKind, number][];
      const total = kinds.reduce((n, [, c]) => n + c, 0);
      const ownRows = g.rows.filter((r) => colOf(r.node.kind) < SHARED_FROM);
      if (!detail && !collapsed.has(g.id) && g.id !== "loose" && ownRows.length && ownRows.length <= SHELF_MAX) {
        // Too small for a lane of its own: it goes on the shelf, but its
        // shared resources still count it as a user.
        for (const row of g.rows) {
          if (colOf(row.node.kind) < SHARED_FROM) continue;
          const name = row.node.fullLabel ?? row.node.label;
          const node = nodes.get(row.node.id) ?? { id: row.node.id, kind: row.node.kind, label: name, sub: row.node.sub, col: colOf(row.node.kind), y: 0, lanes: new Set<string>() };
          node.lanes.add(g.id);
          nodes.set(row.node.id, node);
        }
        shelf.push({
          id: g.id,
          title: g.title,
          nodes: ownRows.map((r) => ({ id: r.node.id, kind: r.node.kind, label: r.node.fullLabel ?? r.node.label, sub: r.node.sub })),
          // External services first: a card has room for two names and
          // "reaches ledger-api" says more than "reaches a secret".
          shared: g.rows
            .filter((r) => colOf(r.node.kind) >= SHARED_FROM)
            .map((r) => ({ id: r.node.id, kind: r.node.kind, label: r.node.fullLabel ?? r.node.label, sub: r.node.sub }))
            .sort((a, b) => SHARED_ORDER.indexOf(a.kind) - SHARED_ORDER.indexOf(b.kind) || a.label.localeCompare(b.label)),
          kinds,
        });
        continue;
      }
      if (!detail && collapsed.has(g.id)) {
        // Shared nodes still count the lane as a user, but draw no links.
        for (const row of g.rows) nodes.get(row.node.id)?.lanes.add(g.id);
        lanes.push({ id: g.id, title: g.title, y, h: COLLAPSED_H, collapsed: true, kinds, total });
        y += COLLAPSED_H + LANE_GAP;
        continue;
      }

      const top = y + (detail ? 0 : LANE_HEAD);
      const own: Placed[] = [];
      for (const row of g.rows) {
        const col = colOf(row.node.kind);
        const name = row.node.fullLabel ?? row.node.label;
        if (col >= SHARED_FROM) {
          const shared = nodes.get(row.node.id) ?? { id: row.node.id, kind: row.node.kind, label: name, sub: row.node.sub, col, y: 0, lanes: new Set() };
          shared.lanes.add(g.id);
          nodes.set(row.node.id, shared);
          continue;
        }
        const n: Placed = { id: `${g.id}::${row.node.id}`, kind: row.node.kind, label: name, sub: row.node.sub, col, y: 0, lanes: new Set([g.id]) };
        own.push(n);
        nodes.set(n.id, n);
      }

      const key = (i: number) => {
        const node = g.rows[i].node;
        return colOf(node.kind) >= SHARED_FROM ? node.id : `${g.id}::${node.id}`;
      };
      const push = (from: string, to: string, id: string) => {
        // Links between shared nodes are global; draw them once.
        const global = !from.includes("::") && !to.includes("::");
        const lid = global ? `shared:${from}>${to}` : id;
        if (!seen.has(lid)) { seen.add(lid); links.push({ id: lid, from, to, lane: global ? "" : g.id }); }
      };
      g.rows.forEach((row, i) => {
        if (row.parentIndex !== null) push(key(row.parentIndex), key(i), `${g.id}:${row.parentIndex}>${i}`);
        for (const ci of row.refs) push(key(i), key(ci), `${g.id}:${i}>${ci}`);
      });

      // Place column by column; each node sits near the mean slot of whatever
      // feeds it from the left (barycentre ordering) to cut crossings.
      const slot = new Map<string, number>();
      let rowsUsed = 1;
      for (const col of [...new Set(own.map((n) => n.col))].sort((a, b) => a - b)) {
        const inCol = own.filter((n) => n.col === col);
        const weight = (n: Placed) => {
          const feeders = links.filter((l) => l.to === n.id && slot.has(l.from)).map((l) => slot.get(l.from)!);
          return feeders.length ? feeders.reduce((s, v) => s + v, 0) / feeders.length : Infinity;
        };
        const ranked = inCol.map((n, i) => ({ n, w: weight(n), i })).sort((a, b) => a.w - b.w || a.i - b.i);
        ranked.forEach(({ n }, i) => slot.set(n.id, i));
        rowsUsed = Math.max(rowsUsed, inCol.length);
      }
      for (const n of own) n.y = top + slot.get(n.id)! * PITCH;

      const h = (detail ? 0 : LANE_HEAD) + rowsUsed * PITCH - (PITCH - NODE_H) + LANE_PAD;
      lanes.push({ id: g.id, title: g.title, y, h, collapsed: false, kinds, total });
      y += h + LANE_GAP;
    }

    // Shared nodes sit at the mean height of whatever feeds them, then get
    // pushed apart so nothing overlaps.
    const shared = [...nodes.values()].filter((n) => n.col >= SHARED_FROM);
    const centreOf = (n: Placed) => n.y + NODE_H / 2;
    for (let pass = 0; pass < 2; pass++) {
      for (const n of shared) {
        const feeders = links.filter((l) => l.to === n.id).map((l) => nodes.get(l.from)!).filter((f) => f && f !== n);
        if (feeders.length) n.y = feeders.reduce((s, f) => s + centreOf(f), 0) / feeders.length - NODE_H / 2;
      }
      for (const col of [SHARED_FROM, SHARED_FROM + 1]) {
        const stack = shared.filter((n) => n.col === col).sort((a, b) => a.y - b.y || a.label.localeCompare(b.label));
        let floor = LANE_HEAD;
        for (const n of stack) {
          n.y = Math.max(n.y, floor);
          floor = n.y + PITCH;
        }
      }
    }
    const bottom = Math.max(y - LANE_GAP, ...shared.map((n) => n.y + NODE_H + LANE_PAD));
    // The rail hugs its contents rather than running the full canvas height.
    const railBottom = shared.length ? Math.max(...shared.map((n) => n.y + NODE_H)) + LANE_PAD : 0;
    return { nodes, links, lanes, shelf, height: bottom, railBottom };
  });

  // ─── Horizontal geometry ───
  // Shared columns hold few, short names, so they get a fixed width and the
  // request-path columns split whatever is left.
  const SHARED_W = 188;

  const colWidths = $derived.by(() => {
    if (!usedCols.length || !width) return new Map<number, number>();
    const sharedCols = usedCols.filter((c) => c >= SHARED_FROM);
    const flowCols = usedCols.filter((c) => c < SHARED_FROM);
    const sharedTotal = Math.min(sharedCols.length * SHARED_W, width * 0.34);
    const each = sharedCols.length ? sharedTotal / sharedCols.length : 0;
    const flowEach = flowCols.length ? (width - sharedTotal) / flowCols.length : 0;
    return new Map(usedCols.map((c) => [c, c >= SHARED_FROM ? each : flowEach]));
  });

  const xStart = (col: number) => {
    let x = 0;
    for (const c of usedCols) {
      if (c === col) break;
      x += colWidths.get(c) ?? 0;
    }
    return x;
  };
  const xOf = (col: number) => xStart(col) + GUTTER / 2;
  const NODE_MAX = 268;
  const wOf = (col: number) => Math.max(0, Math.min(NODE_MAX, (colWidths.get(col) ?? 0) - GUTTER));
  const sharedX = $derived(usedCols.some((c) => c >= SHARED_FROM) ? xStart(usedCols.find((c) => c >= SHARED_FROM)!) : width);

  /**
   * Each link turns in the gutter right after its source column. Links from
   * the same source share a track (they read as one fork); different sources
   * get their own evenly spaced track so parallel runs never merge. Links
   * into the shared rail get one track per target instead.
   */
  const tracks = $derived.by(() => {
    const buckets = new Map<string, { key: string; y: number }[]>();
    const trackKey = new Map<string, [string, string]>();
    for (const l of layout.links) {
      const a = layout.nodes.get(l.from);
      const b = layout.nodes.get(l.to);
      if (!a || !b || a.col === b.col || xOf(b.col) < xOf(a.col)) continue;
      const toRail = b.col >= SHARED_FROM && a.col < SHARED_FROM;
      const gap = toRail ? "rail" : `${l.lane}|${a.col}`;
      const key = toRail ? `t:${b.id}` : `s:${a.id}`;
      trackKey.set(l.id, [gap, key]);
      const list = buckets.get(gap) ?? [];
      if (!list.some((t) => t.key === key)) list.push({ key, y: (toRail ? b : a).y });
      buckets.set(gap, list);
    }
    const frac = new Map<string, number>();
    for (const [gap, list] of buckets) {
      list.sort((p, q) => p.y - q.y);
      list.forEach((t, i) => frac.set(`${gap}#${t.key}`, (i + 1) / (list.length + 1)));
    }
    const out = new Map<string, number>();
    for (const [id, [gap, key]] of trackKey) out.set(id, frac.get(`${gap}#${key}`) ?? 0.5);
    return out;
  });

  /** Orthogonal path with rounded bends, right edge of `a` to left edge of `b`. */
  function route(link: Link, a: Placed, b: Placed): string {
    const x1 = xOf(a.col) + wOf(a.col);
    const y1 = a.y + NODE_H / 2;
    const x2 = xOf(b.col);
    const y2 = b.y + NODE_H / 2;
    if (a.col === b.col) {
      // Same column (queue → DLQ, cache → cache): loop out to the right.
      const out = x1 + 12;
      const r = Math.min(RADIUS, Math.abs(y2 - y1) / 2);
      const s = Math.sign(y2 - y1) || 1;
      return `M ${x1} ${y1} H ${out - r} Q ${out} ${y1} ${out} ${y1 + s * r} V ${y2 - s * r} Q ${out} ${y2} ${out - r} ${y2} H ${x1}`;
    }
    if (x2 < x1) {
      // Backward edge (stream → function): arc under both nodes.
      const low = Math.max(a.y, b.y) + NODE_H + 10;
      const ax = xOf(a.col) + wOf(a.col) / 2;
      const bx = xOf(b.col) + wOf(b.col) / 2;
      return `M ${ax} ${a.y + NODE_H} C ${ax} ${low} ${bx} ${low} ${bx} ${b.y + NODE_H}`;
    }
    const f = tracks.get(link.id) ?? 0.5;
    const toRail = b.col >= SHARED_FROM && a.col < SHARED_FROM;
    const lo = toRail ? sharedX - GUTTER / 2 : x1;
    const mid = lo + (GUTTER - 4) * f + (toRail ? 0 : 2);
    const skips = !toRail && usedCols.indexOf(b.col) - usedCols.indexOf(a.col) > 1;
    if (skips) {
      // Skipping a column: drop into the empty channel just above the target
      // row so the run never hides behind nodes in between, then rise again
      // in the gutter before the target.
      const cy = b.y - (PITCH - NODE_H) / 2;
      const mid2 = x2 - 6;
      const r1 = Math.max(0, Math.min(RADIUS, Math.abs(cy - y1) / 2));
      const r2 = Math.max(0, Math.min(RADIUS, Math.abs(y2 - cy) / 2, 6));
      const s1 = Math.sign(cy - y1) || 1;
      return `M ${x1} ${y1} H ${mid - r1} Q ${mid} ${y1} ${mid} ${y1 + s1 * r1} V ${cy - s1 * r1} Q ${mid} ${cy} ${mid + r1} ${cy} H ${mid2 - r2} Q ${mid2} ${cy} ${mid2} ${cy + r2} V ${y2 - r2} Q ${mid2} ${y2} ${mid2 + r2} ${y2} H ${x2}`;
    }
    const dy = y2 - y1;
    if (Math.abs(dy) < 1) return `M ${x1} ${y1} H ${x2}`;
    const r = Math.max(0, Math.min(RADIUS, Math.abs(dy) / 2, mid - x1, x2 - mid));
    const s = Math.sign(dy);
    return `M ${x1} ${y1} H ${mid - r} Q ${mid} ${y1} ${mid} ${y1 + s * r} V ${y2 - s * r} Q ${mid} ${y2} ${mid + r} ${y2} H ${x2}`;
  }

  // ─── Focus: hover a node → its whole upstream/downstream chain ───
  let hoverNode = $state<string | null>(null);
  let hoverLane = $state<string | null>(null);
  /** Click a resource to keep its trace lit while the pointer moves away. */
  let pinned = $state<string | null>(null);
  let query = $state("");
  let searchEl = $state<HTMLInputElement | null>(null);

  const activeNode = $derived(pinned && layout.nodes.has(pinned) ? pinned : hoverNode);

  function onKey(e: KeyboardEvent) {
    const typing = e.target instanceof HTMLElement && ["INPUT", "TEXTAREA"].includes(e.target.tagName);
    if (e.key === "Escape") {
      if (query) query = "";
      else pinned = null;
      if (typing) (e.target as HTMLElement).blur();
      return;
    }
    if (e.key === "/" && !typing) {
      e.preventDefault();
      searchEl?.focus();
    }
  }

  // Search dims everything that doesn't match; it never touches the user's
  // collapse choices.
  const matches = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return null;
    const ids = new Set<string>();
    const lanes = new Set<string>();
    for (const n of layout.nodes.values()) {
      if (!`${n.label} ${n.sub ?? ""} ${KIND_LABEL[n.kind]}`.toLowerCase().includes(q)) continue;
      ids.add(n.id);
      if (n.col < SHARED_FROM) for (const l of n.lanes) lanes.add(l);
    }
    for (const card of layout.shelf) {
      for (const n of card.nodes) {
        if (!`${n.label} ${n.sub ?? ""} ${KIND_LABEL[n.kind]}`.toLowerCase().includes(q)) continue;
        ids.add(n.id);
        lanes.add(card.id);
      }
    }
    return { ids, lanes };
  });

  const shelfIds = $derived(new Set(layout.shelf.map((c) => c.id)));
  const collapsible = $derived(groups.filter((g) => !shelfIds.has(g.id)));
  const allCollapsed = $derived(collapsible.length > 0 && collapsible.every((g) => collapsed.has(g.id)));
  function toggleAll() {
    const next = allCollapsed ? new Set<string>() : new Set(collapsible.map((g) => g.id));
    collapsed = next;
    localStorage.setItem(COLLAPSE_KEY, JSON.stringify([...next]));
  }

  function walk(start: string, dir: "from" | "to", edges: Set<string>, nodeIds: Set<string>) {
    const other = dir === "from" ? "to" : "from";
    const queue = [start];
    while (queue.length) {
      const id = queue.pop()!;
      for (const l of layout.links) {
        if (l[dir] !== id || edges.has(l.id)) continue;
        edges.add(l.id);
        nodeIds.add(l[other]);
        // Stop at shared nodes so one secret doesn't light up every stack.
        if (!layout.nodes.get(l[other])?.id.includes("::") && l[other] !== start) continue;
        queue.push(l[other]);
      }
    }
  }

  const focus = $derived.by(() => {
    if (activeNode) {
      const edges = new Set<string>();
      const nodeIds = new Set([activeNode]);
      walk(activeNode, "from", edges, nodeIds);
      walk(activeNode, "to", edges, nodeIds);
      const lanes = new Set<string>();
      for (const id of nodeIds) for (const lane of layout.nodes.get(id)?.lanes ?? []) if (id.includes("::") || id === activeNode) lanes.add(lane);
      return { edges, nodeIds, lanes };
    }
    if (hoverLane) {
      const inLane = (id: string) => layout.nodes.get(id)?.lanes.has(hoverLane!) ?? false;
      const edges = new Set(layout.links.filter((l) => l.lane === hoverLane || (!l.lane && inLane(l.from) && inLane(l.to))).map((l) => l.id));
      const nodeIds = new Set<string>();
      for (const n of layout.nodes.values()) if (n.lanes.has(hoverLane)) nodeIds.add(n.id);
      return { edges, nodeIds, lanes: new Set([hoverLane]) };
    }
    return null;
  });

  const hovered = $derived(activeNode ? layout.nodes.get(activeNode) : null);

  /** Direct neighbours of the pinned resource, for the inspector bar. */
  const neighbours = $derived.by(() => {
    if (!pinned) return null;
    const up: Placed[] = [];
    const down: Placed[] = [];
    for (const l of layout.links) {
      if (l.to === pinned) { const n = layout.nodes.get(l.from); if (n && !up.includes(n)) up.push(n); }
      if (l.from === pinned) { const n = layout.nodes.get(l.to); if (n && !down.includes(n)) down.push(n); }
    }
    return { up, down };
  });
</script>

<svelte:window onkeydown={onKey} />

<div class="lanes-view">
  <div class="toolbar">
    <label class="search" class:active={query}>
      <MagnifyingGlassIcon size={12} />
      <input
        bind:this={searchEl}
        bind:value={query}
        type="search"
        placeholder="Find a resource"
        spellcheck="false"
        aria-label="Find a resource"
      />
      {#if query}
        <button type="button" class="clear" onclick={() => (query = "")} aria-label="Clear search"><XIcon size={10} weight="bold" /></button>
      {:else}
        <kbd>/</kbd>
      {/if}
    </label>
    {#if matches}
      <span class="hits">{matches.ids.size} match{matches.ids.size === 1 ? "" : "es"} in {matches.lanes.size} stack{matches.lanes.size === 1 ? "" : "s"}</span>
    {/if}
    {#if !detail}
      <button type="button" class="ghost" onclick={toggleAll}>{allCollapsed ? "Expand all" : "Collapse all"}</button>
    {/if}
  </div>

  {#if pinned && hovered && neighbours}
    <div class="inspector">
      <span class="pin" aria-hidden="true"><PushPinIcon size={11} weight="fill" /></span>
      <span class="ins-kind" style:--c={kindVar(hovered.kind)}>{KIND_LABEL[hovered.kind]}</span>
      <span class="ins-name">{hovered.label}</span>
      {#if hovered.sub}<span class="ins-sub">{hovered.sub}</span>{/if}
      <div class="ins-rel">
        {#each [["Upstream", neighbours.up], ["Downstream", neighbours.down]] as const as [label, list] (label)}
          {#if list.length}
            <span class="rel-label">{label}</span>
            {#each list as n (n.id)}
              <button type="button" class="rel" style:--c={kindVar(n.kind)} onclick={() => (pinned = n.id)}>{n.label}</button>
            {/each}
          {/if}
        {/each}
      </div>
      {#if KIND_TAB[hovered.kind]}
        <button type="button" class="ghost open" onclick={() => onNavigate(KIND_TAB[hovered.kind]!)}>
          Open in {KIND_TAB[hovered.kind]?.replace(/^./, (c) => c.toUpperCase())}<ArrowSquareOutIcon size={11} />
        </button>
      {/if}
      <button type="button" class="ghost" onclick={() => (pinned = null)} aria-label="Unpin"><XIcon size={11} weight="bold" /></button>
    </div>
  {/if}

  <div class="cols-head" style:grid-template-columns={usedCols.map((c) => `${colWidths.get(c) ?? 0}px`).join(" ")}>
    {#each usedCols as c, i (c)}
      <span class:shared={c >= SHARED_FROM}><em>{String(i + 1).padStart(2, "0")}</em>{COLUMNS[c].title}</span>
    {/each}
  </div>

  <div class="canvas" bind:clientWidth={width} style:height="{layout.height}px">
    {#if width}
      {#if layout.railBottom}
        <div class="rail" style:left="{sharedX}px" style:height="{layout.railBottom}px"></div>
      {/if}

      {#each layout.lanes as lane, li (lane.id)}
        <div
          class="lane"
          class:bare={detail}
          class:collapsed={lane.collapsed}
          class:dim={focus && !focus.lanes.has(lane.id)}
          class:faint={matches && !matches.lanes.has(lane.id)}
          style:top="{lane.y}px"
          style:height="{lane.h}px"
          style:width="{sharedX - 8}px"
          style:--i={li}
          role="presentation"
          onmouseenter={() => (hoverLane = lane.id)}
          onmouseleave={() => (hoverLane = null)}
        >
          {#if !detail}
          <button type="button" class="lane-head" onclick={() => !detail && toggle(lane.id)} aria-expanded={!lane.collapsed} disabled={detail}>
            <span class="caret"><CaretRightIcon size={10} weight="bold" /></span>
            <span class="lane-title" title={lane.title}>{lane.title}</span>
            <span class="mix" aria-hidden="true">
              {#each lane.kinds as [kind, n] (kind)}
                <span style:flex-grow={n} style:background={kindVar(kind)}></span>
              {/each}
            </span>
            <span class="lane-count">{lane.total}</span>
            {#if !pinned && hovered && hovered.lanes.has(lane.id) && hovered.id.includes("::")}
              <span class="peek" style:--c={kindVar(hovered.kind)}>
                <span class="peek-kind">{KIND_LABEL[hovered.kind]}</span>
                <span class="peek-name">{hovered.label}</span>
                {#if hovered.sub}<span>{hovered.sub}</span>{/if}
                {#if focus}<span>{focus.nodeIds.size - 1} linked</span>{/if}
              </span>
            {:else if lane.collapsed}
              <span class="lane-summary">
                {#each lane.kinds as [kind, n] (kind)}
                  <span style:--c={kindVar(kind)}>{n} {KIND_LABEL[kind]}</span>
                {/each}
              </span>
            {/if}
          </button>
          {/if}
        </div>
      {/each}

      <svg width={width} height={layout.height} aria-hidden="true">
        {#each layout.links as link (link.id)}
          {@const a = layout.nodes.get(link.from)}
          {@const b = layout.nodes.get(link.to)}
          {#if a && b}
            <path
              d={route(link, a, b)}
              class:shared={b.col >= SHARED_FROM}
              class:dim={focus && !focus.edges.has(link.id)}
              class:lit={focus?.edges.has(link.id)}
              style:stroke={kindVar(a.kind)}
            />
          {/if}
        {/each}
      </svg>

      {#each [...layout.nodes.values()] as node (node.id)}
        {#if node.col < SHARED_FROM || [...node.lanes].some((l) => !collapsed.has(l))}
          <button
            type="button"
            class="node"
            class:shared={node.col >= SHARED_FROM}
            class:hot={hoverNode === node.id}
            class:dim={focus && !focus.nodeIds.has(node.id)}
            class:faint={matches && !matches.ids.has(node.id)}
            class:hit={matches?.ids.has(node.id)}
            class:pinned={pinned === node.id}
            style:--c={kindVar(node.kind)}
            style:left="{xOf(node.col)}px"
            style:top="{node.y}px"
            style:width="{wOf(node.col)}px"
            onmouseenter={() => (hoverNode = node.id)}
            onmouseleave={() => (hoverNode = null)}
            onfocus={() => (hoverNode = node.id)}
            onblur={() => (hoverNode = null)}
            onclick={() => (pinned = pinned === node.id ? null : node.id)}
          >
            <span class="name">{node.label}</span>
            {#if node.lanes.size > 1}
              <span class="count">×{node.lanes.size}</span>
            {:else}
              <span class="kind">{KIND_LABEL[node.kind]}</span>
            {/if}
          </button>
        {/if}
      {/each}

    {/if}
  </div>

  {#if layout.shelf.length}
    <div class="shelf">
      <span class="shelf-label">Small stacks</span>
      <div class="shelf-grid">
        {#each layout.shelf as card (card.id)}
          <div
            class="card"
            class:dim={focus && !focus.lanes.has(card.id)}
            class:faint={matches && !matches.lanes.has(card.id)}
            role="presentation"
            onmouseenter={() => (hoverLane = card.id)}
            onmouseleave={() => (hoverLane = null)}
          >
            <span class="card-title" title={card.title}>{card.title}</span>
            <div class="chips">
              {#each card.nodes as n, i (n.id)}
                {#if i > 0}<span class="arrow" aria-hidden="true">→</span>{/if}
                <button
                  type="button"
                  class="chip"
                  class:faint={matches && !matches.ids.has(n.id)}
                  style:--c={kindVar(n.kind)}
                  title="{KIND_LABEL[n.kind]} · {n.label}"
                  onclick={() => KIND_TAB[n.kind] && onNavigate(KIND_TAB[n.kind]!)}
                >{n.label}</button>
              {/each}
              {#if card.shared.length}
                <span class="arrow" aria-hidden="true">→</span>
                {#each card.shared.slice(0, 2) as n (n.id)}
                  <button
                    type="button"
                    class="chip shared"
                    class:faint={matches && !matches.ids.has(n.id)}
                    style:--c={kindVar(n.kind)}
                    title="{KIND_LABEL[n.kind]} · {n.label}"
                    onmouseenter={() => (hoverNode = n.id)}
                    onmouseleave={() => (hoverNode = null)}
                    onclick={() => (pinned = pinned === n.id ? null : n.id)}
                  >{n.label}</button>
                {/each}
                {#if card.shared.length > 2}<span class="card-shared">+{card.shared.length - 2}</span>{/if}
              {/if}
            </div>
          </div>
        {/each}
      </div>
    </div>
  {/if}
</div>

<style>
  .lanes-view { display: flex; flex-direction: column; gap: 10px; }
  .cols-head { display: grid; border-bottom: 1px solid var(--border-subtle); }
  .cols-head span {
    display: flex; align-items: baseline; gap: 6px; padding: 0 22px 7px;
    font: 10px var(--font-mono, ui-monospace, monospace); letter-spacing: 0.05em; text-transform: uppercase; color: var(--text-secondary);
  }
  .cols-head em { font-style: normal; color: var(--text-tertiary); opacity: 0.6; }
  .cols-head span.shared { color: color-mix(in srgb, var(--color-chart-2) 75%, var(--text-secondary)); }

  .canvas { position: relative; }
  .rail {
    position: absolute; top: 0; right: 0; border-radius: 8px;
    border: 1px dashed color-mix(in srgb, var(--color-chart-2) 25%, var(--border-subtle));
    background:
      repeating-linear-gradient(135deg, transparent 0 7px, color-mix(in srgb, var(--color-chart-2) 4%, transparent) 7px 8px),
      color-mix(in srgb, var(--color-chart-2) 2%, transparent);
  }

  .lane {
    position: absolute; left: 0; border: 1px solid var(--border-subtle); border-radius: 8px; background: var(--bg-stage);
    transition: opacity 160ms ease, border-color 160ms ease, height 220ms var(--ease-snappy);
    animation: laneIn 240ms var(--ease-snappy) both; animation-delay: calc(var(--i) * 30ms);
  }
  .lane:hover { border-color: color-mix(in srgb, var(--text-primary) 16%, transparent); }
  .lane.bare { border-color: transparent; background: none; }
  .lane.collapsed { background: transparent; border-style: dashed; }
  @keyframes laneIn { from { opacity: 0; transform: translateY(3px); } }

  .lane-head {
    display: flex; align-items: center; gap: 8px; width: 100%; height: 32px; padding: 0 12px 0 8px; text-align: left; border-radius: 8px;
  }
  .lane-head:disabled { cursor: default; }
  .lane-head:disabled .caret { display: none; }
  .lane-head:disabled { cursor: default; }
  .lane-head:disabled .caret { display: none; }
  .caret { display: inline-flex; color: var(--text-tertiary); transform: rotate(90deg); transition: transform 180ms var(--ease-snappy), color 120ms ease; }
  .collapsed .caret { transform: rotate(0deg); }
  .lane-head:hover .caret { color: var(--text-primary); }
  .lane-head:focus-visible { outline: 1px solid var(--border-focus); outline-offset: -1px; }
  .lane-title { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; font-weight: 600; color: var(--text-primary); }
  .mix { display: flex; flex-shrink: 0; gap: 1px; width: 56px; height: 4px; border-radius: 2px; overflow: hidden; opacity: 0.8; }
  .mix span { min-width: 3px; }
  .lane-count { flex-shrink: 0; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .lane-summary { display: flex; gap: 10px; min-width: 0; margin-left: auto; overflow: hidden; }
  .lane-summary span {
    flex-shrink: 0; padding-left: 6px; border-left: 2px solid var(--c);
    font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); white-space: nowrap;
  }

  svg { position: absolute; inset: 0; overflow: visible; pointer-events: none; }
  path { fill: none; stroke-width: 1.4; stroke-linecap: round; opacity: 0.92; transition: opacity 160ms ease, stroke-width 160ms ease; }
  path.shared { stroke-dasharray: 1 4; opacity: 0.82; }
  path.lit { opacity: 1; stroke-width: 1.6; stroke-dasharray: 6 5; animation: flow 900ms linear infinite; }
  path.dim { opacity: 0.12; }
  @keyframes flow { to { stroke-dashoffset: -22; } }

  .node {
    position: absolute; height: 28px; display: flex; align-items: center; gap: 8px; padding: 0 9px 0 11px;
    border: 1px solid color-mix(in srgb, var(--c) 88%, var(--bg-stage)); border-radius: 7px;
    background: var(--bg-stage); text-align: left;
    transition: opacity 160ms ease, border-color 120ms ease, background 120ms ease, transform 160ms var(--ease-snappy);
  }
  .node:hover, .node.hot { border-color: color-mix(in srgb, var(--c) 70%, transparent); background: color-mix(in srgb, var(--c) 9%, var(--bg-stage)); }
  .node:active { transform: scale(0.98); }
  .node:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }
  .node.shared { border-style: dashed; }
  .name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 11.5px; color: var(--text-primary); }
  .kind, .count { flex-shrink: 0; font: 9.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .count { color: var(--color-chart-2); }
  .dim { opacity: 0.25; }

  .peek {
    display: flex; align-items: baseline; gap: 10px; min-width: 0; margin-left: auto; overflow: hidden; white-space: nowrap;
    font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); animation: peekIn 140ms var(--ease-snappy) both;
  }
  @keyframes peekIn { from { opacity: 0; transform: translateX(3px); } }
  .peek-kind { letter-spacing: 0.05em; text-transform: uppercase; color: var(--c); }
  .peek-name { font: 600 11.5px var(--font-sans, inherit); color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; }

  .toolbar { display: flex; align-items: center; gap: 10px; }
  .search {
    display: flex; align-items: center; gap: 7px; height: 26px; padding: 0 8px; border-radius: 7px;
    border: 1px solid var(--border-subtle); color: var(--text-tertiary); transition: border-color 140ms ease, color 140ms ease;
  }
  .search:focus-within, .search.active { border-color: var(--border-focus); color: var(--text-secondary); }
  .search input { width: 180px; background: none; border: 0; outline: none; font-size: 11.5px; color: var(--text-primary); }
  .search input::placeholder { color: var(--text-tertiary); }
  .search input::-webkit-search-cancel-button { display: none; }
  kbd {
    padding: 0 4px; border: 1px solid var(--border-subtle); border-radius: 4px;
    font: 9.5px/14px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary);
  }
  .clear { display: inline-flex; color: var(--text-tertiary); }
  .clear:hover { color: var(--text-primary); }
  .hits { font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .ghost {
    display: inline-flex; align-items: center; gap: 5px; height: 26px; padding: 0 9px; border-radius: 7px;
    border: 1px solid var(--border-subtle); font-size: 11.5px; color: var(--text-secondary);
    transition: border-color 120ms ease, color 120ms ease;
  }
  .toolbar .ghost { margin-left: auto; }
  .ghost:hover { border-color: var(--border-focus); color: var(--text-primary); }

  .inspector {
    display: flex; align-items: center; flex-wrap: wrap; gap: 8px 10px; padding: 8px 10px;
    border: 1px solid color-mix(in srgb, var(--text-primary) 14%, transparent); border-radius: 8px; background: var(--bg-stage);
    animation: insIn 160ms var(--ease-snappy) both;
  }
  @keyframes insIn { from { opacity: 0; transform: translateY(-3px); } }
  .pin { display: inline-flex; color: var(--text-tertiary); }
  .ins-kind { font: 9.5px var(--font-mono, ui-monospace, monospace); letter-spacing: 0.05em; text-transform: uppercase; color: var(--c); }
  .ins-name { font-size: 12.5px; font-weight: 600; color: var(--text-primary); }
  .ins-sub { font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .ins-rel { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; min-width: 0; }
  .rel-label { font: 9.5px var(--font-mono, ui-monospace, monospace); letter-spacing: 0.05em; text-transform: uppercase; color: var(--text-tertiary); }
  .rel-label:not(:first-child) { margin-left: 6px; }
  .rel {
    height: 20px; padding: 0 7px; border-radius: 5px;
    border: 1px solid color-mix(in srgb, var(--c) 85%, var(--bg-stage)); font-size: 11px; color: var(--text-secondary);
  }
  .rel:hover { border-color: color-mix(in srgb, var(--c) 70%, transparent); color: var(--text-primary); }
  .inspector .open { margin-left: auto; }
  .node.hit { border-color: color-mix(in srgb, var(--c) 60%, transparent); }
  .node.pinned { border-color: color-mix(in srgb, var(--c) 80%, transparent); background: color-mix(in srgb, var(--c) 12%, var(--bg-stage)); }
  .faint { opacity: 0.22; }

  .shelf { display: flex; flex-direction: column; gap: 8px; padding-top: 2px; }
  .shelf-label { font: 10px var(--font-mono, ui-monospace, monospace); letter-spacing: 0.05em; text-transform: uppercase; color: var(--text-tertiary); }
  .shelf-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 8px; }
  .card {
    display: flex; flex-direction: column; gap: 7px; min-height: 64px; padding: 9px 11px 10px;
    border: 1px solid var(--border-subtle); border-radius: 8px; background: var(--bg-stage);
    transition: opacity 160ms ease, border-color 160ms ease;
  }
  .card:hover { border-color: color-mix(in srgb, var(--text-primary) 16%, transparent); }
  .card-title { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 11.5px; font-weight: 600; color: var(--text-primary); }
  .chips { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; min-width: 0; }
  .chip {
    max-width: 100%; padding: 0 8px; height: 21px; border-radius: 5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    border: 1px solid color-mix(in srgb, var(--c) 85%, var(--bg-stage));
    font-size: 11px; color: var(--text-secondary); transition: border-color 120ms ease, color 120ms ease;
  }
  .chip:hover { border-color: color-mix(in srgb, var(--c) 70%, transparent); color: var(--text-primary); }
  .arrow { font-size: 10px; color: var(--text-tertiary); }
  .chip.shared { border-style: dashed; }
  .card-shared { font: 10px var(--font-mono, ui-monospace, monospace); color: var(--color-chart-2); }

  @media (prefers-reduced-motion: reduce) {
    .lane, .peek, .inspector { animation: none; }
    path.lit { animation: none; stroke-dasharray: none; }
  }
</style>
