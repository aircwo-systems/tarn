<script lang="ts">
  import { onMount } from "svelte";
  import {
    ArrowSquareDownIcon,
    ArrowSquareLeftIcon,
    ArrowSquareRightIcon,
    ArrowSquareUpIcon,
  } from "phosphor-svelte";
  import { Canvas, type CanvasResizeEvent } from "svelte-canvas";
  import Badge from "$lib/components/ui/badge/badge.svelte";
  import Button from "$lib/components/ui/button/button.svelte";
  import type { RequestTrace } from "$lib/types";
  import type { ConnectionNode } from "../types";
  import {
    clampInfraNodePosition,
    clampViewportTransform,
    CONNECTION_CANVAS,
    computeViewportTransform,
    findNodeAt,
    hoverFocusState,
    viewportToCanvasPoint,
    type InfraNodePosition,
    type TopologyGraphModel,
    type ViewportTransform,
  } from "../topology-connection-model";
  import TopologyCanvasBackgroundLayer from "./TopologyCanvasBackgroundLayer.svelte";
  import TopologyCanvasEdgeLayer from "./TopologyCanvasEdgeLayer.svelte";
  import TopologyCanvasNodeLayer from "./TopologyCanvasNodeLayer.svelte";
  import {
    readTopologyCanvasPalette,
    type TopologyCanvasPalette,
  } from "../topology-canvas-theme";

  export interface TopologyNodeHoverPayload {
    node: ConnectionNode;
    clientX: number;
    clientY: number;
  }

  let {
    model,
    selectedTrace = null,
    canvasExpanded = false,
    panEnabled = false,
    viewportResetToken = 0,
    onGatewayClick = (_id: string) => {},
    onNodeHover = (_payload: TopologyNodeHoverPayload) => {},
    onNodeLeave = () => {},
    onInfraNodePositionChange = (
      _id: string,
      _position: InfraNodePosition,
    ) => {},
  }: {
    model: TopologyGraphModel;
    selectedTrace?: RequestTrace | null;
    canvasExpanded?: boolean;
    panEnabled?: boolean;
    viewportResetToken?: number;
    onGatewayClick?: (id: string) => void;
    onNodeHover?: (payload: TopologyNodeHoverPayload) => void;
    onNodeLeave?: () => void;
    onInfraNodePositionChange?: (
      id: string,
      position: InfraNodePosition,
    ) => void;
  } = $props();

  type PointerInteraction =
    | { kind: "idle" }
    | {
        kind: "infra";
        pointerId: number;
        nodeId: string;
        startClientX: number;
        startClientY: number;
        originX: number;
        originY: number;
        dragging: boolean;
      }
    | {
        kind: "press";
        pointerId: number;
        targetNodeId: string | null;
        targetNodeKind: ConnectionNode["kind"] | null;
        startClientX: number;
        startClientY: number;
        moved: boolean;
      };

  const DRAG_THRESHOLD_PX = 6;
  const KEYBOARD_PAN_STEP = 96;

  let viewportWidth = $state<number>(CONNECTION_CANVAS.width);
  let viewportHeight = $state<number>(CONNECTION_CANVAS.height);
  let hoveredNodeId = $state<string | null>(null);
  let viewportDirty = $state(false);
  let viewportTransform = $state<ViewportTransform>(
    computeViewportTransform(CONNECTION_CANVAS.width, CONNECTION_CANVAS.height),
  );
  let canvasContainer = $state<HTMLDivElement | null>(null);
  let pointerInteraction = $state<PointerInteraction>({ kind: "idle" });
  let appliedViewportResetToken = $state(0);

  let redraw = $state<() => void>(() => {});
  let palette = $state<TopologyCanvasPalette>(readTopologyCanvasPalette());

  const baselineViewport = $derived(
    computeViewportTransform(viewportWidth, viewportHeight, {
      expanded: canvasExpanded,
    }),
  );
  const hoverFocus = $derived(hoverFocusState(model, hoveredNodeId));
  const shouldAnimate = $derived(
    !!selectedTrace || model.traces.edgeActivity.size > 0,
  );
  const hoveredNode = $derived(findNodeById(model, hoveredNodeId));
  const overlayCursor = $derived(
    hoveredNode?.kind === "gateway" ? "pointer" : "default",
  );

  onMount(() => {
    if (typeof window === "undefined") return;

    const root = document.documentElement;
    const observer = new MutationObserver(() => {
      palette = readTopologyCanvasPalette();
      redraw?.();
    });
    const handleWindowKeyDown = (event: KeyboardEvent) => {
      if (!canvasExpanded || !panEnabled) return;
      const target = event.target;
      if (
        target instanceof HTMLElement &&
        (target.isContentEditable ||
          target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT")
      ) {
        return;
      }

      handleKeyDown(event);
    };

    observer.observe(root, {
      attributes: true,
      attributeFilter: ["class", "style", "data-theme"],
    });
    window.addEventListener("keydown", handleWindowKeyDown);

    return () => {
      observer.disconnect();
      window.removeEventListener("keydown", handleWindowKeyDown);
    };
  });

  function handleResize(detail: CanvasResizeEvent) {
    viewportWidth = detail.width;
    viewportHeight = detail.height;
  }

  function handleCanvasNodeLeave() {
    hoveredNodeId = null;
    onNodeLeave();
  }

  function handlePointerDown(event: PointerEvent) {
    if (!canvasContainer) return;
    if (event.button !== 0) return;

    const point = canvasPointFromPointer(event);
    const matched = point ? findNodeAt(model, point.x, point.y) : null;

    if (canvasExpanded && matched?.kind === "infra") {
      canvasContainer.setPointerCapture?.(event.pointerId);
      handleCanvasNodeLeave();
      pointerInteraction = {
        kind: "infra",
        pointerId: event.pointerId,
        nodeId: matched.id,
        startClientX: event.clientX,
        startClientY: event.clientY,
        originX: matched.x,
        originY: matched.y,
        dragging: false,
      };
      event.preventDefault();
      return;
    }

    pointerInteraction = {
      kind: "press",
      pointerId: event.pointerId,
      targetNodeId: matched?.id ?? null,
      targetNodeKind: matched?.kind ?? null,
      startClientX: event.clientX,
      startClientY: event.clientY,
      moved: false,
    };

    updateHover(event);
  }

  function handlePointerMove(event: PointerEvent) {
    if (pointerInteraction.kind === "infra") {
      if (event.pointerId !== pointerInteraction.pointerId) return;
      const dragStarted = dragMovedEnough(pointerInteraction, event);
      if (!dragStarted) return;

      pointerInteraction = { ...pointerInteraction, dragging: true };
      const nextPosition = clampInfraNodePosition(
        pointerInteraction.originX +
          (event.clientX - pointerInteraction.startClientX) /
            viewportTransform.scale,
        pointerInteraction.originY +
          (event.clientY - pointerInteraction.startClientY) /
            viewportTransform.scale,
      );
      onInfraNodePositionChange(pointerInteraction.nodeId, nextPosition);
      handleCanvasNodeLeave();
      return;
    }

    if (pointerInteraction.kind === "press") {
      if (event.pointerId !== pointerInteraction.pointerId) return;
      if (!pointerInteraction.moved && dragMovedEnough(pointerInteraction, event)) {
        pointerInteraction = { ...pointerInteraction, moved: true };
      }
      if (!pointerInteraction.moved) {
        updateHover(event);
      } else {
        handleCanvasNodeLeave();
      }
      return;
    }

    updateHover(event);
  }

  function handlePointerUp(event: PointerEvent) {
    if (pointerInteraction.kind === "idle") return;
    if (event.pointerId !== pointerInteraction.pointerId) return;

    const finishedInteraction = pointerInteraction;
    releasePointer(event.pointerId);
    pointerInteraction = { kind: "idle" };

    if (
      finishedInteraction.kind === "press" &&
      !finishedInteraction.moved &&
      finishedInteraction.targetNodeKind === "gateway" &&
      finishedInteraction.targetNodeId
    ) {
      onGatewayClick(finishedInteraction.targetNodeId);
    }

    updateHover(event);
  }

  function handlePointerCancel(event: PointerEvent) {
    if (
      pointerInteraction.kind !== "idle" &&
      event.pointerId === pointerInteraction.pointerId
    ) {
      releasePointer(event.pointerId);
      pointerInteraction = { kind: "idle" };
    }
    handleCanvasNodeLeave();
  }

  function handlePointerLeave() {
    if (pointerInteraction.kind !== "idle") return;
    handleCanvasNodeLeave();
  }

  function handleKeyDown(event: KeyboardEvent) {
    if (!canvasExpanded || !panEnabled) return;

    switch (event.key) {
      case "ArrowLeft":
        event.preventDefault();
        panViewportBy(KEYBOARD_PAN_STEP, 0);
        break;
      case "ArrowRight":
        event.preventDefault();
        panViewportBy(-KEYBOARD_PAN_STEP, 0);
        break;
      case "ArrowUp":
        event.preventDefault();
        panViewportBy(0, KEYBOARD_PAN_STEP);
        break;
      case "ArrowDown":
        event.preventDefault();
        panViewportBy(0, -KEYBOARD_PAN_STEP);
        break;
    }
  }

  function panViewportBy(deltaX: number, deltaY: number) {
    viewportDirty = true;
    viewportTransform = clampViewportTransform(viewportWidth, viewportHeight, {
      scale: viewportTransform.scale,
      offsetX: viewportTransform.offsetX + deltaX,
      offsetY: viewportTransform.offsetY + deltaY,
    });
    handleCanvasNodeLeave();
  }

  function handlePanControlPointerDown(
    event: PointerEvent,
    deltaX: number,
    deltaY: number,
  ) {
    event.preventDefault();
    event.stopPropagation();
    panViewportBy(deltaX, deltaY);
  }

  function canvasPointFromPointer(event: PointerEvent) {
    const rect = canvasContainer?.getBoundingClientRect();
    if (!rect) return null;

    return viewportToCanvasPoint(
      event.clientX - rect.left,
      event.clientY - rect.top,
      viewportTransform,
    );
  }

  function updateHover(event: PointerEvent) {
    const point = canvasPointFromPointer(event);
    if (
      !point ||
      point.x < 0 ||
      point.y < 0 ||
      point.x > CONNECTION_CANVAS.width ||
      point.y > CONNECTION_CANVAS.height
    ) {
      handleCanvasNodeLeave();
      return;
    }

    const matched = findNodeAt(model, point.x, point.y);
    hoveredNodeId = matched?.id ?? null;

    if (!matched) {
      onNodeLeave();
      return;
    }

    onNodeHover({
      node: matched,
      clientX: event.clientX,
      clientY: event.clientY,
    });
  }

  function dragMovedEnough(
    interaction:
      | Extract<PointerInteraction, { kind: "infra" }>
      | Extract<PointerInteraction, { kind: "press" }>,
    event: PointerEvent,
  ): boolean {
    return (
      Math.abs(event.clientX - interaction.startClientX) >= DRAG_THRESHOLD_PX ||
      Math.abs(event.clientY - interaction.startClientY) >= DRAG_THRESHOLD_PX
    );
  }

  function releasePointer(pointerId: number) {
    if (!canvasContainer?.hasPointerCapture?.(pointerId)) return;
    canvasContainer.releasePointerCapture(pointerId);
  }

  function findNodeById(
    graph: TopologyGraphModel,
    nodeId: string | null,
  ): ConnectionNode | null {
    if (!nodeId) return null;

    const nodes: ConnectionNode[] = [
      ...graph.nodes.gateways,
      ...graph.nodes.eventbridges,
      ...graph.nodes.topics,
      ...graph.nodes.queues,
      ...graph.nodes.functions,
      ...(graph.nodes.cacheExtension ? [graph.nodes.cacheExtension] : []),
      ...graph.nodes.secrets,
      ...graph.nodes.buckets,
      ...graph.nodes.infra,
    ];

    return nodes.find((node) => node.id === nodeId) ?? null;
  }

  $effect(() => {
    model;
    hoveredNodeId;
    viewportTransform.scale;
    viewportTransform.offsetX;
    viewportTransform.offsetY;
    palette;
    redraw?.();
  });

  $effect(() => {
    if (!viewportDirty || !canvasExpanded) {
      viewportTransform = baselineViewport;
    }
  });

  $effect(() => {
    if (viewportResetToken === appliedViewportResetToken) return;
    appliedViewportResetToken = viewportResetToken;
    viewportDirty = false;
    viewportTransform = baselineViewport;
    handleCanvasNodeLeave();
  });

  $effect(() => {
    if (!canvasExpanded) {
      viewportDirty = false;
    }
  });

</script>

<div
  bind:this={canvasContainer}
  class={`relative w-full overflow-hidden border border-border/70 bg-card/30 shadow-inner ${
    canvasExpanded
      ? "h-screen min-h-[100svh] rounded-xl"
      : "h-full min-h-0 rounded-lg"
  }`}
>
  {#if canvasExpanded}
    <div
      class="pointer-events-none absolute left-3 top-3 z-20 max-w-[calc(100%-1.5rem)] rounded-xl border border-border/80 bg-background/80 px-3 py-2 shadow-xl backdrop-blur-md"
    >
      <div class="flex flex-wrap items-center gap-2">
        <Badge variant={panEnabled ? "default" : "secondary"}>
          {panEnabled ? "Explore mode" : "Layout mode"}
        </Badge>
        {#if model.nodes.infra.length > 0}
          <Badge variant="outline">
            {panEnabled ? "Use arrow keys or the D-pad" : "Drag infra to pin layout"}
          </Badge>
        {/if}
      </div>
      <p class="mt-1 text-[11px] text-muted-foreground">
        {#if panEnabled}
          Use the arrow keys or the bottom-right controls to move around the topology.
        {:else}
          Infra nodes can be repositioned and their coordinates persist across refreshes.
        {/if}
      </p>
    </div>

    {#if panEnabled}
      <div
        class="absolute bottom-3 right-3 z-20 flex flex-col items-center gap-1 rounded-2xl border border-border/80 bg-background/85 p-2 shadow-xl backdrop-blur-md"
      >
        <div class="flex items-center justify-center">
          <Button
            variant="secondary"
            size="icon"
            class="h-9 w-9 rounded-xl"
            aria-label="Pan canvas up"
            title="Pan up"
            onclick={() => panViewportBy(0, KEYBOARD_PAN_STEP)}
            onpointerdown={(event: PointerEvent) =>
              handlePanControlPointerDown(event, 0, KEYBOARD_PAN_STEP)}
          >
            <ArrowSquareUpIcon size={18} />
          </Button>
        </div>
        <div class="flex items-center gap-1">
          <Button
            variant="secondary"
            size="icon"
            class="h-9 w-9 rounded-xl"
            aria-label="Pan canvas left"
            title="Pan left"
            onclick={() => panViewportBy(KEYBOARD_PAN_STEP, 0)}
            onpointerdown={(event: PointerEvent) =>
              handlePanControlPointerDown(event, KEYBOARD_PAN_STEP, 0)}
          >
            <ArrowSquareLeftIcon size={18} />
          </Button>
          <Button
            variant="secondary"
            size="icon"
            class="h-9 w-9 rounded-xl"
            aria-label="Pan canvas down"
            title="Pan down"
            onclick={() => panViewportBy(0, -KEYBOARD_PAN_STEP)}
            onpointerdown={(event: PointerEvent) =>
              handlePanControlPointerDown(event, 0, -KEYBOARD_PAN_STEP)}
          >
            <ArrowSquareDownIcon size={18} />
          </Button>
          <Button
            variant="secondary"
            size="icon"
            class="h-9 w-9 rounded-xl"
            aria-label="Pan canvas right"
            title="Pan right"
            onclick={() => panViewportBy(-KEYBOARD_PAN_STEP, 0)}
            onpointerdown={(event: PointerEvent) =>
              handlePanControlPointerDown(event, -KEYBOARD_PAN_STEP, 0)}
          >
            <ArrowSquareRightIcon size={18} />
          </Button>
        </div>
        <span class="text-[10px] font-mono uppercase tracking-wide text-muted-foreground">
          arrows / keys
        </span>
      </div>
    {/if}
  {/if}

  <Canvas
    bind:redraw
    class="h-full"
    layerEvents={true}
    autoplay={shouldAnimate}
    onresize={handleResize}
  >
    <TopologyCanvasBackgroundLayer
      {model}
      {palette}
      {viewportTransform}
    />
    <TopologyCanvasEdgeLayer
      {model}
      {hoverFocus}
      {palette}
      {viewportTransform}
    />
    <TopologyCanvasNodeLayer
      {model}
      {selectedTrace}
      {hoverFocus}
      {palette}
      {viewportTransform}
      {hoveredNodeId}
    />
  </Canvas>

  <div
    class="absolute inset-0 z-10"
    class:touch-none={canvasExpanded}
    role="presentation"
    aria-hidden="true"
    style={`cursor: ${overlayCursor};`}
    onpointerdown={handlePointerDown}
    onpointermove={handlePointerMove}
    onpointerup={handlePointerUp}
    onpointercancel={handlePointerCancel}
    onpointerleave={handlePointerLeave}
  ></div>
</div>
