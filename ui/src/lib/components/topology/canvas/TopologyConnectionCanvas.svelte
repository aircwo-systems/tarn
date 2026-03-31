<script lang="ts">
  import { onMount } from "svelte";
  import {
    MagnifyingGlassMinusIcon,
    MagnifyingGlassPlusIcon,
    SquaresFourIcon,
  } from "phosphor-svelte";
  import { Canvas, type CanvasResizeEvent } from "svelte-canvas";
  import Button from "$lib/components/ui/button/button.svelte";
  import * as ContextMenu from "$lib/components/ui/context-menu/index.js";
  import type { RequestTrace } from "$lib/types";
  import {
    getTopologyNodeConfigTab,
    getTopologyNodeSupportedSizes,
    getTopologyNodeViews,
  } from "../registry";
  import type { ConnectionNode, NodeSide, NodeSize, NodeView } from "../types";
  import {
    clampViewportTransform,
    CONNECTION_CANVAS,
    computeViewportTransform,
    findNodeAt,
    hoverFocusState,
    resolveSnappedNodePosition,
    viewportToCanvasPoint,
    type InfraNodePosition,
    type NodeOverride,
    type TopologyGraphModel,
    type ViewportTransform,
  } from "../topology-connection-model";
  // Alias — all node kinds use the same position shape
  type NodePosition = InfraNodePosition;
  import TopologyCanvasBackgroundLayer from "./TopologyCanvasBackgroundLayer.svelte";
  import TopologyCanvasEdgeLayer from "./TopologyCanvasEdgeLayer.svelte";
  import TopologyCanvasMiniMap from "./TopologyCanvasMiniMap.svelte";
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
    onNodePositionChange = (
      _id: string,
      _kind: ConnectionNode["kind"],
      _position: NodePosition,
    ) => {},
    onNodeOverrideChange = (_id: string, _override: NodeOverride) => {},
    onAutoOrganize = () => {},
    onNavigate = (_tab: string) => {},
  }: {
    model: TopologyGraphModel;
    selectedTrace?: RequestTrace | null;
    canvasExpanded?: boolean;
    panEnabled?: boolean;
    viewportResetToken?: number;
    onGatewayClick?: (id: string) => void;
    onNodeHover?: (payload: TopologyNodeHoverPayload) => void;
    onNodeLeave?: () => void;
    onNodePositionChange?: (
      id: string,
      kind: ConnectionNode["kind"],
      position: NodePosition,
    ) => void;
    onNodeOverrideChange?: (id: string, override: NodeOverride) => void;
    onAutoOrganize?: () => void;
    onNavigate?: (tab: string) => void;
  } = $props();

  type PointerInteraction =
    | { kind: "idle" }
    | {
        kind: "infra";
        pointerId: number;
        nodeId: string;
        nodeKind: ConnectionNode["kind"];
        startClientX: number;
        startClientY: number;
        originX: number;
        originY: number;
        latestPosition: NodePosition;
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
  const DRAG_SETTLE_DELAY_MS = 2200;
  const KEYBOARD_PAN_STEP = 96;
  const ZOOM_STEP = 1.14;
  const TRACKPAD_ZOOM_SENSITIVITY = 0.0025;
  const PLACEMENT_CONFIRMATION_FLASHES = 3;
  const PLACEMENT_CONFIRMATION_DURATION_MS = 1320;

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
  let placementConfirmation = $state<{
    nodeId: string;
    nodeKind: ConnectionNode["kind"];
    startedAt: number;
    flashes: number;
    durationMs: number;
  } | null>(null);

  let redraw = $state<() => void>(() => {});
  let palette = $state<TopologyCanvasPalette>(readTopologyCanvasPalette());
  let dragSettleTimer: ReturnType<typeof window.setTimeout> | null = null;
  let placementConfirmationTimer: ReturnType<typeof window.setTimeout> | null =
    null;
  // Store only the clicked node's kind+id; derive the live node from the current
  // model so the context menu always reflects post-rebuild state and never
  // triggers spurious onValueChange callbacks from stale prop values.
  // Both kind AND id are required because EventBridge node IDs can equal
  // function node IDs (both use the Lambda name), so kind is needed to
  // disambiguate the lookup and to namespace the override key.
  let contextMenuNodeId = $state<string | null>(null);
  let contextMenuNodeKind = $state<string | null>(null);
  const contextMenuNode = $derived(
    contextMenuNodeId && contextMenuNodeKind
      ? findNodeByKindId(model, contextMenuNodeKind, contextMenuNodeId)
      : null,
  );
  const contextMenuViews = $derived(
    contextMenuNode
      ? getTopologyNodeViews(
          contextMenuNode.kind,
          contextMenuNode.size ?? "small",
        )
      : [],
  );
  const contextMenuConfigTab = $derived(
    contextMenuNode ? getTopologyNodeConfigTab(contextMenuNode.kind) : null,
  );
  const contextMenuSupportedSizes = $derived(
    contextMenuNode ? getTopologyNodeSupportedSizes(contextMenuNode.kind) : [],
  );

  const baselineViewport = $derived(
    computeViewportTransform(viewportWidth, viewportHeight, {
      expanded: canvasExpanded,
    }),
  );
  const hoverFocus = $derived(hoverFocusState(model, hoveredNodeId));
  const activeDragNode = $derived(
    pointerInteraction.kind === "infra" && pointerInteraction.dragging
      ? {
          nodeId: pointerInteraction.nodeId,
          nodeKind: pointerInteraction.nodeKind,
        }
      : null,
  );
  const shouldAnimate = $derived(
    !!selectedTrace ||
      model.traces.edgeActivity.size > 0 ||
      !!placementConfirmation ||
      !!activeDragNode,
  );
  const minZoomScale = $derived(baselineViewport.scale * 0.78);
  const maxZoomScale = $derived(baselineViewport.scale * 2.2);
  const canZoomOut = $derived(viewportTransform.scale > minZoomScale + 0.001);
  const canZoomIn = $derived(viewportTransform.scale < maxZoomScale - 0.001);
  const hoveredNode = $derived(findNodeById(model, hoveredNodeId));
  const overlayCursor = $derived(
    canvasExpanded && hoveredNode ? "grab" : "default",
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
      clearDragSettleTimer();
      clearPlacementConfirmation();
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
    clearPlacementConfirmation();

    const point = canvasPointFromPointer(event);
    const matched = point ? findNodeAt(model, point.x, point.y) : null;

    if (canvasExpanded && matched) {
      canvasContainer.setPointerCapture?.(event.pointerId);
      handleCanvasNodeLeave();
      pointerInteraction = {
        kind: "infra",
        pointerId: event.pointerId,
        nodeId: matched.id,
        nodeKind: matched.kind,
        startClientX: event.clientX,
        startClientY: event.clientY,
        originX: matched.x,
        originY: matched.y,
        latestPosition: { x: matched.x, y: matched.y },
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
      const activeInteraction = pointerInteraction;
      const dragStarted = dragMovedEnough(activeInteraction, event);
      if (!dragStarted) return;

      const nextPosition = resolveSnappedNodePosition(
        model,
        {
          id: activeInteraction.nodeId,
          kind: activeInteraction.nodeKind,
        },
        activeInteraction.originX +
          (event.clientX - activeInteraction.startClientX) /
            viewportTransform.scale,
        activeInteraction.originY +
          (event.clientY - activeInteraction.startClientY) /
            viewportTransform.scale,
      );
      const positionChanged =
        nextPosition.x !== activeInteraction.latestPosition.x ||
        nextPosition.y !== activeInteraction.latestPosition.y;
      pointerInteraction = {
        ...activeInteraction,
        dragging: activeInteraction.dragging || positionChanged,
        latestPosition: positionChanged
          ? nextPosition
          : activeInteraction.latestPosition,
      };
      if (positionChanged) {
        onNodePositionChange(
          activeInteraction.nodeId,
          activeInteraction.nodeKind,
          nextPosition,
        );
        scheduleDragSettle();
      }
      handleCanvasNodeLeave();
      return;
    }

    if (pointerInteraction.kind === "press") {
      if (event.pointerId !== pointerInteraction.pointerId) return;
      if (
        !pointerInteraction.moved &&
        dragMovedEnough(pointerInteraction, event)
      ) {
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
    clearDragSettleTimer();

    if (finishedInteraction.kind === "infra") {
      finalizeDraggedInteraction(finishedInteraction);
      return;
    }

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
    clearDragSettleTimer();
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

  function handleContextMenu(e: MouseEvent) {
    const rect = canvasContainer?.getBoundingClientRect();
    if (!rect) return;
    const point = viewportToCanvasPoint(
      e.clientX - rect.left,
      e.clientY - rect.top,
      viewportTransform,
    );
    const inBounds =
      point.x >= 0 &&
      point.y >= 0 &&
      point.x <= CONNECTION_CANVAS.width &&
      point.y <= CONNECTION_CANVAS.height;
    const matched = inBounds ? findNodeAt(model, point.x, point.y) : null;
    if (!matched) {
      e.preventDefault();
      contextMenuNodeId = null;
      contextMenuNodeKind = null;
      return;
    }
    contextMenuNodeId = matched.id;
    contextMenuNodeKind = matched.kind;
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

  function zoomViewportBy(factor: number) {
    const currentScale = viewportTransform.scale;
    const nextScale = Math.min(
      maxZoomScale,
      Math.max(minZoomScale, currentScale * factor),
    );
    if (Math.abs(nextScale - currentScale) < 0.001) return;

    viewportDirty = true;
    const centerCanvasX = (viewportWidth / 2 - viewportTransform.offsetX) / currentScale;
    const centerCanvasY = (viewportHeight / 2 - viewportTransform.offsetY) / currentScale;
    viewportTransform = clampViewportTransform(viewportWidth, viewportHeight, {
      scale: nextScale,
      offsetX: viewportWidth / 2 - centerCanvasX * nextScale,
      offsetY: viewportHeight / 2 - centerCanvasY * nextScale,
    });
    handleCanvasNodeLeave();
  }

  function handleZoomControlPointerDown(event: PointerEvent, factor: number) {
    event.preventDefault();
    event.stopPropagation();
    zoomViewportBy(factor);
  }

  function handleOrganizePointerDown(event: PointerEvent) {
    event.preventDefault();
    event.stopPropagation();
    onAutoOrganize();
  }

  function handleWheel(event: WheelEvent) {
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

    event.preventDefault();
    event.stopPropagation();

    if (event.ctrlKey || event.metaKey) {
      zoomViewportBy(Math.exp(-event.deltaY * TRACKPAD_ZOOM_SENSITIVITY));
      return;
    }

    panViewportBy(-event.deltaX, -event.deltaY);
  }

  function focusViewportAt(canvasX: number, canvasY: number) {
    viewportDirty = true;
    viewportTransform = clampViewportTransform(viewportWidth, viewportHeight, {
      scale: viewportTransform.scale,
      offsetX: viewportWidth / 2 - canvasX * viewportTransform.scale,
      offsetY: viewportHeight / 2 - canvasY * viewportTransform.scale,
    });
    handleCanvasNodeLeave();
  }

  function navigateToConfigTab(tab: string) {
    onNavigate(tab);
    if (typeof window === "undefined") return;

    if (window.location.pathname !== "/") {
      window.location.assign(`/#${tab}`);
      return;
    }

    if (window.location.hash !== `#${tab}`) {
      window.location.hash = tab;
    }
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

  function scheduleDragSettle() {
    if (typeof window === "undefined") return;
    clearDragSettleTimer();
    dragSettleTimer = window.setTimeout(() => {
      if (pointerInteraction.kind !== "infra" || !pointerInteraction.dragging) {
        return;
      }
      finalizeDraggedInteraction(pointerInteraction);
    }, DRAG_SETTLE_DELAY_MS);
  }

  function clearDragSettleTimer() {
    if (dragSettleTimer === null || typeof window === "undefined") return;
    window.clearTimeout(dragSettleTimer);
    dragSettleTimer = null;
  }

  function finalizeDraggedInteraction(
    interaction: Extract<PointerInteraction, { kind: "infra" }>,
  ) {
    clearDragSettleTimer();
    releasePointer(interaction.pointerId);
    pointerInteraction = { kind: "idle" };
    if (interaction.dragging) {
      onNodePositionChange(
        interaction.nodeId,
        interaction.nodeKind,
        interaction.latestPosition,
      );
      triggerPlacementConfirmation(interaction.nodeId, interaction.nodeKind);
    } else if (interaction.nodeKind === "gateway") {
      onGatewayClick(interaction.nodeId);
    }
    handleCanvasNodeLeave();
  }

  function triggerPlacementConfirmation(
    nodeId: string,
    nodeKind: ConnectionNode["kind"],
  ) {
    clearPlacementConfirmation();
    const startedAt =
      typeof performance !== "undefined" ? performance.now() : Date.now();
    placementConfirmation = {
      nodeId,
      nodeKind,
      startedAt,
      flashes: PLACEMENT_CONFIRMATION_FLASHES,
      durationMs: PLACEMENT_CONFIRMATION_DURATION_MS,
    };
    redraw?.();

    if (typeof window === "undefined") return;
    placementConfirmationTimer = window.setTimeout(() => {
      placementConfirmation = null;
      placementConfirmationTimer = null;
      redraw?.();
    }, PLACEMENT_CONFIRMATION_DURATION_MS);
  }

  function clearPlacementConfirmation() {
    if (placementConfirmationTimer !== null && typeof window !== "undefined") {
      window.clearTimeout(placementConfirmationTimer);
      placementConfirmationTimer = null;
    }
    if (placementConfirmation) {
      placementConfirmation = null;
      redraw?.();
    }
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
      ...graph.nodes.dynamodbs,
      ...graph.nodes.functions,
      ...(graph.nodes.cacheExtension ? [graph.nodes.cacheExtension] : []),
      ...graph.nodes.secrets,
      ...graph.nodes.buckets,
      ...graph.nodes.infra,
    ];

    return nodes.find((node) => node.id === nodeId) ?? null;
  }

  function findNodeByKindId(
    graph: TopologyGraphModel,
    kind: string,
    nodeId: string,
  ): ConnectionNode | null {
    const nodes: ConnectionNode[] = [
      ...graph.nodes.gateways,
      ...graph.nodes.eventbridges,
      ...graph.nodes.topics,
      ...graph.nodes.queues,
      ...graph.nodes.dynamodbs,
      ...graph.nodes.functions,
      ...(graph.nodes.cacheExtension ? [graph.nodes.cacheExtension] : []),
      ...graph.nodes.secrets,
      ...graph.nodes.buckets,
      ...graph.nodes.infra,
    ];

    return (
      nodes.find((node) => node.kind === kind && node.id === nodeId) ?? null
    );
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
  class={`relative h-full min-h-0 w-full overflow-hidden border border-border/70 bg-card/30 shadow-inner ${
    canvasExpanded ? "rounded-xl" : "rounded-lg"
  }`}
  onwheel={handleWheel}
>
  {#if canvasExpanded}
    {#if panEnabled}
      <div
        class="absolute bottom-3 left-3 z-20 flex flex-col gap-1"
      >
        <div class="flex flex-col gap-1">
          <Button
            variant="secondary"
            size="icon"
            class="h-9 w-9 rounded-xl shadow-lg"
            aria-label="Organise canvas layout"
            title="Organise canvas layout"
            onclick={onAutoOrganize}
            onpointerdown={handleOrganizePointerDown}
          >
            <SquaresFourIcon size={18} />
          </Button>
          <Button
            variant="secondary"
            size="icon"
            class="h-9 w-9 rounded-xl shadow-lg"
            aria-label="Zoom in"
            title="Zoom in"
            disabled={!canZoomIn}
            onclick={() => zoomViewportBy(ZOOM_STEP)}
            onpointerdown={(event: PointerEvent) =>
              handleZoomControlPointerDown(event, ZOOM_STEP)}
          >
            <MagnifyingGlassPlusIcon size={18} />
          </Button>
          <Button
            variant="secondary"
            size="icon"
            class="h-9 w-9 rounded-xl shadow-lg"
            aria-label="Zoom out"
            title="Zoom out"
            disabled={!canZoomOut}
            onclick={() => zoomViewportBy(1 / ZOOM_STEP)}
            onpointerdown={(event: PointerEvent) =>
              handleZoomControlPointerDown(event, 1 / ZOOM_STEP)}
          >
            <MagnifyingGlassMinusIcon size={18} />
          </Button>
        </div>
      </div>

      <TopologyCanvasMiniMap
        {model}
        {palette}
        {viewportTransform}
        {viewportWidth}
        {viewportHeight}
        onFocusCanvas={focusViewportAt}
      />
    {/if}
  {/if}

  <Canvas
    bind:redraw
    class="h-full"
    layerEvents={true}
    autoplay={shouldAnimate}
    onresize={handleResize}
  >
    <TopologyCanvasBackgroundLayer {model} {palette} {viewportTransform} />
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
      {activeDragNode}
      {placementConfirmation}
    />
  </Canvas>

  {#if canvasExpanded}
    <ContextMenu.Root>
      <ContextMenu.Trigger
        class="absolute inset-0 z-10 touch-none block"
        role="presentation"
        aria-hidden="true"
        style={`cursor: ${overlayCursor};`}
        onpointerdown={handlePointerDown}
        onpointermove={handlePointerMove}
        onpointerup={handlePointerUp}
        onpointercancel={handlePointerCancel}
        onpointerleave={handlePointerLeave}
        oncontextmenu={handleContextMenu}
      />
      <ContextMenu.Content>
        {#if contextMenuNode}
          {@const overrideKey = `${contextMenuNode.kind}:${contextMenuNode.id}`}
          {@const inputVal = contextMenuNode.inputSide ?? "left"}
          {@const outputVal = contextMenuNode.outputSide ?? "right"}
          {@const sizeVal = contextMenuNode.size ?? "small"}
          {@const viewVal = contextMenuNode.view ?? "standard"}

          <ContextMenu.Label
            class="text-[10px] font-mono uppercase tracking-wide text-muted-foreground/60 px-1.5 pb-0.5"
          >
            {contextMenuNode.label}
          </ContextMenu.Label>
          <ContextMenu.Separator />

          <ContextMenu.Sub>
            <ContextMenu.SubTrigger>Input port</ContextMenu.SubTrigger>
            <ContextMenu.SubContent>
              <ContextMenu.RadioGroup
                value={inputVal}
                onValueChange={(v) =>
                  onNodeOverrideChange(overrideKey, {
                    inputSide: v as NodeSide,
                  })}
              >
                <ContextMenu.RadioItem value="left"
                  >← Left</ContextMenu.RadioItem
                >
                <ContextMenu.RadioItem value="right"
                  >→ Right</ContextMenu.RadioItem
                >
                <ContextMenu.RadioItem value="top">↑ Top</ContextMenu.RadioItem>
                <ContextMenu.RadioItem value="bottom"
                  >↓ Bottom</ContextMenu.RadioItem
                >
              </ContextMenu.RadioGroup>
            </ContextMenu.SubContent>
          </ContextMenu.Sub>

          <ContextMenu.Sub>
            <ContextMenu.SubTrigger>Output port</ContextMenu.SubTrigger>
            <ContextMenu.SubContent>
              <ContextMenu.RadioGroup
                value={outputVal}
                onValueChange={(v) =>
                  onNodeOverrideChange(overrideKey, {
                    outputSide: v as NodeSide,
                  })}
              >
                <ContextMenu.RadioItem value="left"
                  >← Left</ContextMenu.RadioItem
                >
                <ContextMenu.RadioItem value="right"
                  >→ Right</ContextMenu.RadioItem
                >
                <ContextMenu.RadioItem value="top">↑ Top</ContextMenu.RadioItem>
                <ContextMenu.RadioItem value="bottom"
                  >↓ Bottom</ContextMenu.RadioItem
                >
              </ContextMenu.RadioGroup>
            </ContextMenu.SubContent>
          </ContextMenu.Sub>

          <ContextMenu.Separator />

          <ContextMenu.Sub>
            <ContextMenu.SubTrigger>Size</ContextMenu.SubTrigger>
            <ContextMenu.SubContent>
              <ContextMenu.RadioGroup
                value={sizeVal}
                onValueChange={(v) =>
                  onNodeOverrideChange(overrideKey, { size: v as NodeSize })}
              >
                <ContextMenu.RadioItem
                  value="small"
                  disabled={!contextMenuSupportedSizes.includes("small")}
                  >Small</ContextMenu.RadioItem
                >
                <ContextMenu.RadioItem
                  value="medium"
                  disabled={!contextMenuSupportedSizes.includes("medium")}
                  >Medium</ContextMenu.RadioItem
                >
                <ContextMenu.RadioItem
                  value="large"
                  disabled={!contextMenuSupportedSizes.includes("large")}
                  >Large</ContextMenu.RadioItem
                >
              </ContextMenu.RadioGroup>
            </ContextMenu.SubContent>
          </ContextMenu.Sub>

          {#if contextMenuViews.length > 1}
            <ContextMenu.Separator />

            <ContextMenu.Sub>
              <ContextMenu.SubTrigger>View</ContextMenu.SubTrigger>
              <ContextMenu.SubContent>
                <ContextMenu.RadioGroup
                  value={viewVal}
                  onValueChange={(v) =>
                    onNodeOverrideChange(overrideKey, { view: v as NodeView })}
                >
                  {#each contextMenuViews as view}
                    <ContextMenu.RadioItem value={view.id}
                      >{view.label}</ContextMenu.RadioItem
                    >
                  {/each}
                </ContextMenu.RadioGroup>
              </ContextMenu.SubContent>
            </ContextMenu.Sub>
          {/if}

          {#if contextMenuConfigTab}
            <ContextMenu.Separator />
            <ContextMenu.Item onSelect={() => navigateToConfigTab(contextMenuConfigTab)}>
              View config
            </ContextMenu.Item>
          {/if}
        {/if}
      </ContextMenu.Content>
    </ContextMenu.Root>
  {:else}
    <div
      class="absolute inset-0 z-10"
      role="presentation"
      aria-hidden="true"
      style={`cursor: ${overlayCursor};`}
      onpointerdown={handlePointerDown}
      onpointermove={handlePointerMove}
      onpointerup={handlePointerUp}
      onpointercancel={handlePointerCancel}
      onpointerleave={handlePointerLeave}
    ></div>
  {/if}
</div>
