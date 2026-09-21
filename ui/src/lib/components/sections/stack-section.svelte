<script lang="ts">
  import { ArrowLeftIcon, ArrowsLeftRightIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import StackLanes from "$lib/components/stack/stack-lanes.svelte";
  import StackGrid from "$lib/components/stack/stack-grid.svelte";
  import StackHex from "$lib/components/stack/stack-hex.svelte";
  import StackHexCards from "$lib/components/stack/stack-hex-cards.svelte";
  import { getDashboard, getVisibleInfra } from "$lib/state.svelte";
  import { buildTopologyGraph } from "$lib/components/topology/topology-connection-model";
  import {
    buildStackGroups,
    KIND_LABEL,
    KIND_TAB,
    kindVar,
    type StackGroup,
  } from "$lib/components/stack/stack-model";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
    onNavigate = (_tab: string) => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
    onNavigate?: (tab: string) => void;
  } = $props();

  const dashboard = getDashboard();

  type View = "hexstacks" | "map" | "grid" | "lanes" | "waterfall";
  const VIEWS: View[] = ["hexstacks", "map", "grid", "lanes", "waterfall"];
  const VIEW_KEY = "tarn-ui-stack-view";
  let view = $state<View>(readView());
  function readView(): View {
    if (typeof localStorage === "undefined") return "hexstacks";
    const saved = localStorage.getItem(VIEW_KEY) as View | null;
    return saved && VIEWS.includes(saved) ? saved : "hexstacks";
  }
  function setView(next: View) {
    view = next;
    opened = null;
    localStorage.setItem(VIEW_KEY, next);
  }

  /** Grid drill-in: the id of the stack being viewed on its own. */
  let opened = $state<string | null>(null);

  const groups = $derived.by(() => {
    const d = dashboard.data;
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
  });

  const openedGroup = $derived(opened ? (groups.find((g) => g.id === opened) ?? null) : null);

  const linked = $derived(groups.filter((g) => g.links.size > 0).length);
  const resourceCount = $derived(groups.reduce((n, g) => n + g.rows.filter((r) => !r.shared).length, 0));

  const ROW = 30;
  const BAR = 18;
  const R = 8;

  let hover = $state<{ group: string; index: number } | null>(null);
  let flash = $state<string | null>(null);
  let widths = $state<Record<string, number>>({});

  function lineage(group: StackGroup, index: number): Set<number> {
    const out = new Set<number>();
    for (let i: number | null = index; i !== null; i = group.rows[i].parentIndex) out.add(i);
    return out;
  }

  function x(group: StackGroup, col: number): number {
    return ((widths[group.id] ?? 0) * col) / group.depth;
  }

  /** Rounded elbow from a parent bar's start down and across to a child bar. */
  function connector(group: StackGroup, child: number): string {
    const row = group.rows[child];
    const parent = group.rows[row.parentIndex!];
    const px = x(group, parent.depth) + 10;
    const py = row.parentIndex! * ROW + ROW / 2 + BAR / 2;
    const cx = x(group, row.depth);
    const cy = child * ROW + ROW / 2;
    const r = Math.min(R, cx - px, cy - py);
    return `M ${px} ${py} V ${cy - r} Q ${px} ${cy} ${px + r} ${cy} H ${cx}`;
  }

  /** Dashed link to a child that already sits under another parent. */
  function refPath(group: StackGroup, from: number, to: number): string {
    const px = x(group, group.rows[from].depth) + 10;
    const down = to > from;
    const py = from * ROW + ROW / 2 + (down ? BAR / 2 : -BAR / 2);
    const cx = x(group, group.rows[to].depth);
    const cy = to * ROW + ROW / 2;
    const r = Math.max(0, Math.min(R, Math.abs(cx - px), Math.abs(cy - py)));
    const dir = Math.sign(cx - px) || 1;
    return `M ${px} ${py} V ${down ? cy - r : cy + r} Q ${px} ${cy} ${px + dir * r} ${cy} H ${cx}`;
  }

  function jumpTo(id: string) {
    document.getElementById(`stack-${id}`)?.scrollIntoView({ behavior: "smooth", block: "start" });
    flash = id;
    setTimeout(() => (flash = flash === id ? null : flash), 1200);
  }

  const titleOf = $derived(new Map(groups.map((g) => [g.id, g.title])));
