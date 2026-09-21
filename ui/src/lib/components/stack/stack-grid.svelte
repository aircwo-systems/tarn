<script lang="ts">
  import { ArrowRightIcon, MagnifyingGlassIcon, XIcon } from "phosphor-svelte";
  import type { NodeKind } from "$lib/components/topology/types";
  import { KIND_ICON } from "./stack-icons";
  import { KIND_LABEL, kindVar, SHARED_KINDS, type StackGroup } from "./stack-model";

  let {
    groups,
    onOpen,
  }: {
    groups: StackGroup[];
    onOpen: (id: string) => void;
  } = $props();

  type Tile = { id: string; kind: NodeKind; label: string; sub: string; shared: boolean };
  type Stage = { id: string; label: string; tiles: Tile[] };
  type Card = {
    id: string;
    title: string;
    stages: Stage[];
    tiles: Tile[];
    own: number;
    lead: NodeKind;
    shared: Tile[];
    kinds: [NodeKind, number][];
    span: 1 | 2 | 3;
  };

  /** Cubes cluster by pipeline stage, so a card reads left→right as a flow. */
  const STAGES: { id: string; label: string; kinds: NodeKind[] }[] = [
    { id: "entry", label: "Entry", kinds: ["gateway", "eventbridge", "bucket", "topic"] },
    { id: "queue", label: "Queue", kinds: ["queue"] },
    { id: "compute", label: "Compute", kinds: ["function"] },
    { id: "data", label: "Data", kinds: ["dynamodb"] },
  ];

  let query = $state("");
  let searchEl = $state<HTMLInputElement | null>(null);
  let hovered = $state<string | null>(null);
  /** The cube under the pointer or keyboard focus, named in the card header. */
  let peek = $state<{ card: string; tile: Tile } | null>(null);

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

  // A stack's footprint follows its size, so the grid reads as a map of where
  // the weight sits rather than a uniform wall of cards.
  const cards = $derived.by<Card[]>(() =>
    groups.map((g) => {
      const tiles: Tile[] = g.rows.map((r) => ({
        id: r.node.id,
        kind: r.node.kind,
        label: r.node.fullLabel ?? r.node.label,
        sub: r.node.sub,
        shared: SHARED_KINDS.has(r.node.kind),
      }));
      const ownTiles = tiles.filter((t) => !t.shared);
      const own = ownTiles.length;
      const span = own >= 10 ? 3 : own >= 5 ? 2 : 1;
      const stages = STAGES.map((st) => ({
        id: st.id,
        label: st.label,
        tiles: ownTiles.filter((t) => st.kinds.includes(t.kind)),
      })).filter((st) => st.tiles.length);
      return {
        id: g.id,
        title: g.title,
        stages,
        tiles: ownTiles,
        own,
        lead: (stages[0]?.tiles[0]?.kind ?? "infra") as NodeKind,
        shared: tiles.filter((t) => t.shared),
        kinds: Object.entries(g.kinds) as [NodeKind, number][],
        span: span as 1 | 2 | 3,
      };
    }),
  );

  const matches = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return null;
    const ids = new Set<string>();
    for (const c of cards) {
      const hit = c.title.toLowerCase().includes(q) || c.tiles.some((t) => `${t.label} ${KIND_LABEL[t.kind]}`.toLowerCase().includes(q));
      if (hit) ids.add(c.id);
    }
    return ids;
  });

  const tileHit = (t: Tile) => {
    const q = query.trim().toLowerCase();
    return q ? `${t.label} ${KIND_LABEL[t.kind]}`.toLowerCase().includes(q) : false;
  };

  /** Stacks that share a resource with the hovered one. */
  const related = $derived.by(() => {
    if (!hovered) return null;
    const g = groups.find((x) => x.id === hovered);
    return g ? new Set(g.links.keys()) : null;
  });

  const titleOf = $derived(new Map(groups.map((g) => [g.id, g.title])));
</script>

<svelte:window onkeydown={onKey} />

