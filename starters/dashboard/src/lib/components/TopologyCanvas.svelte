<script lang="ts" module>
  import type { Tone } from "./StatusBadge.svelte";
  export interface FlowNode {
    id: string;
    label: string;
    detail: string;
    tone: Tone;
    x: number;
    y: number;
  }
  export interface FlowEdge {
    from: string;
    to: string;
  }
</script>

<script lang="ts">
  import {
    ArrowsOutSimpleIcon,
    ArrowsInSimpleIcon,
    ArrowsClockwiseIcon,
  } from "phosphor-svelte";
  import StatusBadge from "./StatusBadge.svelte";
  let {
    nodes,
    edges,
    onselect,
    expanded = $bindable(false),
  }: {
    nodes: readonly FlowNode[];
    edges: readonly FlowEdge[];
    onselect: (id: string) => void;
    expanded?: boolean;
  } = $props();
  let positions = $state<Record<string, { x: number; y: number }>>({});
  let svg = $state<SVGSVGElement>();
  let drag = $state<{
    id: string;
    startX: number;
    startY: number;
    x: number;
    y: number;
  } | null>(null);
  let moved = false;
  const placed = $derived(
    nodes.map((node) => ({ ...node, ...(positions[node.id] ?? {}) })),
  );
  const connections = $derived(
    edges.flatMap((edge) => {
      const from = placed.find((node) => node.id === edge.from);
      const to = placed.find((node) => node.id === edge.to);
      return from && to ? [{ from, to }] : [];
    }),
  );
  function startDrag(event: PointerEvent, node: FlowNode) {
    if (event.button !== 0) return;
    moved = false;
    drag = {
      id: node.id,
      startX: event.clientX,
      startY: event.clientY,
      x: node.x,
      y: node.y,
    };
  }
  function move(event: PointerEvent) {
    if (!drag || !svg) return;
    const bounds = svg.getBoundingClientRect();
    const scale = Math.min(bounds.width / 900, bounds.height / 460);
    if (
      Math.abs(event.clientX - drag.startX) +
        Math.abs(event.clientY - drag.startY) >
      3
    )
      moved = true;
    positions[drag.id] = {
      x: Math.max(
        8,
        Math.min(752, drag.x + (event.clientX - drag.startX) / scale),
      ),
      y: Math.max(
        8,
        Math.min(396, drag.y + (event.clientY - drag.startY) / scale),
      ),
    };
  }
  function moveWithKeyboard(event: KeyboardEvent, node: FlowNode) {
    if (
      !event.altKey ||
      !["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(event.key)
    )
      return;
    event.preventDefault();
    const dx =
      event.key === "ArrowLeft" ? -10 : event.key === "ArrowRight" ? 10 : 0;
    const dy =
      event.key === "ArrowUp" ? -10 : event.key === "ArrowDown" ? 10 : 0;
    positions[node.id] = {
      x: Math.max(8, Math.min(752, node.x + dx)),
      y: Math.max(8, Math.min(396, node.y + dy)),
    };
  }
</script>

<svelte:window
  onpointermove={move}
  onpointerup={() => (drag = null)}
  onpointercancel={() => (drag = null)}
/>
<section class="topology" class:expanded aria-label="Resource topology">
  <header>
    <p>Explore and arrange the connections in your workspace</p>
    <div class="canvas-actions">
      <button
        aria-label="Reset topology layout"
        title="Reset layout"
        onclick={() => (positions = {})}
        ><ArrowsClockwiseIcon size={13} aria-hidden="true" /></button
      ><button
        aria-label={expanded
          ? "Restore topology canvas"
          : "Expand topology canvas"}
        aria-pressed={expanded}
        title={expanded ? "Restore canvas" : "Expand canvas"}
        onclick={() => (expanded = !expanded)}
        >{#if expanded}<ArrowsInSimpleIcon
            size={13}
            aria-hidden="true"
          />{:else}<ArrowsOutSimpleIcon
            size={13}
            aria-hidden="true"
          />{/if}</button
      >
    </div>
  </header>
  <div class="canvas">
    <svg
      bind:this={svg}
      viewBox="0 0 900 460"
      aria-label="Worker connections. Drag a node to arrange it, or use Alt and arrow keys. Select a node to inspect it."
    >
      {#each connections as connection (connection.from.id + connection.to.id)}
        <path
          d={`M${connection.from.x + 140} ${connection.from.y + 28} C${connection.from.x + 210} ${connection.from.y + 28}, ${connection.to.x - 70} ${connection.to.y + 28}, ${connection.to.x} ${connection.to.y + 28}`}
        />
      {/each}
      {#each placed as node (node.id)}
        <foreignObject x={node.x} y={node.y} width="140" height="56">
          <button
            class="node"
            data-tone={node.tone}
            aria-label={`${node.label}, ${node.detail}`}
            title="Drag to arrange. Alt+arrow keys to move. Enter to inspect."
            onpointerdown={(event) => startDrag(event, node)}
            onkeydown={(event) => moveWithKeyboard(event, node)}
            onclick={(event) => {
              if (event.detail === 0 || !moved) onselect(node.id);
            }}><span>{node.label}</span><small>{node.detail}</small></button
          >
        </foreignObject>
      {/each}
    </svg>
  </div>
  <div class="mobile-flow">
    {#each nodes as node (node.id)}<button onclick={() => onselect(node.id)}
        ><span>{node.label}</span><StatusBadge
          label={node.detail}
          tone={node.tone}
        /></button
      >{/each}
  </div>
</section>

<style>
  .topology {
    min-width: 0;
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 8px;
  }
  header p {
    font: 10.5px var(--font-data);
    color: var(--ink-tertiary);
  }
  .canvas-actions {
    display: flex;
    gap: 4px;
  }
  .canvas-actions button {
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 1px solid transparent;
    border-radius: 5px;
    color: var(--ink-tertiary);
    background: var(--hover-fill);
  }
  .canvas-actions button:hover {
    color: var(--ink);
    border-color: var(--line-strong);
  }
  .canvas {
    height: min(540px, calc(100dvh - 260px));
    min-height: 320px;
    border: 1px solid var(--line);
    border-radius: 5px;
    background-color: var(--surface);
    background-image: radial-gradient(var(--grid-dot) 0.7px, transparent 0.7px);
    background-size: 10px 10px;
    overflow: hidden;
  }
  .expanded .canvas {
    height: calc(100dvh - 150px);
  }
  svg {
    display: block;
    width: 100%;
    height: 100%;
  }
  path {
    fill: none;
    stroke: var(--line-strong);
    stroke-width: 1.5;
  }
  .node {
    --node-tone: var(--ink-secondary);
    display: flex;
    flex-direction: column;
    justify-content: center;
    align-items: center;
    gap: 4px;
    width: 100%;
    height: 100%;
    border-radius: 6px;
    border: 1px solid color-mix(in srgb, var(--node-tone) 45%, transparent);
    color: var(--ink);
    background: var(--surface);
    touch-action: none;
    user-select: none;
    font: 12px var(--font-ui);
  }
  .node small {
    color: var(--node-tone);
    font: 9px var(--font-data);
  }
  .node:hover {
    border-color: var(--node-tone);
    background: var(--surface-raised);
  }
  .node:focus-visible {
    outline-offset: -3px;
  }
  [data-tone="success"] {
    --node-tone: var(--success);
  }
  [data-tone="warning"] {
    --node-tone: var(--warning);
  }
  [data-tone="danger"] {
    --node-tone: var(--danger);
  }
  [data-tone="info"] {
    --node-tone: var(--info);
  }
  .mobile-flow {
    display: none;
  }
  @media (max-width: 640px) {
    .canvas,
    .canvas-actions {
      display: none;
    }
    .mobile-flow {
      display: grid;
      border-top: 1px solid var(--line);
    }
    .mobile-flow > button {
      min-height: 52px;
      padding: 12px;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 8px;
      border: 0;
      border-bottom: 1px solid var(--line);
      background: transparent;
      color: var(--ink);
    }
    header p {
      font-family: var(--font-ui);
      font-size: 12px;
    }
  }
</style>
