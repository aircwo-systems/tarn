<script lang="ts">
  import { ArrowRightIcon, MagnifyingGlassIcon, XIcon } from "phosphor-svelte";
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
   * One square per stack, each holding a small honeycomb of its resources:
   * the grouping of the bento with the shape-reading of the map.
   */

  type Axial = { q: number; r: number };
  type Hex = Axial & { key: string; kind: NodeKind; label: string; shared: boolean; x: number; y: number };
  type Card = {
    id: string;
    title: string;
    hexes: Hex[];
    own: number;
    shared: number;
    kinds: [NodeKind, number][];
    w: number;
    h: number;
    span: 1 | 2 | 3;
  };

  const DIRS: Axial[] = [
    { q: 1, r: 0 },
    { q: 1, r: -1 },
    { q: 0, r: -1 },
    { q: -1, r: 0 },
    { q: -1, r: 1 },
    { q: 0, r: 1 },
  ];
  const dist = (q: number, r: number) => (Math.abs(q) + Math.abs(r) + Math.abs(q + r)) / 2;

  /** Resources read in request order, so a cluster grows entry → data. */
  const RANK: Record<NodeKind, number> = {
    gateway: 0,
    eventbridge: 1,
    topic: 2,
    bucket: 3,
    queue: 4,
    function: 5,
    dynamodb: 6,
    extension: 7,
    infra: 8,
    secret: 9,
  };

  const SIZE = 15;
  const SQRT3 = Math.sqrt(3);

  let query = $state("");
  let searchEl = $state<HTMLInputElement | null>(null);
  let hovered = $state<string | null>(null);
  let peek = $state<{ card: string; label: string; kind: NodeKind } | null>(null);

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

  /** Spiral of axial coordinates, centre outwards — a compact honeycomb. */
  function spiral(n: number): Axial[] {
    const out: Axial[] = [{ q: 0, r: 0 }];
    for (let ring = 1; out.length < n; ring++) {
      let c = { q: -ring, r: ring };
      for (let side = 0; side < 6; side++) {
        for (let step = 0; step < ring; step++) {
          out.push({ ...c });
          c = { q: c.q + DIRS[side].q, r: c.r + DIRS[side].r };
        }
      }
    }
    return out.slice(0, n);
  }

  const cards = $derived.by<Card[]>(() =>
    groups.map((g) => {
      const items = g.rows
        .map((r) => ({ kind: r.node.kind, label: r.node.fullLabel ?? r.node.label, shared: SHARED_KINDS.has(r.node.kind) }))
        .sort((a, b) => RANK[a.kind] - RANK[b.kind] || a.label.localeCompare(b.label));

      const cells = spiral(items.length).sort(
        (a, b) => dist(a.q, a.r) - dist(b.q, b.r) || Math.atan2(a.r, a.q + a.r / 2) - Math.atan2(b.r, b.q + b.r / 2),
      );

      const raw = items.map((item, i) => ({
        ...item,
        ...cells[i],
        key: `${cells[i].q},${cells[i].r}`,
        x: SQRT3 * SIZE * (cells[i].q + cells[i].r / 2),
        y: 1.5 * SIZE * cells[i].r,
      }));

      const minX = Math.min(...raw.map((h) => h.x)) - SIZE;
      const minY = Math.min(...raw.map((h) => h.y)) - SIZE;
      const hexes = raw.map((h) => ({ ...h, x: h.x - minX, y: h.y - minY }));
      const own = items.filter((i) => !i.shared).length;
      return {
        id: g.id,
        title: g.title,
        hexes,
        own,
        shared: items.length - own,
        kinds: Object.entries(g.kinds) as [NodeKind, number][],
        w: Math.max(...hexes.map((h) => h.x)) + SIZE,
        h: Math.max(...hexes.map((h) => h.y)) + SIZE,
        span: (items.length >= 9 ? 2 : 1) as 1 | 2 | 3,
      };
    }),
  );

  function points(h: Hex): string {
    return Array.from({ length: 6 }, (_, i) => {
      const a = (Math.PI / 180) * (60 * i - 30);
      return `${(h.x + SIZE * 0.9 * Math.cos(a)).toFixed(2)},${(h.y + SIZE * 0.9 * Math.sin(a)).toFixed(2)}`;
    }).join(" ");
  }

  const matches = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return null;
    const cards_ = new Set<string>();
    const hexes = new Set<string>();
    for (const c of cards) {
      const titleHit = c.title.toLowerCase().includes(q);
      if (titleHit) cards_.add(c.id);
      for (const h of c.hexes) {
        if (!`${h.label} ${KIND_LABEL[h.kind]}`.toLowerCase().includes(q)) continue;
        hexes.add(`${c.id}|${h.key}`);
        cards_.add(c.id);
      }
    }
    return { cards: cards_, hexes };
  });

  const related = $derived.by(() => {
    if (!hovered) return null;
    const g = groups.find((x) => x.id === hovered);
    return g ? new Set(g.links.keys()) : null;
  });

  const titleOf = $derived(new Map(groups.map((g) => [g.id, g.title])));