</script>

<div class="stack">
  <SectionHeader
    title="Stack"
    description="{groups.length} groups · {resourceCount} resources · {linked} linked · experimental"
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      <div class="seg" role="tablist" aria-label="Stack view">
        {#each [["hexstacks", "Hex stacks"], ["map", "Map"], ["grid", "Bento"], ["lanes", "Lanes"], ["waterfall", "Waterfall"]] as const as [id, label] (id)}
          <button type="button" role="tab" aria-selected={view === id} class:on={view === id} onclick={() => setView(id)}>{label}</button>
        {/each}
      </div>
    {/snippet}
  </SectionHeader>

  {#if dashboard.loading && !dashboard.data}
    <p class="empty">Loading…</p>
  {:else if groups.length === 0}
    <p class="empty">No resources yet. Deploy something and it'll show up here.</p>
  {:else if view === "hexstacks" || view === "map" || view === "grid"}
    {#if openedGroup}
      <div class="detail">
        <div class="detail-head">
          <button type="button" class="back" onclick={() => (opened = null)}>
            <ArrowLeftIcon size={12} weight="bold" />All stacks
          </button>
          <h2>{openedGroup.title}</h2>
          <span class="detail-meta">{openedGroup.rows.length} resources</span>
          {#if openedGroup.links.size}
            <div class="links">
              <span class="links-label">Shares with</span>
              {#each [...openedGroup.links] as [other, via] (other)}
                <button type="button" class="link" onclick={() => (opened = other)} title="Both stacks use {via.join(', ')}">
                  <ArrowsLeftRightIcon size={11} />{titleOf.get(other)}<span>· {via.length}</span>
                </button>
              {/each}
            </div>
          {/if}
        </div>
        <StackLanes groups={[openedGroup]} {onNavigate} detail />
      </div>
    {:else if view === "hexstacks"}
      <StackHexCards {groups} onOpen={(id) => (opened = id)} />
    {:else if view === "map"}
      <StackHex {groups} onOpen={(id) => (opened = id)} />
    {:else}
      <StackGrid {groups} onOpen={(id) => (opened = id)} />
    {/if}
  {:else if view === "lanes"}
    <StackLanes {groups} {onNavigate} />
  {:else}
    <div class="groups">
      {#each groups as group, gi (group.id)}
        {@const focus = hover?.group === group.id ? lineage(group, hover.index) : null}
        <section id="stack-{group.id}" class="group" class:flash={flash === group.id} style:--i={gi}>
          <header>
            <div class="title">
              <h2>{group.title}</h2>
              <div class="kinds">
                {#each Object.entries(group.kinds) as [kind, n] (kind)}
                  <span class="kind" style:--c={kindVar(kind as never)}>{n} {KIND_LABEL[kind as keyof typeof KIND_LABEL]}</span>
                {/each}
              </div>
            </div>
            {#if group.links.size}
              <div class="links">
                <span class="links-label">Shares with</span>
                {#each [...group.links] as [other, via] (other)}
                  <button type="button" class="link" onclick={() => jumpTo(other)} title="Both stacks use {via.join(', ')}. Click to jump.">
                    <ArrowsLeftRightIcon size={11} />{titleOf.get(other)}<span>· {via.length} shared</span>
                  </button>
                {/each}
              </div>
            {/if}
          </header>

          <div class="ruler" style:--depth={group.depth}>
            <span></span>
            <div class="cols">
              {#each Array(group.depth) as _, c (c)}<span>L{c}</span>{/each}
            </div>
          </div>

          <div class="body" style:--rows={group.rows.length} style:--depth={group.depth}>
            <div class="labels">
              {#each group.rows as row, i (row.node.id)}
                <button
                  type="button"
                  class="label"
                  class:dim={focus && !focus.has(i)}
                  style:padding-left="{row.depth * 12 + 8}px"
                  onmouseenter={() => (hover = { group: group.id, index: i })}
                  onmouseleave={() => (hover = null)}
                  onclick={() => KIND_TAB[row.node.kind] && onNavigate(KIND_TAB[row.node.kind]!)}
                >
                  <span class="tag" style:--c={kindVar(row.node.kind)}>{KIND_LABEL[row.node.kind]}</span>
                  <span class="name" title={(row.node.fullLabel ?? row.node.label)}>{(row.node.fullLabel ?? row.node.label)}</span>
                  {#if row.sharedWith.length}
                    <span class="shared" title="Also used by {row.sharedWith.join(', ')}">+{row.sharedWith.length}</span>
                  {/if}
                </button>
              {/each}
            </div>

            <div class="track" bind:clientWidth={widths[group.id]}>
              <div class="bands">
                {#each Array(group.depth) as _, c (c)}<span></span>{/each}
              </div>
              {#if widths[group.id]}
                <svg width={widths[group.id]} height={group.rows.length * ROW} aria-hidden="true">
                  {#each group.rows as row, i (row.node.id)}
                    {#if row.parentIndex !== null}
                      <path
                        d={connector(group, i)}
                        class:dim={focus && !focus.has(i)}
                        style:stroke={kindVar(group.rows[row.parentIndex].node.kind)}
                      />
                    {/if}
                    {#each row.refs as ci (ci)}
                      <path
                        class="ref"
                        d={refPath(group, i, ci)}
                        class:dim={focus && hover?.index !== i && hover?.index !== ci}
                        style:stroke={kindVar(row.node.kind)}
                      />
                    {/each}
                  {/each}
                </svg>
              {/if}
              {#each group.rows as row, i (row.node.id)}
                <div
                  class="bar"
                  class:shared={row.shared}
                  class:dim={focus && !focus.has(i)}
                  style:--c={kindVar(row.node.kind)}
                  style:top="{i * ROW + (ROW - BAR) / 2}px"
                  style:left="calc({row.depth} / var(--depth) * 100%)"
                  style:width="calc({row.end - row.depth} / var(--depth) * 100% - 4px)"
                  role="presentation"
                  onmouseenter={() => (hover = { group: group.id, index: i })}
                  onmouseleave={() => (hover = null)}
                >
                  <span>{row.node.sub || (row.node.fullLabel ?? row.node.label)}</span>
                </div>
              {/each}
            </div>
          </div>
        </section>
      {/each}
    </div>
  {/if}
</div>

<style>
  .stack { display: flex; flex-direction: column; gap: 16px; }
  .seg { display: inline-flex; padding: 2px; gap: 2px; border: 1px solid var(--border-subtle); border-radius: 8px; }
  .seg button { height: 22px; padding: 0 10px; border-radius: 6px; font-size: 11.5px; color: var(--text-tertiary); transition: color 120ms ease, background 120ms ease; }
  .seg button:hover { color: var(--text-primary); }
  .seg button.on { background: color-mix(in srgb, var(--text-primary) 8%, transparent); color: var(--text-primary); }
  .detail { display: flex; flex-direction: column; gap: 12px; animation: detailIn 200ms var(--ease-snappy) both; }
  @keyframes detailIn { from { opacity: 0; transform: translateY(4px); } }
  .detail-head { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; }
  .detail-head h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .detail-meta { font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .back {
    display: inline-flex; align-items: center; gap: 6px; height: 26px; padding: 0 10px; border-radius: 7px;
    border: 1px solid var(--border-subtle); font-size: 11.5px; color: var(--text-secondary);
    transition: border-color 120ms ease, color 120ms ease;
  }
  .back:hover { border-color: var(--border-focus); color: var(--text-primary); }
  .detail-head .links { margin-left: auto; }

  .empty { padding: 24px 0; font-size: 12px; color: var(--text-tertiary); }
  .groups { display: flex; flex-direction: column; gap: 12px; }

  .group {
    border: 1px solid var(--border-subtle); border-radius: 8px; background: var(--bg-stage);
    overflow: hidden; animation: groupIn 260ms var(--ease-snappy) both; animation-delay: calc(var(--i) * 40ms);
    transition: border-color 200ms ease; scroll-margin-top: 12px;
  }
  .group.flash { border-color: var(--border-focus); }
  @keyframes groupIn { from { opacity: 0; transform: translateY(4px); } }

  header { display: flex; flex-wrap: wrap; align-items: flex-start; justify-content: space-between; gap: 8px 16px; padding: 10px 12px; border-bottom: 1px solid var(--border-subtle); }
  .title { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; min-width: 0; }
  h2 { font-size: 13px; font-weight: 600; color: var(--text-primary); }
  .kinds, .links { display: flex; flex-wrap: wrap; gap: 4px; }
  .kind {
    padding: 1px 7px; border-radius: 999px; border: 1px solid color-mix(in srgb, var(--c) 35%, transparent);
    font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary);
  }
  .link {
    display: inline-flex; align-items: center; gap: 5px; height: 22px; padding: 0 8px; border-radius: 999px;
    border: 1px solid var(--border-subtle); font-size: 11px; color: var(--text-secondary); transition: border-color 120ms ease, color 120ms ease;
  }
  .links { align-items: center; }
  .links-label { font-size: 10.5px; color: var(--text-tertiary); margin-right: 2px; }
  h2 { max-width: 40ch; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .link span { font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .link:hover { border-color: var(--border-focus); color: var(--text-primary); }

  .ruler, .body { display: grid; grid-template-columns: minmax(12rem, 18rem) minmax(0, 1fr); }
  .ruler { border-bottom: 1px dashed var(--border-subtle); }
  .cols { display: grid; grid-template-columns: repeat(var(--depth, 1), 1fr); }
  .cols span { padding: 3px 6px; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); border-left: 1px solid var(--border-subtle); }

  .labels { display: flex; flex-direction: column; padding: 0 0 6px; border-right: 1px solid var(--border-subtle); min-width: 0; }
  .label {
    display: flex; align-items: center; gap: 6px; height: 30px; padding-right: 8px; min-width: 0; text-align: left;
    transition: opacity 150ms ease, background 120ms ease;
  }
  .label:hover { background: color-mix(in srgb, var(--text-primary) 4%, transparent); }
  .tag {
    flex-shrink: 0; padding: 0 5px; border-radius: 4px; border-left: 2px solid var(--c);
    background: color-mix(in srgb, var(--c) 10%, transparent); font: 9.5px/16px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary);
  }
  .name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; color: var(--text-primary); }
  .shared { flex-shrink: 0; margin-left: auto; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--color-chart-2); }

  .track { position: relative; height: calc(var(--rows) * 30px + 6px); min-width: 0; }
  .bands { position: absolute; inset: 0; display: grid; grid-template-columns: repeat(var(--depth), 1fr); }
  .bands span { border-left: 1px solid var(--border-subtle); }
  .bands span:nth-child(even) { background: color-mix(in srgb, var(--text-primary) 2%, transparent); }
  svg { position: absolute; inset: 0; overflow: visible; pointer-events: none; }
  path { fill: none; stroke-width: 1.25; opacity: 0.55; transition: opacity 150ms ease; }
  path.ref { stroke-dasharray: 3 3; opacity: 0.4; }
  path.dim { opacity: 0.12; }

  .bar {
    position: absolute; height: 18px; margin-left: 2px; padding: 0 8px; border-radius: 6px; overflow: hidden;
    display: flex; align-items: center;
    border: 1px solid color-mix(in srgb, var(--c) 55%, transparent);
    background: color-mix(in srgb, var(--c) 14%, transparent);
    transition: opacity 150ms ease, background 150ms ease;
  }
  .bar:hover { background: color-mix(in srgb, var(--c) 24%, transparent); }
  .bar.shared { border-style: dashed; background: color-mix(in srgb, var(--c) 7%, transparent); }
  .bar span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); }
  .dim { opacity: 0.3; }

  @media (prefers-reduced-motion: reduce) { .group { animation: none; } }
</style>
