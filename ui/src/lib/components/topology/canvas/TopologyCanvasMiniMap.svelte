<script lang="ts">
  import {
    nodeBounds,
    type TopologyGraphModel,
    type ViewportTransform,
  } from "../topology-connection-model";
  import {
    infraKindColor,
    kindColor,
    type TopologyCanvasPalette,
  } from "../topology-canvas-theme";
  import type { ConnectionNode } from "../types";

  const MINI_MAP_WIDTH = 220;
  const MINI_MAP_HEIGHT = 132;
  const MINI_MAP_PADDING = 10;

  let {
    model,
    palette,
    viewportTransform,
    viewportWidth,
    viewportHeight,
    onFocusCanvas = (_x: number, _y: number) => {},
  }: {
    model: TopologyGraphModel;
    palette: TopologyCanvasPalette;
    viewportTransform: ViewportTransform;
    viewportWidth: number;
    viewportHeight: number;
    onFocusCanvas?: (x: number, y: number) => void;
  } = $props();

  const plotWidth = MINI_MAP_WIDTH - MINI_MAP_PADDING * 2;
  const plotHeight = MINI_MAP_HEIGHT - MINI_MAP_PADDING * 2;

  let miniMapElement = $state<HTMLDivElement | null>(null);

  const allNodes = $derived(
    model.allNodes.map((node) => ({
      node,
      stroke:
        node.kind === "infra"
          ? node.status === "connected"
            ? infraKindColor(model.infraById.get(node.id)?.kind ?? "", palette)
            : palette.destructive
          : kindColor(node.kind, palette),
    })),
  );

  const allEdges = $derived(
    model.allEdges.map((edge, index) => ({
      edge,
      miniMapKey: `${edge.from.kind}:${edge.from.id}→${edge.to.kind}:${edge.to.id}:${index}`,
    })),
  );

  const visibleRect = $derived.by(() => {
    const cw = model.canvasSize.width;
    const ch = model.canvasSize.height;
    const left = clamp((0 - viewportTransform.offsetX) / viewportTransform.scale, 0, cw);
    const top = clamp((0 - viewportTransform.offsetY) / viewportTransform.scale, 0, ch);
    const right = clamp(
      (viewportWidth - viewportTransform.offsetX) / viewportTransform.scale,
      0,
      cw,
    );
    const bottom = clamp(
      (viewportHeight - viewportTransform.offsetY) / viewportTransform.scale,
      0,
      ch,
    );
    return { left, top, right, bottom };
  });

  const showMiniMap = $derived(
    model.hasData &&
      (visibleRect.left > 1 ||
        visibleRect.top > 1 ||
        visibleRect.right < model.canvasSize.width - 1 ||
        visibleRect.bottom < model.canvasSize.height - 1),
  );

  function minimapX(x: number): number {
    return MINI_MAP_PADDING + (x / model.canvasSize.width) * plotWidth;
  }

  function minimapY(y: number): number {
    return MINI_MAP_PADDING + (y / model.canvasSize.height) * plotHeight;
  }

  function handlePointerDown(event: PointerEvent) {
    if (!miniMapElement) return;
    const rect = miniMapElement.getBoundingClientRect();
    const localX = clamp(event.clientX - rect.left, MINI_MAP_PADDING, MINI_MAP_PADDING + plotWidth);
    const localY = clamp(event.clientY - rect.top, MINI_MAP_PADDING, MINI_MAP_PADDING + plotHeight);
    onFocusCanvas(
      ((localX - MINI_MAP_PADDING) / plotWidth) * model.canvasSize.width,
      ((localY - MINI_MAP_PADDING) / plotHeight) * model.canvasSize.height,
    );
  }

  function clamp(value: number, min: number, max: number): number {
    return Math.min(max, Math.max(min, value));
  }

  function minimapNodeRect(node: ConnectionNode) {
    const bounds = nodeBounds(node);
    const x = minimapX(bounds.left);
    const y = minimapY(bounds.top);
    const width = Math.max(4, minimapX(bounds.right) - minimapX(bounds.left));
    const height = Math.max(3, minimapY(bounds.bottom) - minimapY(bounds.top));
    return { x, y, width, height };
  }
</script>

{#if showMiniMap}
  <div
    bind:this={miniMapElement}
    class="absolute bottom-3 right-3 z-20"
    role="button"
    tabindex="0"
    aria-label="Mini-map"
    onpointerdown={handlePointerDown}
  >
    <svg
      width={MINI_MAP_WIDTH}
      height={MINI_MAP_HEIGHT}
      viewBox={`0 0 ${MINI_MAP_WIDTH} ${MINI_MAP_HEIGHT}`}
      class="block"
    >
      <rect
        x={0.5}
        y={0.5}
        width={MINI_MAP_WIDTH - 1}
        height={MINI_MAP_HEIGHT - 1}
        rx={16}
        fill={palette.popover}
        fill-opacity={0.82}
        stroke={palette.border}
      />

      {#each allEdges as { edge, miniMapKey } (miniMapKey)}
        <line
          x1={minimapX(edge.from.x)}
          y1={minimapY(edge.from.y)}
          x2={minimapX(edge.to.x)}
          y2={minimapY(edge.to.y)}
          stroke={palette.mutedForeground}
          stroke-opacity={palette.isDark ? 0.26 : 0.18}
          stroke-width={1}
          stroke-linecap="round"
        />
      {/each}

      {#each allNodes as { node, stroke } (`${node.kind}:${node.id}`)}
        {@const rect = minimapNodeRect(node)}
        <rect
          x={rect.x}
          y={rect.y}
          width={rect.width}
          height={rect.height}
          rx={3}
          fill={palette.background}
          fill-opacity={0.95}
          stroke={stroke}
          stroke-width={1.2}
        />
      {/each}

      <rect
        x={minimapX(visibleRect.left)}
        y={minimapY(visibleRect.top)}
        width={Math.max(
          14,
          minimapX(visibleRect.right) - minimapX(visibleRect.left),
        )}
        height={Math.max(
          10,
          minimapY(visibleRect.bottom) - minimapY(visibleRect.top),
        )}
        rx={8}
        fill={palette.warning}
        fill-opacity={0.08}
        stroke={palette.warning}
        stroke-width={1.3}
      />
    </svg>
  </div>
{/if}
