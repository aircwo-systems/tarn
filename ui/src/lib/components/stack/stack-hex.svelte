<script lang="ts">
  import { MagnifyingGlassIcon, XIcon } from "phosphor-svelte";
  import type { NodeKind } from "$lib/components/topology/types";
  import { KIND_LABEL, kindVar, SHARED_KINDS, type StackGroup } from "./stack-model";

  let {
    groups,
    onOpen,
  }: {
    groups: StackGroup[];
    onOpen: (id: string) => void;
  } = $props();

  /**
   * Honeycomb map: one hexagon per resource. Shared resources hold the centre
   * of the board and each stack grows outward from it as a contiguous blob, so
   * the whole estate reads as a single shape instead of a grid of cards.
   */

  type Axial = { q: number; r: number };
  type Cell = Axial & {
    key: string;
    x: number;
    y: number;
    stack: string | null;
    label: string;
    kind: NodeKind | null;
    shared: boolean;
  };

  const DIRS: Axial[] = [
    { q: 1, r: 0 },
    { q: 1, r: -1 },
    { q: 0, r: -1 },
    { q: -1, r: 0 },
    { q: -1, r: 1 },
    { q: 0, r: 1 },
  ];
  const key = (q: number, r: number) => `${q},${r}`;
  const dist = (q: number, r: number) => (Math.abs(q) + Math.abs(r) + Math.abs(q + r)) / 2;

  let query = $state("");
  let searchEl = $state<HTMLInputElement | null>(null);
  let hovered = $state<string | null>(null);
  let peek = $state<{ label: string; kind: NodeKind } | null>(null);
  let width = $state(0);

  function onKey(e: KeyboardEvent) {
    const typing = e.target instanceof HTMLElement && ["INPUT", "TEXTAREA"].includes(e.target.tagName);
    if (e.key === "Escape" && query) {
      query = "";
      if (typing) (e.target as HTMLElement).blur();
    } else if (e.key === "/" && !typing) {
      e.preventDefault();
      searchEl?.focus();
    }
  }

  type Blob = {
    id: string;
    title: string;
    cells: Cell[];
    cx: number;
    cy: number;
    kinds: [NodeKind, number][];
    total: number;
    shares: string[];
  };

  const board = $derived.by(() => {
    // Shared resources first — they are the hub every stack reaches into.
    const sharedSeen = new Map<string, { kind: NodeKind; label: string }>();
    for (const g of groups) {
      for (const row of g.rows) {
        if (!SHARED_KINDS.has(row.node.kind)) continue;
        sharedSeen.set(row.node.id, { kind: row.node.kind, label: row.node.fullLabel ?? row.node.label });
      }
    }

    const stacks = groups
      .map((g) => ({
        id: g.id,
        title: g.title,
        items: g.rows
          .filter((r) => !SHARED_KINDS.has(r.node.kind))
          .map((r) => ({ kind: r.node.kind, label: r.node.fullLabel ?? r.node.label })),
        kinds: Object.entries(g.kinds) as [NodeKind, number][],
        shares: [...g.links.keys()],
      }))
      .filter((s) => s.items.length)
      .sort((a, b) => b.items.length - a.items.length);

    const needed = sharedSeen.size + stacks.reduce((n, s) => n + s.items.length, 0);
    // Room for the resources plus the gaps between blobs and a quiet margin.
    let radius = 3;
    while (3 * radius * radius + 3 * radius + 1 < needed * 2) radius++;

    const all = new Map<string, Cell>();
    const order: Cell[] = [];
    for (let q = -radius; q <= radius; q++) {
      for (let r = Math.max(-radius, -q - radius); r <= Math.min(radius, -q + radius); r++) {
        const cell: Cell = { q, r, key: key(q, r), x: 0, y: 0, stack: null, label: "", kind: null, shared: false };
        all.set(cell.key, cell);
        order.push(cell);
      }
    }
    order.sort((a, b) => dist(a.q, a.r) - dist(b.q, b.r) || Math.atan2(a.r, a.q) - Math.atan2(b.r, b.q));

    const free = (c: Cell | undefined): c is Cell => !!c && c.stack === null;
    const neighbours = (c: Axial) => DIRS.map((d) => all.get(key(c.q + d.q, c.r + d.r))).filter(Boolean) as Cell[];
    const touches = (c: Cell, id: string) => neighbours(c).some((n) => n.stack !== null && n.stack !== id);

    /** Claim `items.length` connected cells, hugging the seed. */
    function grow(id: string, items: { kind: NodeKind; label: string }[], seed: Cell, shared: boolean): Cell[] {
      const taken: Cell[] = [];
      const frontier: Cell[] = [seed];
      const queued = new Set([seed.key]);
      const from = (c: Cell) => dist(c.q - seed.q, c.r - seed.r);
      while (taken.length < items.length && frontier.length) {
        // Round outward from the seed; among equals, prefer cells that don't
        // butt up against a neighbouring stack.
        frontier.sort((a, b) => from(a) - from(b) || Number(touches(a, id)) - Number(touches(b, id)) || dist(a.q, a.r) - dist(b.q, b.r));
        let idx = frontier.findIndex((c) => free(c) && !touches(c, id));
        if (idx < 0) idx = frontier.findIndex(free);
        if (idx < 0) break;
        const cell = frontier.splice(idx, 1)[0];
        if (!free(cell)) continue;
        const item = items[taken.length];
        cell.stack = id;
        cell.kind = item.kind;
        cell.label = item.label;
        cell.shared = shared;
        taken.push(cell);
        for (const n of neighbours(cell)) if (free(n) && !queued.has(n.key)) { queued.add(n.key); frontier.push(n); }
      }
      return taken;
    }

    const blobs: Blob[] = [];
    if (sharedSeen.size) {
      const items = [...sharedSeen.values()].sort((a, b) => a.kind.localeCompare(b.kind) || a.label.localeCompare(b.label));
      const cells = grow("::shared", items, all.get(key(0, 0))!, true);
      const kinds = new Map<NodeKind, number>();
      for (const i of items) kinds.set(i.kind, (kinds.get(i.kind) ?? 0) + 1);
      blobs.push({ id: "::shared", title: "Shared", cells, cx: 0, cy: 0, kinds: [...kinds], total: items.length, shares: [] });
    }

    // Fan the stacks around the hub: each one aims at its own bearing, so the
    // board stays balanced instead of filling one side first.
    const GOLDEN = Math.PI * (3 - Math.sqrt(5));
    stacks.forEach((s, i) => {
      const aim = i * GOLDEN;
      const bearing = (c: Cell) => {
        const p = { x: SQRT3 * (c.q + c.r / 2), y: 1.5 * c.r };
        return Math.atan2(p.y, p.x);
      };
      const off = (c: Cell) => {
        const d = Math.abs(((bearing(c) - aim + Math.PI) % (2 * Math.PI) + 2 * Math.PI) % (2 * Math.PI) - Math.PI);
        return d;
      };
      const clear = (c: Cell) =>
        free(c) && !touches(c, s.id) && neighbours(c).every((n) => !touches(n, s.id) || n.stack !== null);
      const candidates = order.filter(clear);
      const pool = candidates.length ? candidates : order.filter((c) => free(c) && !touches(c, s.id));
      const seed = (pool.length ? pool : order.filter(free)).sort(
        (a, b) => dist(a.q, a.r) + off(a) * 1.6 - (dist(b.q, b.r) + off(b) * 1.6),
      )[0];
      if (!seed) return;
      const cells = grow(s.id, s.items, seed, false);
      blobs.push({ id: s.id, title: s.title, cells, cx: 0, cy: 0, kinds: s.kinds, total: s.items.length, shares: s.shares });
    });

    for (const b of blobs) {
      const low = Math.max(...b.cells.map((c) => c.r));
      const row = b.cells.filter((c) => c.r === low);
      b.cx = row.reduce((n, c) => n + c.q + c.r / 2, 0) / row.length;
      b.cy = low;
    }

    // Trim the board to the rings that are actually in use, plus one for air.
    const used = Math.max(1, ...blobs.flatMap((b) => b.cells.map((c) => dist(c.q, c.r))));
    const shown = [...all.values()].filter((c) => dist(c.q, c.r) <= Math.min(radius, used + 1));
    return { cells: shown, blobs, radius: Math.min(radius, used + 1) };
  });

  const SQRT3 = Math.sqrt(3);
  const size = $derived(Math.max(9, Math.min(38, width / (SQRT3 * (2 * board.radius + 2)))));
  const cx = $derived(SQRT3 * size * (board.radius + 0.5) + 2);
  const cy = $derived(1.5 * size * board.radius + size + 2);
  const boardW = $derived(cx * 2);
  // Extra room at the foot of the board for the blob captions.
  const boardH = $derived(cy * 2 + size * 0.9);
  const px = (c: Axial) => ({ x: cx + SQRT3 * size * (c.q + c.r / 2), y: cy + 1.5 * size * c.r });

  const corner = (c: Axial, i: number, scale = 0.92) => {
    const { x, y } = px(c);
    const a = (Math.PI / 180) * (60 * i - 30);
    return { x: x + size * scale * Math.cos(a), y: y + size * scale * Math.sin(a) };
  };

  function points(c: Axial): string {
    return Array.from({ length: 6 }, (_, i) => {
      const p = corner(c, i);
      return `${p.x.toFixed(2)},${p.y.toFixed(2)}`;
    }).join(" ");
  }

  /** Corner pair that forms the edge facing each neighbour direction. */
  const EDGE: [number, number][] = [
    [0, 1],
    [5, 0],
    [4, 5],
    [3, 4],
    [2, 3],
    [1, 2],
  ];

  /** Outline around a blob: every hex edge with no sibling on the far side. */
  function hull(cells: Cell[]): string {
    const own = new Set(cells.map((c) => c.key));
    let d = "";
    for (const c of cells) {
      DIRS.forEach((dir, i) => {
        if (own.has(key(c.q + dir.q, c.r + dir.r))) return;
        const a = corner(c, EDGE[i][0], 1.02);
        const b = corner(c, EDGE[i][1], 1.02);
        d += `M ${a.x.toFixed(2)} ${a.y.toFixed(2)} L ${b.x.toFixed(2)} ${b.y.toFixed(2)} `;
      });
    }
    return d;
  }

  const matches = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return null;
    const cells = new Set<string>();
    const stacks = new Set<string>();
    for (const b of board.blobs) {
      const titleHit = b.title.toLowerCase().includes(q);
      for (const c of b.cells) {
        if (!titleHit && !`${c.label} ${c.kind ? KIND_LABEL[c.kind] : ""}`.toLowerCase().includes(q)) continue;
        cells.add(c.key);
        stacks.add(b.id);
      }
    }
    return { cells, stacks };
  });

  const titleOf = $derived(new Map(groups.map((g) => [g.id, g.title])));
  const active = $derived(board.blobs.find((b) => b.id === hovered) ?? null);
  const related = $derived(new Set(active?.shares ?? []));