</script>

<svelte:window onkeydown={onKey} />

<div class="hexcards">
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
    {#if matches}<span class="hits">{matches.hexes.size} in {matches.cards.size} stack{matches.cards.size === 1 ? "" : "s"}</span>{/if}
    <span class="legend">
      {#each [["gateway", "Entry"], ["topic", "Events"], ["queue", "Queue"], ["function", "Compute"], ["dynamodb", "Data"], ["secret", "Shared"]] as const as [kind, label] (kind)}
        <span style:--c={kindVar(kind as NodeKind)}><i></i>{label}</span>
      {/each}
    </span>
  </div>

  <div class="grid">
    {#each cards as card, i (card.id)}
      <button
        type="button"
        class="card"
        class:faint={matches && !matches.cards.has(card.id)}
        class:related={related?.has(card.id)}
        style:--span={card.span}
        style:--i={i}
        aria-label="{card.title}, {card.own} resources{card.shared ? `, ${card.shared} shared` : ''}"
        onmouseenter={() => (hovered = card.id)}
        onmouseleave={() => { hovered = null; peek = null; }}
        onfocus={() => (hovered = card.id)}
        onblur={() => { hovered = hovered === card.id ? null : hovered; peek = null; }}
        onclick={() => onOpen(card.id)}
      >
        <span class="strip" aria-hidden="true">
          {#each card.kinds as [kind, n] (kind)}
            <span style:flex-grow={n} style:background={kindVar(kind)}></span>
          {/each}
        </span>

        <span class="head">
          <span class="title" title={card.title}>{card.title}</span>
          {#if peek && peek.card === card.id}
            <span class="peek" style:--c={kindVar(peek.kind)}>
              <em>{KIND_LABEL[peek.kind]}</em>{peek.label}
            </span>
          {:else}
            <span class="count">{card.own}{#if card.shared}<i>+{card.shared}</i>{/if}</span>
          {/if}
          <span class="enter" aria-hidden="true"><ArrowRightIcon size={11} weight="bold" /></span>
        </span>

        <span class="comb">
          <svg
            viewBox="0 0 {card.w} {card.h}"
            preserveAspectRatio="xMidYMid meet"
            style:max-width="{card.w * 1.5}px"
            style:max-height="{card.h * 1.5}px"
            aria-hidden="true"
          >
            {#each card.hexes as h (h.key)}
              <polygon
                class="hex"
                class:shared={h.shared}
                class:hit={matches?.hexes.has(`${card.id}|${h.key}`)}
                class:on={peek?.card === card.id && peek.label === h.label}
                style:--c={kindVar(h.kind)}
                points={points(h)}
                onmouseenter={() => (peek = { card: card.id, label: h.label, kind: h.kind })}
                role="presentation"
              ><title>{KIND_LABEL[h.kind]} · {h.label}</title></polygon>
            {/each}
          </svg>
        </span>
      </button>
    {/each}
  </div>

  {#if related?.size && hovered}
    <p class="relnote">
      {titleOf.get(hovered)} shares resources with {[...related].map((id) => titleOf.get(id) ?? id).join(", ")}
    </p>
  {/if}
</div>

<style>
  .hexcards { display: flex; flex-direction: column; gap: 12px; }

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
    width: 9px; height: 10px; background: color-mix(in srgb, var(--c) 40%, transparent); border: 1px solid var(--c);
    clip-path: polygon(50% 0%, 100% 25%, 100% 75%, 50% 100%, 0% 75%, 0% 25%);
  }

  .grid {
    display: grid; grid-template-columns: repeat(auto-fill, minmax(208px, 1fr)); align-items: start;
    grid-auto-flow: row dense; gap: 10px; width: 100%; max-width: 1120px; margin: 0 auto;
  }

  .card {
    position: relative; grid-column: span min(var(--span), 3);
    display: flex; flex-direction: column; gap: 10px; padding: 13px 12px 12px; text-align: left; min-width: 0;
    border: 1px solid var(--border-subtle); border-radius: 10px; background: var(--bg-stage); overflow: hidden;
    transition: border-color 160ms ease, transform 160ms var(--ease-snappy), opacity 160ms ease;
    animation: cardIn 260ms var(--ease-snappy) both; animation-delay: calc(var(--i) * 28ms);
  }
  .card:hover { border-color: color-mix(in srgb, var(--text-primary) 22%, transparent); transform: translateY(-1px); }
  .card:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }
  .card.related { border-color: var(--stack-infra); }
  .faint { opacity: 0.28; }
  @keyframes cardIn { from { opacity: 0; transform: translateY(5px); } }

  .strip { position: absolute; inset: 0 0 auto; display: flex; gap: 1px; height: 2px; opacity: 0.9; }
  .strip span { min-width: 2px; }

  .head { display: flex; align-items: baseline; gap: 8px; min-width: 0; }
  .title { flex-shrink: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; font-weight: 600; color: var(--text-primary); }
  .count { flex-shrink: 0; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .count i { font-style: normal; margin-left: 3px; color: var(--stack-secret); }
  .peek { display: inline-flex; align-items: baseline; gap: 6px; min-width: 0; overflow: hidden; white-space: nowrap; font-size: 11px; color: var(--text-secondary); }
  .peek em { font-style: normal; font: 9px var(--font-mono, ui-monospace, monospace); letter-spacing: 0.05em; text-transform: uppercase; color: var(--c); }
  .enter { display: inline-flex; margin-left: auto; color: var(--text-tertiary); opacity: 0; transform: translateX(-3px); transition: opacity 160ms ease, transform 160ms var(--ease-snappy); }
  .card:hover .enter, .card:focus-visible .enter { opacity: 1; transform: none; }

  .comb { display: flex; justify-content: center; align-items: center; flex: 1; min-height: 88px; }
  svg { width: 100%; height: auto; overflow: visible; }
  .hex {
    fill: color-mix(in srgb, var(--c) 46%, var(--bg-stage));
    stroke: color-mix(in srgb, var(--c) 88%, var(--bg-stage)); stroke-width: 1;
    transition: fill 140ms ease, stroke 140ms ease;
  }
  .card:hover .hex, .card:focus-visible .hex { fill: color-mix(in srgb, var(--c) 62%, var(--bg-stage)); stroke: var(--c); }
  .hex.shared { stroke-dasharray: 3 2.5; }
  .hex.on { fill: color-mix(in srgb, var(--c) 85%, var(--bg-stage)); stroke: var(--c); }
  .hex.hit { stroke: var(--c); stroke-width: 2; }

  .relnote { text-align: center; font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }

  @media (prefers-reduced-motion: reduce) {
    .card { animation: none; transition: border-color 160ms ease; }
    .card:hover { transform: none; }
    .hex { transition: none; }
  }
</style>