<div class="grid-view">
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
    {#if matches}<span class="hits">{matches.size} stack{matches.size === 1 ? "" : "s"}</span>{/if}
    <span class="legend">
      {#each [["gateway", "Entry"], ["queue", "Queue"], ["function", "Compute"], ["dynamodb", "Data"], ["infra", "External"], ["secret", "Secrets"]] as const as [kind, label] (kind)}
        {@const Icon = KIND_ICON[kind as NodeKind]}
        <span style:--c={kindVar(kind as NodeKind)}><Icon size={11} weight="bold" />{label}</span>
      {/each}
    </span>
  </div>

  <div class="bento">
    {#each cards as card, i (card.id)}
      <button
        type="button"
        class="bcard"
        class:faint={matches && !matches.has(card.id)}
        class:related={related?.has(card.id)}
        aria-label="{card.title}, {card.own} resources{card.shared.length ? `, ${card.shared.length} shared` : ''}"
        style:--span={card.span}
        style:--i={i}
        onmouseenter={() => (hovered = card.id)}
        onmouseleave={() => (hovered = null)}
        onfocus={() => (hovered = card.id)}
        onblur={() => { hovered = hovered === card.id ? null : hovered; peek = null; }}
        onclick={() => onOpen(card.id)}
      >
        <span class="strip" aria-hidden="true">
          {#each card.kinds as [kind, n] (kind)}
            <span style:flex-grow={n} style:background={kindVar(kind)}></span>
          {/each}
        </span>

        <span class="bhead">
          <span class="btitle" title={card.title}>{card.title}</span>
          {#if peek && peek.card === card.id}
            <span class="peek" style:--c={kindVar(peek.tile.kind)}>
              <span class="peek-kind">{KIND_LABEL[peek.tile.kind]}</span>
              <span class="peek-name">{peek.tile.label}</span>
            </span>
          {:else}
            <span class="bcount">{card.own}</span>
          {/if}
          <span class="enter" aria-hidden="true"><ArrowRightIcon size={11} weight="bold" /></span>
        </span>

        <span class="stages">
          {#each card.stages as stage, si (stage.id)}
            <span class="stage" class:after={si > 0}>
              <span class="stage-label">{stage.label}<em>{stage.tiles.length}</em></span>
              <span class="cubes">
                {#each stage.tiles as t (t.id)}
                  {@const Icon = KIND_ICON[t.kind]}
                  <span
                    class="cube"
                    class:hit={tileHit(t)}
                    class:on={peek?.tile.id === t.id}
                    style:--c={kindVar(t.kind)}
                    title="{KIND_LABEL[t.kind]} · {t.label}"
                    aria-hidden="true"
                    onmouseenter={() => (peek = { card: card.id, tile: t })}
                    onmouseleave={() => (peek = null)}
                  >
                    <Icon size={13} weight="bold" />
                  </span>
                {/each}
              </span>
            </span>
          {/each}
        </span>

        {#if card.shared.length}
          <span class="bfoot">
            <span class="bshared-label">shared</span>
            <span class="sharedset">
              {#each card.shared.slice(0, 3) as t (t.id)}
                {@const Icon = KIND_ICON[t.kind]}
                <span
                  class="scube"
                  style:--c={kindVar(t.kind)}
                  title="Shared · {KIND_LABEL[t.kind]} · {t.label}"
                  aria-hidden="true"
                  onmouseenter={() => (peek = { card: card.id, tile: t })}
                  onmouseleave={() => (peek = null)}
                ><Icon size={11} /></span>
              {/each}
              {#if card.shared.length > 3}<span class="more">+{card.shared.length - 3}</span>{/if}
            </span>
          </span>
        {/if}
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
  .grid-view { display: flex; flex-direction: column; gap: 12px; }

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
  .legend span { display: inline-flex; align-items: center; gap: 4px; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); }
  .legend :global(svg) { color: var(--c); }

  .bento {
    display: grid; grid-template-columns: repeat(auto-fill, minmax(196px, 1fr)); grid-auto-rows: min-content;
    align-items: start; grid-auto-flow: row dense; gap: 10px; width: 100%; max-width: 1120px; margin: 0 auto;
  }

  .bcard {
    position: relative; grid-column: span min(var(--span), 3);
    display: flex; flex-direction: column; gap: 10px; padding: 13px 12px 11px; text-align: left; min-width: 0;
    border: 1px solid var(--border-subtle); border-radius: 10px; background: var(--bg-stage); overflow: hidden;
    transition: border-color 160ms ease, transform 160ms var(--ease-snappy), opacity 160ms ease;
    animation: cardIn 260ms var(--ease-snappy) both; animation-delay: calc(var(--i) * 28ms);
  }
  .bcard:hover { border-color: color-mix(in srgb, var(--text-primary) 22%, transparent); transform: translateY(-1px); }
  .bcard:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }
  .bcard.related { border-color: var(--stack-infra); }
  .faint { opacity: 0.28; }
  @keyframes cardIn { from { opacity: 0; transform: translateY(5px); } }

  .bhead { display: flex; align-items: center; gap: 7px; min-width: 0; }
  .btitle { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; font-weight: 600; color: var(--text-primary); }
  .bcount { flex-shrink: 0; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .enter { display: inline-flex; margin-left: auto; color: var(--text-tertiary); opacity: 0; transform: translateX(-3px); transition: opacity 160ms ease, transform 160ms var(--ease-snappy); }
  .bcard:hover .enter, .bcard:focus-visible .enter { opacity: 1; transform: none; }

  .strip { position: absolute; inset: 0 0 auto; display: flex; gap: 1px; height: 2px; opacity: 0.9; }
  .strip span { min-width: 2px; }

  .stages { display: flex; flex-wrap: wrap; align-items: stretch; align-content: flex-start; gap: 8px 0; flex: 1; min-width: 0; }
  .stage {
    position: relative; flex: 0 1 auto; display: flex; flex-direction: column; align-items: center; gap: 6px;
    min-width: 0; padding: 6px 7px; border-radius: 10px;
    background: color-mix(in srgb, var(--text-primary) 3.5%, transparent);
    transition: background 160ms ease;
  }
  .bcard:hover .stage, .bcard:focus-visible .stage { background: color-mix(in srgb, var(--text-primary) 6%, transparent); }
  .stage-label {
    display: inline-flex; align-items: baseline; gap: 4px; max-width: 100%;
    font: 8.5px/1 var(--font-mono, ui-monospace, monospace); letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-tertiary);
  }
  .stage-label em { font-style: normal; color: var(--text-secondary); }
  .stage.after { margin-left: 18px; }
  .stage.after::before {
    content: "›"; position: absolute; left: -18px; top: 0; bottom: 0; width: 18px;
    display: flex; align-items: center; justify-content: center;
    font-size: 13px; line-height: 1; color: var(--text-tertiary); opacity: 0.55;
  }
  .cubes { display: flex; flex-wrap: wrap; justify-content: center; align-content: flex-start; gap: 5px; min-width: 0; }
  .cube {
    display: inline-flex; align-items: center; justify-content: center; width: 26px; height: 26px; border-radius: 8px;
    border: 1px solid var(--c); background: color-mix(in srgb, var(--c) 12%, var(--bg-stage)); color: var(--c);
    transition: transform 160ms var(--ease-snappy), background 140ms ease;
  }
  .bcard:hover .cube, .bcard:focus-visible .cube { transform: translateY(-1px); }
  .bcard:hover .cube, .bcard:focus-visible .cube { background: color-mix(in srgb, var(--c) 20%, var(--bg-stage)); }
  .cube.on { background: color-mix(in srgb, var(--c) 32%, var(--bg-stage)); transform: translateY(-2px); }
  .cube.hit { outline: 1px solid var(--c); outline-offset: 1px; }

  .bfoot { display: flex; align-items: center; gap: 7px; margin-top: auto; }
  .bshared-label { font: 9px var(--font-mono, ui-monospace, monospace); letter-spacing: 0.07em; text-transform: uppercase; color: var(--text-tertiary); }
  .sharedset { display: inline-flex; align-items: center; gap: 4px; }
  .scube {
    display: inline-flex; align-items: center; justify-content: center; width: 18px; height: 18px; border-radius: 5px;
    border: 1px dashed var(--c); color: var(--c);
  }
  .more { font: 9.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }

  .peek { display: inline-flex; align-items: baseline; gap: 6px; min-width: 0; overflow: hidden; white-space: nowrap; animation: peekIn 130ms var(--ease-snappy) both; }
  @keyframes peekIn { from { opacity: 0; transform: translateX(-3px); } }
  .btitle { flex-shrink: 1; }
  .peek-kind { font: 9.5px var(--font-mono, ui-monospace, monospace); letter-spacing: 0.05em; text-transform: uppercase; color: var(--c); }
  .peek-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; font-size: 12px; font-weight: 600; color: var(--text-primary); }

  .relnote { text-align: center; font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }

  @media (prefers-reduced-motion: reduce) {
    .bcard { animation: none; transition: border-color 160ms ease; }
    .bcard:hover { transform: none; }
    .cube, .bcard:hover .cube, .cube.on { transform: none; transition: background 140ms ease; }
    .peek { animation: none; }
  }
</style>