</script>

<svelte:window onkeydown={onKey} />

<div class="hex-view">
  <div class="toolbar">
    <label class="search" class:active={query}>
      <MagnifyingGlassIcon size={12} />
      <input bind:this={searchEl} bind:value={query} type="search" placeholder="Find a stack or resource" spellcheck="false" aria-label="Find a stack or resource" />
      {#if query}
        <button type="button" class="clear" onclick={() => (query = "")} aria-label="Clear search"><XIcon size={10} weight="bold" /></button>
      {:else}
        <kbd>/</kbd>
      {/if}
    </label>
    {#if matches}<span class="hits">{matches.cells.size} in {matches.stacks.size} stack{matches.stacks.size === 1 ? "" : "s"}</span>{/if}
    <span class="legend">
      {#each [["gateway", "Entry"], ["topic", "Events"], ["queue", "Queue"], ["function", "Compute"], ["dynamodb", "Data"], ["secret", "Shared"]] as const as [kind, label] (kind)}
        <span style:--c={kindVar(kind as NodeKind)}><i></i>{label}</span>
      {/each}
    </span>
  </div>

  <div class="board" bind:clientWidth={width}>
    {#if width}
      <svg viewBox="0 0 {boardW} {boardH}" width={boardW} height={boardH} role="presentation">
        {#each board.cells as cell (cell.key)}
          {#if cell.stack === null}
            <polygon class="empty" points={points(cell)} />
          {/if}
        {/each}

        {#each board.blobs as blob (blob.id)}
          <g
            class="blob"
            class:dim={(hovered && hovered !== blob.id && !related.has(blob.id)) || (matches && !matches.stacks.has(blob.id))}
            class:lit={hovered === blob.id}
            class:near={related.has(blob.id)}
            role="button"
            tabindex="0"
            aria-label="{blob.title}, {blob.total} resources"
            onmouseenter={() => (hovered = blob.id)}
            onmouseleave={() => { hovered = null; peek = null; }}
            onfocus={() => (hovered = blob.id)}
            onblur={() => { hovered = null; peek = null; }}
            onclick={() => blob.id !== "::shared" && onOpen(blob.id)}
            onkeydown={(e) => { if ((e.key === "Enter" || e.key === " ") && blob.id !== "::shared") { e.preventDefault(); onOpen(blob.id); } }}
          >
            <path class="hull" d={hull(blob.cells)} />
            {#each blob.cells as cell (cell.key)}
              <polygon
                class="cell"
                class:shared={cell.shared}
                class:hit={matches?.cells.has(cell.key)}
                style:--c={cell.kind ? kindVar(cell.kind) : "var(--text-tertiary)"}
                points={points(cell)}
                onmouseenter={() => (peek = cell.kind ? { label: cell.label, kind: cell.kind } : null)}
                role="presentation"
              >
                <title>{cell.kind ? KIND_LABEL[cell.kind] : ""} · {cell.label}</title>
              </polygon>
            {/each}
            {#if blob.cells.length >= 3 || hovered === blob.id}
              <text
                class="tag"
                x={cx + SQRT3 * size * blob.cx}
                y={cy + 1.5 * size * blob.cy + size * 1.35}
                text-anchor="middle"
              >{blob.id === "::shared" ? "shared" : blob.title}</text>
            {/if}
          </g>
        {/each}
      </svg>
    {/if}
  </div>

  <div class="caption" aria-live="polite">
    {#if active}
      <span class="cap-title">{active.id === "::shared" ? "Shared by every stack" : active.title}</span>
      <span class="cap-kinds">
        {#each active.kinds as [kind, n] (kind)}
          <span style:--c={kindVar(kind)}>{n} {KIND_LABEL[kind]}</span>
        {/each}
      </span>
      {#if peek}
        <span class="cap-peek" style:--c={kindVar(peek.kind)}><em>{KIND_LABEL[peek.kind]}</em>{peek.label}</span>
      {/if}
      {#if active.shares.length}
        <span class="cap-share">links to {active.shares.map((id) => titleOf.get(id) ?? id).join(", ")}</span>
      {/if}
    {:else}
      <span class="cap-idle">{board.blobs.length - 1} stacks · shared resources in the centre · click a cluster to open it</span>
    {/if}
  </div>
</div>

<style>
  .hex-view { display: flex; flex-direction: column; gap: 10px; }

  .toolbar { display: flex; align-items: center; gap: 10px; }
  .search {
    display: flex; align-items: center; gap: 7px; height: 26px; padding: 0 8px; border-radius: 7px;
    border: 1px solid var(--border-subtle); color: var(--text-tertiary); transition: border-color 140ms ease, color 140ms ease;
  }
  .search:focus-within, .search.active { border-color: var(--border-focus); color: var(--text-secondary); }
  .search input { width: 200px; background: none; border: 0; outline: none; font-size: 11.5px; color: var(--text-primary); }
  .search input::placeholder { color: var(--text-tertiary); }
  .search input::-webkit-search-cancel-button { display: none; }
  kbd { padding: 0 4px; border: 1px solid var(--border-subtle); border-radius: 4px; font: 9.5px/14px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .clear { display: inline-flex; color: var(--text-tertiary); }
  .clear:hover { color: var(--text-primary); }
  .hits { font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .legend { display: flex; flex-wrap: wrap; gap: 10px; margin-left: auto; }
  .legend span { display: inline-flex; align-items: center; gap: 5px; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); }
  .legend i {
    width: 9px; height: 10px; background: color-mix(in srgb, var(--c) 30%, transparent); border: 1px solid var(--c);
    clip-path: polygon(50% 0%, 100% 25%, 100% 75%, 50% 100%, 0% 75%, 0% 25%);
  }

  .board { display: flex; justify-content: center; }
  svg { max-width: 100%; height: auto; overflow: visible; }

  polygon { transition: opacity 180ms ease, fill 160ms ease, stroke 160ms ease; }
  .empty { fill: color-mix(in srgb, var(--text-primary) 5%, transparent); stroke: none; }

  .blob { cursor: pointer; outline: none; }
  .blob .cell {
    fill: color-mix(in srgb, var(--c) 50%, var(--bg-stage));
    stroke: color-mix(in srgb, var(--c) 88%, var(--bg-stage)); stroke-width: 1;
  }
  .blob.lit .cell { fill: color-mix(in srgb, var(--c) 78%, var(--bg-stage)); stroke: var(--c); }
  .blob.near .cell { fill: color-mix(in srgb, var(--c) 58%, var(--bg-stage)); stroke: var(--c); }
  .blob.dim { opacity: 0.34; }
  .blob:focus-visible .cell { stroke: var(--border-focus); stroke-width: 1.5; }
  .cell.shared { stroke-dasharray: 3 2.5; }
  .hull {
    fill: none; stroke: color-mix(in srgb, var(--text-primary) 62%, transparent); stroke-width: 1.5;
    stroke-linecap: round; transition: stroke 160ms ease;
  }
  .blob.lit .hull { stroke: var(--text-primary); stroke-width: 2; }
  .blob.near .hull { stroke: color-mix(in srgb, var(--text-primary) 62%, transparent); stroke-width: 1.75; }
  .cell.hit { stroke: var(--c); stroke-width: 1.75; }

  .tag {
    font: 9.5px var(--font-mono, ui-monospace, monospace); letter-spacing: 0.04em; fill: var(--text-secondary);
    paint-order: stroke; stroke: var(--bg-stage); stroke-width: 3px; stroke-linejoin: round; pointer-events: none;
  }
  .blob.lit .tag { fill: var(--text-primary); }

  .caption { display: flex; flex-wrap: wrap; align-items: baseline; justify-content: center; gap: 6px 12px; min-height: 18px; text-align: center; }
  .cap-title { font-size: 12.5px; font-weight: 600; color: var(--text-primary); }
  .cap-kinds { display: inline-flex; flex-wrap: wrap; gap: 8px; }
  .cap-kinds span { font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); border-left: 2px solid var(--c); padding-left: 5px; }
  .cap-peek { display: inline-flex; align-items: baseline; gap: 6px; font-size: 11.5px; color: var(--text-primary); }
  .cap-peek em { font-style: normal; font: 9.5px var(--font-mono, ui-monospace, monospace); letter-spacing: 0.05em; text-transform: uppercase; color: var(--c); }
  .cap-share, .cap-idle { font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }

  @media (prefers-reduced-motion: reduce) {
    polygon { transition: none; }
  }
</style>
