<script lang="ts">
  import { Layer, type Render } from "svelte-canvas";
  import type {
    EventBridgeRuleSummary,
    FunctionSummary,
    InfraProbe,
    RequestTrace,
  } from "$lib/types";
  import { formatBytes } from "$lib/utils";
  import {
    CONNECTION_CANVAS,
    nodeBounds,
    portPos,
    selectedTraceNodes,
    traceStatusTone,
    type HoverFocusState,
    type TopologyGraphModel,
    type ViewportTransform,
  } from "../topology-connection-model";
  import {
    infraKindColor,
    kindColor,
    type TopologyCanvasPalette,
  } from "../topology-canvas-theme";
  import type { ConnectionNode } from "../types";

  const MONO_FONT = '"JetBrains Mono Variable", "SF Mono", ui-monospace, monospace';

  let {
    model,
    selectedTrace = null,
    hoverFocus,
    palette,
    viewportTransform,
    hoveredNodeId = null,
    activeDragNode = null,
    placementConfirmation = null,
  }: {
    model: TopologyGraphModel;
    selectedTrace?: RequestTrace | null;
    hoverFocus: HoverFocusState;
    palette: TopologyCanvasPalette;
    viewportTransform: ViewportTransform;
    hoveredNodeId?: string | null;
    activeDragNode?: {
      nodeId: string;
      nodeKind: ConnectionNode["kind"];
    } | null;
    placementConfirmation?: {
      nodeId: string;
      nodeKind: ConnectionNode["kind"];
      startedAt: number;
      flashes: number;
      durationMs: number;
    } | null;
  } = $props();

  const graphEdges = $derived([
    ...model.edges.apigwToQueue,
    ...model.edges.apigwToFunction,
    ...model.edges.eventbridgeToFunction,
    ...model.edges.snsToQueue,
    ...model.edges.snsToFunction,
    ...model.edges.queueToFunction,
    ...model.edges.dynamodbToFunction,
    ...model.edges.queueToDlq,
    ...model.edges.bucketToFunction,
    ...model.edges.functionToDynamodb,
    ...model.edges.functionToCache,
    ...model.edges.cacheToSecret,
    ...model.edges.functionToInfra,
  ]);
  const inputNodeIds = $derived(
    new Set(graphEdges.map((edge) => `${edge.to.kind}:${edge.to.id}`)),
  );
  const outputNodeIds = $derived(
    new Set(graphEdges.map((edge) => `${edge.from.kind}:${edge.from.id}`)),
  );

  const render: Render = ({ context, width, height, time }) => {
    if (!model.hasData) return;

    context.save();
    context.translate(viewportTransform.offsetX, viewportTransform.offsetY);
    context.scale(viewportTransform.scale, viewportTransform.scale);

    const allNodes: Array<{ node: ConnectionNode; stroke: string }> = [
      ...model.nodes.gateways.map((n) => ({ node: n, stroke: kindColor("gateway", palette) })),
      ...model.nodes.eventbridges.map((n) => ({ node: n, stroke: kindColor("eventbridge", palette) })),
      ...model.nodes.topics.map((n) => ({ node: n, stroke: kindColor("topic", palette) })),
      ...model.nodes.queues.map((n) => ({ node: n, stroke: kindColor("queue", palette) })),
      ...model.nodes.dynamodbs.map((n) => ({ node: n, stroke: kindColor("dynamodb", palette) })),
      ...model.nodes.functions.map((n) => ({ node: n, stroke: kindColor("function", palette) })),
      ...model.nodes.buckets.map((n) => ({ node: n, stroke: kindColor("bucket", palette) })),
      ...model.nodes.secrets.map((n) => ({ node: n, stroke: kindColor("secret", palette) })),
      ...(model.nodes.cacheExtension ? [{ node: model.nodes.cacheExtension, stroke: kindColor("extension", palette) }] : []),
      ...model.nodes.infra.map((n) => {
        const probe = model.infraById.get(n.id);
        const isConnected = n.status === "connected";
        const stroke = isConnected
          ? infraKindColor(probe?.kind ?? "", palette)
          : palette.destructive;
        return { node: n, stroke };
      }),
    ];

    for (const { node, stroke } of allNodes) {
      drawNode(context, node, {
        palette,
        stroke,
        fill: palette.popover,
        titleColor: palette.foreground,
        subColor: palette.mutedForeground,
        hovered: hoveredNodeId === node.id,
        focused: isFocused(node.id),
        time,
      });
      drawNodePorts(
        context,
        node,
        stroke,
        isFocused(node.id),
        inputNodeIds.has(`${node.kind}:${node.id}`),
        outputNodeIds.has(`${node.kind}:${node.id}`),
      );
    }

    if (selectedTrace) {
      const tone = traceStatusTone(selectedTrace.status);
      const highlight =
        tone === "destructive"
          ? palette.destructive
          : tone === "warning"
            ? palette.warning
            : palette.primary;
      const pulseAlpha = 0.35 + 0.35 * (0.5 + 0.5 * Math.sin(time / 280));
      const matchedNodes = selectedTraceNodes(model, selectedTrace);
      for (const node of matchedNodes) {
        const bounds = nodeBounds(node);
        context.save();
        context.strokeStyle = highlight;
        context.globalAlpha = focusOpacity(pulseAlpha, isFocused(node.id), palette);
        context.lineWidth = 1.5;
        context.setLineDash([4, 2]);
        context.lineDashOffset = -(time / 110);
        roundRect(
          context,
          bounds.left - 3,
          bounds.top - 3,
          bounds.right - bounds.left + 6,
          bounds.bottom - bounds.top + 6,
          8,
        );
        context.stroke();
        context.restore();
      }
    }

    if (activeDragNode) {
      const matchedNode = allNodes.find(
        ({ node }) =>
          node.id === activeDragNode.nodeId &&
          node.kind === activeDragNode.nodeKind,
      )?.node;
      if (matchedNode) {
        drawActiveDragBorder(context, matchedNode, palette, time);
      }
    }

    if (placementConfirmation) {
      const matchedNode = allNodes.find(
        ({ node }) =>
          node.id === placementConfirmation.nodeId &&
          node.kind === placementConfirmation.nodeKind,
      )?.node;
      if (matchedNode) {
        drawPlacementConfirmation(context, matchedNode, palette, time);
      }
    }

    context.restore();
  };

  function isFocused(nodeId: string): boolean {
    if (!hoverFocus.active) return true;
    return hoverFocus.nodeIds.has(nodeId);
  }

  function focusOpacity(
    base: number,
    focused: boolean,
    palette: TopologyCanvasPalette,
  ): number {
    const boostedBase = palette.isDark ? base : Math.min(1, base * 1.3 + 0.06);
    if (!hoverFocus.active) return boostedBase;
    const dimmed = boostedBase * (palette.isDark ? 0.22 : 0.12);
    return focused ? boostedBase : dimmed;
  }

  function drawNodePorts(
    context: CanvasRenderingContext2D,
    node: ConnectionNode,
    color: string,
    focused: boolean,
    showInput: boolean,
    showOutput: boolean,
  ) {
    if (!showInput && !showOutput) return;

    const r = 3.5;
    context.save();
    context.fillStyle = color;
    context.globalAlpha = focusOpacity(palette.isDark ? 0.75 : 0.9, focused, palette);
    if (showInput) {
      const inp = portPos(node, "input");
      context.beginPath();
      context.arc(inp.x, inp.y, r, 0, Math.PI * 2);
      context.fill();
    }
    if (showOutput) {
      const out = portPos(node, "output");
      context.beginPath();
      context.arc(out.x, out.y, r, 0, Math.PI * 2);
      context.fill();
    }
    context.restore();
  }

  function drawNode(
    context: CanvasRenderingContext2D,
    node: ConnectionNode,
    style: {
      palette: TopologyCanvasPalette;
      stroke: string;
      fill: string;
      titleColor: string;
      subColor: string;
      hovered: boolean;
      focused: boolean;
      time: number;
    },
  ) {
    const { palette } = style;
    const bounds = nodeBounds(node);
    const boost =
      hoverFocus.active && style.focused
        ? 1 + 0.024 * (0.5 + 0.5 * Math.sin(style.time / 190))
        : 1;

    context.save();
    if (boost !== 1) {
      context.translate(node.x, node.y);
      context.scale(boost, boost);
      context.translate(-node.x, -node.y);
    }

    context.fillStyle = style.fill;
    context.strokeStyle = style.stroke;
    const baseAlpha = palette.isDark
      ? style.hovered
        ? 0.96
        : 0.84
      : style.hovered
        ? 1
        : 0.97;
    context.globalAlpha = focusOpacity(baseAlpha, style.focused, palette);
    context.lineWidth = style.hovered ? 2.2 : palette.isDark ? 1.35 : 1.5;
    roundRect(
      context,
      bounds.left,
      bounds.top,
      bounds.right - bounds.left,
      bounds.bottom - bounds.top,
      node.kind === "extension" ? 9 : 8,
    );
    context.fill();
    context.stroke();

    if (node.kind === "bucket" && node.bucket && node.size && node.size !== "small") {
      drawBucketNodeContent(context, node, bounds, style);
    } else if (
      node.kind === "eventbridge" &&
      node.size &&
      node.size !== "small" &&
      model.eventBridgeRuleById.get(node.id)
    ) {
      drawEventBridgeNodeContent(
        context,
        node,
        bounds,
        style,
        model.eventBridgeRuleById.get(node.id)!,
      );
    } else if (
      node.kind === "function" &&
      node.size &&
      node.size !== "small" &&
      model.functionById.get(node.id)
    ) {
      drawFunctionNodeContent(
        context,
        node,
        bounds,
        style,
        model.functionById.get(node.id)!,
      );
    } else if (
      node.kind === "extension" &&
      node.size &&
      node.size !== "small"
    ) {
      drawCacheExtensionNodeContent(
        context,
        node,
        bounds,
        style,
      );
    } else if (
      node.kind === "infra" &&
      node.size &&
      node.size !== "small" &&
      model.infraById.get(node.id)
    ) {
      drawExternalNodeContent(
        context,
        node,
        bounds,
        style,
        model.infraById.get(node.id)!,
      );
    } else {
      context.fillStyle = style.titleColor;
      context.globalAlpha = focusOpacity(
        palette.isDark ? 1 : 0.95,
        style.focused,
        palette,
      );
      context.font = `11px ${MONO_FONT}`;
      context.textAlign = "center";
      context.textBaseline = "middle";
      context.fillText(node.label, node.x + (node.kind === "infra" ? 6 : 0), node.y - 3);

      if (node.sub) {
        context.fillStyle = style.subColor;
        context.globalAlpha = focusOpacity(
          palette.isDark ? 0.92 : 0.86,
          style.focused,
          palette,
        );
        context.font = `9px ${MONO_FONT}`;
        context.fillText(node.sub, node.x + (node.kind === "infra" ? 6 : 0), node.y + 11);
      }
    }

    context.restore();
  }

  function drawBucketNodeContent(
    context: CanvasRenderingContext2D,
    node: ConnectionNode,
    bounds: ReturnType<typeof nodeBounds>,
    style: {
      palette: TopologyCanvasPalette;
      stroke: string;
      fill: string;
      titleColor: string;
      subColor: string;
      hovered: boolean;
      focused: boolean;
      time: number;
    },
  ) {
    const bucket = node.bucket;
    if (!bucket) return;

    const previewObjects = bucket.previewObjects ?? [];
    const paddingX = node.size === "large" ? 18 : 14;
    const headerY = bounds.top + (node.size === "large" ? 24 : 18);
    const titleWidth = bounds.right - bounds.left - paddingX * 2 - 74;

    context.save();
    roundRect(
      context,
      bounds.left + 1,
      bounds.top + 1,
      bounds.right - bounds.left - 2,
      bounds.bottom - bounds.top - 2,
      7,
    );
    context.clip();

    context.textBaseline = "middle";
    context.textAlign = "left";
    context.fillStyle = style.titleColor;
    context.globalAlpha = focusOpacity(
      style.palette.isDark ? 1 : 0.95,
      style.focused,
      style.palette,
    );
    context.font = `${node.size === "large" ? 13 : 12}px ${MONO_FONT}`;
    context.fillText(
      trimCanvasText(node.label, titleWidth, node.size === "large" ? 22 : 18),
      bounds.left + paddingX,
      headerY,
    );

    context.textAlign = "right";
    context.fillStyle = style.stroke;
    context.globalAlpha = focusOpacity(0.82, style.focused, style.palette);
    context.font = `${node.size === "large" ? 10 : 9}px ${MONO_FONT}`;
    context.fillText(
      formatBytes(bucket.totalSize),
      bounds.right - paddingX,
      headerY,
    );

    context.textAlign = "left";
    context.fillStyle = style.subColor;
    context.globalAlpha = focusOpacity(
      style.palette.isDark ? 0.88 : 0.82,
      style.focused,
      style.palette,
    );
    context.font = `${node.size === "large" ? 10 : 9}px ${MONO_FONT}`;
    context.fillText(
      `${bucket.objects} object${bucket.objects === 1 ? "" : "s"}`,
      bounds.left + paddingX,
      headerY + 18,
    );

    if (node.view === "recent-artifacts") {
      drawBucketRecentArtifacts(
        context,
        node,
        bounds,
        style,
        previewObjects,
        paddingX,
        headerY + 34,
      );
      context.restore();
      return;
    }

    if (node.view === "artifact-grid") {
      drawBucketArtifactGrid(
        context,
        node,
        bounds,
        style,
        previewObjects,
        paddingX,
        headerY + 34,
      );
      context.restore();
      return;
    }

    drawBucketStandardSummary(
      context,
      node,
      bounds,
      style,
      previewObjects,
      paddingX,
      headerY + 34,
    );
    context.restore();
  }

  function drawEventBridgeNodeContent(
    context: CanvasRenderingContext2D,
    node: ConnectionNode,
    bounds: ReturnType<typeof nodeBounds>,
    style: {
      palette: TopologyCanvasPalette;
      stroke: string;
      fill: string;
      titleColor: string;
      subColor: string;
      hovered: boolean;
      focused: boolean;
      time: number;
    },
    rule: EventBridgeRuleSummary,
  ) {
    const paddingX = node.size === "large" ? 18 : 14;
    const headerY = bounds.top + (node.size === "large" ? 24 : 18);
    const panelTop = headerY + 14;
    const panelWidth = bounds.right - bounds.left - paddingX * 2;
    const panelHeight = bounds.bottom - panelTop - paddingX;
    const targetCount = rule.targets?.length ?? 0;
    const detailRows =
      node.size === "large"
        ? [
            ["Schedule", trimCanvasText(rule.scheduleExpression, panelWidth - 74, 34)],
            ["State", rule.state],
            ["Targets", `${targetCount}`],
            ["Next", compactScheduleTime(rule.nextRunAt)],
            ["Previous", compactScheduleTime(rule.lastRunAt)],
          ]
        : [
            ["Schedule", trimCanvasText(rule.scheduleExpression, panelWidth - 70, 32)],
            ["State", rule.state],
            ["Targets", `${targetCount}`],
            ["Next", compactScheduleTime(rule.nextRunAt)],
            ["Previous", compactScheduleTime(rule.lastRunAt)],
          ];

    context.save();
    roundRect(
      context,
      bounds.left + 1,
      bounds.top + 1,
      bounds.right - bounds.left - 2,
      bounds.bottom - bounds.top - 2,
      7,
    );
    context.clip();

    context.textBaseline = "middle";
    context.textAlign = "left";
    context.fillStyle = style.titleColor;
    context.globalAlpha = focusOpacity(
      style.palette.isDark ? 1 : 0.95,
      style.focused,
      style.palette,
    );
    context.font = `${node.size === "large" ? 13 : 12}px ${MONO_FONT}`;
    context.fillText(
      trimCanvasText(node.label, panelWidth - 34, node.size === "large" ? 34 : 30),
      bounds.left + paddingX,
      headerY,
    );

    context.textAlign = "right";
    context.fillStyle = style.stroke;
    context.globalAlpha = focusOpacity(0.86, style.focused, style.palette);
    context.font = `${node.size === "large" ? 10 : 9}px ${MONO_FONT}`;
    context.fillText(
      rule.state === "ENABLED" ? "Live" : "Paused",
      bounds.right - paddingX,
      headerY,
    );

    context.save();
    context.fillStyle = style.stroke;
    context.globalAlpha = focusOpacity(0.1, style.focused, style.palette);
    roundRect(
      context,
      bounds.left + paddingX,
      panelTop,
      panelWidth,
      panelHeight,
      10,
    );
    context.fill();
    context.restore();

    const rowTop = panelTop + (node.size === "large" ? 18 : 16);
    const rowGap = node.size === "large" ? 24 : 19;
    detailRows.forEach(([label, value], index) => {
      const y = rowTop + index * rowGap;
      context.textAlign = "left";
      context.fillStyle = style.subColor;
      context.globalAlpha = focusOpacity(0.82, style.focused, style.palette);
      context.font = `${node.size === "large" ? 10 : 9}px ${MONO_FONT}`;
      context.fillText(label, bounds.left + paddingX + 10, y);

      context.textAlign = "right";
      context.fillStyle = style.titleColor;
      context.globalAlpha = focusOpacity(0.94, style.focused, style.palette);
      context.fillText(value, bounds.right - paddingX - 10, y);
    });

    context.restore();
  }

  function drawFunctionNodeContent(
    context: CanvasRenderingContext2D,
    node: ConnectionNode,
    bounds: ReturnType<typeof nodeBounds>,
    style: {
      palette: TopologyCanvasPalette;
      stroke: string;
      fill: string;
      titleColor: string;
      subColor: string;
      hovered: boolean;
      focused: boolean;
      time: number;
    },
    fn: FunctionSummary,
  ) {
    const paddingX = node.size === "large" ? 18 : 14;
    const headerY = bounds.top + (node.size === "large" ? 24 : 18);
    const panelTop = headerY + 14;
    const panelWidth = bounds.right - bounds.left - paddingX * 2;
    const panelHeight = bounds.bottom - panelTop - paddingX;
    const chipHeight = node.size === "large" ? 24 : 22;
    const runtimeLabel = trimCanvasText(fn.runtime, panelWidth - 28, node.size === "large" ? 34 : 24);
    const detailRows =
      node.size === "large"
        ? [
            ["Memory", `${fn.memoryMB} MB`],
            ["Timeout", `${fn.timeoutSec}s`],
            ["Layers", `${fn.layers}`],
            ["Code", formatBytes(fn.codeSize)],
            ["Version", fn.version || "$LATEST"],
          ]
        : [
            ["Memory", `${fn.memoryMB} MB`],
            ["Timeout", `${fn.timeoutSec}s`],
            ["Layers", `${fn.layers}`],
          ];

    context.save();
    roundRect(
      context,
      bounds.left + 1,
      bounds.top + 1,
      bounds.right - bounds.left - 2,
      bounds.bottom - bounds.top - 2,
      7,
    );
    context.clip();

    context.textBaseline = "middle";
    context.textAlign = "left";
    context.fillStyle = style.titleColor;
    context.globalAlpha = focusOpacity(
      style.palette.isDark ? 1 : 0.95,
      style.focused,
      style.palette,
    );
    context.font = `${node.size === "large" ? 13 : 12}px ${MONO_FONT}`;
    context.fillText(
      trimCanvasText(node.label, panelWidth - 8, node.size === "large" ? 30 : 22),
      bounds.left + paddingX,
      headerY,
    );

    context.save();
    context.fillStyle = style.stroke;
    context.globalAlpha = focusOpacity(0.12, style.focused, style.palette);
    roundRect(
      context,
      bounds.left + paddingX,
      panelTop,
      panelWidth,
      panelHeight,
      10,
    );
    context.fill();
    context.restore();

    context.save();
    context.fillStyle = style.stroke;
    context.globalAlpha = focusOpacity(0.14, style.focused, style.palette);
    roundRect(
      context,
      bounds.left + paddingX + 10,
      panelTop + 10,
      panelWidth - 20,
      chipHeight,
      9,
    );
    context.fill();
    context.restore();

    context.fillStyle = style.stroke;
    context.globalAlpha = focusOpacity(0.92, style.focused, style.palette);
    context.font = `${node.size === "large" ? 11 : 10}px ${MONO_FONT}`;
    context.fillText(
      runtimeLabel,
      bounds.left + paddingX + 18,
      panelTop + 10 + chipHeight / 2,
    );

    const rowTop = panelTop + chipHeight + 28;
    const rowGap = node.size === "large" ? 24 : 22;
    detailRows.forEach(([label, value], index) => {
      const y = rowTop + index * rowGap;
      context.fillStyle = style.subColor;
      context.globalAlpha = focusOpacity(0.84, style.focused, style.palette);
      context.font = `${node.size === "large" ? 10 : 9}px ${MONO_FONT}`;
      context.fillText(label, bounds.left + paddingX + 10, y);

      context.textAlign = "right";
      context.fillStyle = style.titleColor;
      context.globalAlpha = focusOpacity(0.94, style.focused, style.palette);
      context.fillText(value, bounds.right - paddingX - 10, y);
      context.textAlign = "left";
    });

    if (node.size === "large") {
      const footerY = bounds.bottom - paddingX - 10;
      context.fillStyle = style.subColor;
      context.globalAlpha = focusOpacity(0.78, style.focused, style.palette);
      context.font = `9px ${MONO_FONT}`;
      context.fillText(
        trimCanvasText(
          `Updated ${compactTimestamp(fn.lastModified)}`,
          panelWidth - 20,
          34,
        ),
        bounds.left + paddingX + 10,
        footerY,
      );
    }

    context.restore();
  }

  function drawExternalNodeContent(
    context: CanvasRenderingContext2D,
    node: ConnectionNode,
    bounds: ReturnType<typeof nodeBounds>,
    style: {
      palette: TopologyCanvasPalette;
      stroke: string;
      fill: string;
      titleColor: string;
      subColor: string;
      hovered: boolean;
      focused: boolean;
      time: number;
    },
    probe: InfraProbe,
  ) {
    const paddingX = node.size === "large" ? 18 : 14;
    const headerY = bounds.top + (node.size === "large" ? 24 : 18);
    const panelTop = headerY + 14;
    const panelWidth = bounds.right - bounds.left - paddingX * 2;
    const panelHeight = bounds.bottom - panelTop - paddingX;
    const details =
      node.size === "large"
        ? [
            ["Host", trimCanvasText(probe.host, panelWidth - 84, 26)],
            ["Port", `${probe.port}`],
            ["State", probe.status],
            ["Latency", probe.latencyMs > 0 ? `${probe.latencyMs.toFixed(0)}ms` : "n/a"],
            ["Version", probe.version || "unknown"],
          ]
        : [
            ["Host", trimCanvasText(probe.host, panelWidth - 84, 22)],
            ["Port", `${probe.port}`],
            ["State", probe.status],
          ];

    context.save();
    roundRect(
      context,
      bounds.left + 1,
      bounds.top + 1,
      bounds.right - bounds.left - 2,
      bounds.bottom - bounds.top - 2,
      7,
    );
    context.clip();

    context.textBaseline = "middle";
    context.textAlign = "left";
    context.fillStyle = style.titleColor;
    context.globalAlpha = focusOpacity(
      style.palette.isDark ? 1 : 0.95,
      style.focused,
      style.palette,
    );
    context.font = `${node.size === "large" ? 13 : 12}px ${MONO_FONT}`;
    context.fillText(
      trimCanvasText(node.label, panelWidth - 72, node.size === "large" ? 26 : 20),
      bounds.left + paddingX,
      headerY,
    );

    context.textAlign = "right";
    context.fillStyle = style.stroke;
    context.globalAlpha = focusOpacity(0.86, style.focused, style.palette);
    context.font = `${node.size === "large" ? 10 : 9}px ${MONO_FONT}`;
    context.fillText(
      normalizeExternalKindLabel(probe.kind),
      bounds.right - paddingX,
      headerY,
    );

    context.save();
    context.fillStyle = style.stroke;
    context.globalAlpha = focusOpacity(0.1, style.focused, style.palette);
    roundRect(
      context,
      bounds.left + paddingX,
      panelTop,
      panelWidth,
      panelHeight,
      10,
    );
    context.fill();
    context.restore();

    const rowTop = panelTop + 18;
    const rowGap = node.size === "large" ? 24 : 22;
    details.forEach(([label, value], index) => {
      const y = rowTop + index * rowGap;
      context.fillStyle = style.subColor;
      context.globalAlpha = focusOpacity(0.82, style.focused, style.palette);
      context.font = `${node.size === "large" ? 10 : 9}px ${MONO_FONT}`;
      context.textAlign = "left";
      context.fillText(label, bounds.left + paddingX + 10, y);

      context.textAlign = "right";
      context.fillStyle = style.titleColor;
      context.globalAlpha = focusOpacity(0.94, style.focused, style.palette);
      context.fillText(value, bounds.right - paddingX - 10, y);
    });

    context.restore();
  }

  function drawCacheExtensionNodeContent(
    context: CanvasRenderingContext2D,
    node: ConnectionNode,
    bounds: ReturnType<typeof nodeBounds>,
    style: {
      palette: TopologyCanvasPalette;
      stroke: string;
      fill: string;
      titleColor: string;
      subColor: string;
      hovered: boolean;
      focused: boolean;
      time: number;
    },
  ) {
    const paddingX = 14;
    const headerY = bounds.top + 18;
    const panelTop = headerY + 16;
    const panelWidth = bounds.right - bounds.left - paddingX * 2;
    const panelHeight = bounds.bottom - panelTop - paddingX;
    const lambdaEdges = model.edges.functionToCache
      .filter((edge) => edge.to.id === node.id)
      .sort((a, b) => a.from.y - b.from.y);
    const secretEdges = model.edges.cacheToSecret
      .filter((edge) => edge.from.id === node.id)
      .sort((a, b) => a.to.y - b.to.y);
    const lambdaNames = [...new Set(lambdaEdges.map((edge) => edge.from.id))];
    const secretNames = [...new Set(secretEdges.map((edge) => edge.to.id))];
    const visibleLambdaNames = lambdaNames.slice(0, 4);
    const visibleSecretNames = secretNames.slice(0, 4);
    const lambdaColor = kindColor("function", style.palette);
    const secretColor = kindColor("secret", style.palette);
    const cacheColor = kindColor("extension", style.palette);
    const footerHeight = 40;
    const footerTop = bounds.bottom - paddingX - footerHeight;
    const bodyTop = panelTop + 14;
    const bodyBottom = footerTop - 10;
    const bodyHeight = Math.max(92, bodyBottom - bodyTop);
    const columnHeaderY = bodyTop + 10;
    const slotsTop = bodyTop + 30;
    const slotsBottom = bodyBottom - 8;
    const slotCount = Math.max(visibleLambdaNames.length, visibleSecretNames.length, 3);
    const slotGap =
      slotCount <= 1 ? 0 : (slotsBottom - slotsTop) / Math.max(1, slotCount - 1);
    const chipHeight = 18;
    const chipWidth = Math.min(58, panelWidth * 0.27);
    const leftX = bounds.left + paddingX + 8;
    const rightX = bounds.right - paddingX - 8 - chipWidth;
    const cacheWidth = 46;
    const cacheHeight = Math.min(96, Math.max(80, bodyHeight - 18));
    const cacheX = bounds.left + (bounds.right - bounds.left - cacheWidth) / 2;
    const cacheY = bodyTop + (bodyHeight - cacheHeight) / 2;
    const cacheCenterX = cacheX + cacheWidth / 2;
    const readsCount = model.traces.cacheActivity?.count ?? lambdaEdges.length;

    context.save();
    roundRect(
      context,
      bounds.left + 1,
      bounds.top + 1,
      bounds.right - bounds.left - 2,
      bounds.bottom - bounds.top - 2,
      9,
    );
    context.clip();

    context.textBaseline = "middle";
    context.textAlign = "left";
    context.fillStyle = style.titleColor;
    context.globalAlpha = focusOpacity(
      style.palette.isDark ? 1 : 0.95,
      style.focused,
      style.palette,
    );
    context.font = `12px ${MONO_FONT}`;
    context.fillText(
      trimCanvasText(node.label, panelWidth - 52, 26),
      bounds.left + paddingX,
      headerY,
    );

    context.textAlign = "right";
    context.fillStyle = style.subColor;
    context.globalAlpha = focusOpacity(0.82, style.focused, style.palette);
    context.font = `9px ${MONO_FONT}`;
    context.fillText(
      trimCanvasText(node.sub, 74, 14),
      bounds.right - paddingX,
      headerY,
    );

    context.save();
    context.fillStyle = style.stroke;
    context.globalAlpha = focusOpacity(0.08, style.focused, style.palette);
    roundRect(
      context,
      bounds.left + paddingX,
      panelTop,
      panelWidth,
      panelHeight,
      10,
    );
    context.fill();
    context.restore();

    context.textBaseline = "middle";
    context.font = `8px ${MONO_FONT}`;
    context.textAlign = "left";
    context.fillStyle = lambdaColor;
    context.globalAlpha = focusOpacity(0.82, style.focused, style.palette);
    context.fillText("callers", leftX, columnHeaderY);

    context.textAlign = "right";
    context.fillStyle = secretColor;
    context.fillText("secrets", rightX + chipWidth, columnHeaderY);

    context.save();
    context.fillStyle = cacheColor;
    context.globalAlpha = focusOpacity(0.12, style.focused, style.palette);
    roundRect(context, cacheX, cacheY, cacheWidth, cacheHeight, 12);
    context.fill();
    context.restore();

    context.strokeStyle = cacheColor;
    context.globalAlpha = focusOpacity(0.36, style.focused, style.palette);
    context.lineWidth = 1.2;
    context.beginPath();
    context.moveTo(cacheCenterX, cacheY + 12);
    context.lineTo(cacheCenterX, cacheY + cacheHeight - 12);
    context.stroke();

    context.fillStyle = cacheColor;
    context.globalAlpha = focusOpacity(0.8, style.focused, style.palette);
    for (let i = 0; i < 3; i += 1) {
      roundRect(
        context,
        cacheX + 10,
        cacheY + 16 + i * 18,
        cacheWidth - 20,
        8,
        4,
      );
      context.fill();
    }

    context.textAlign = "center";
    context.fillStyle = style.titleColor;
    context.globalAlpha = focusOpacity(0.92, style.focused, style.palette);
    context.font = `9px ${MONO_FONT}`;
    context.fillText("cache", cacheCenterX, cacheY + cacheHeight - 12);

    const drawRelationChip = (
      labels: string[],
      side: "left" | "right",
      color: string,
      total: number,
    ) => {
      labels.forEach((label, index) => {
        const y = slotsTop + slotGap * index;
        const chipX = side === "left" ? leftX : rightX;
        const textX = side === "left" ? chipX + 8 : chipX + chipWidth - 8;
        const lineStartX = side === "left" ? chipX + chipWidth + 4 : cacheX + cacheWidth + 2;
        const lineEndX = side === "left" ? cacheX - 2 : chipX - 4;
        const lineMidX = side === "left" ? cacheX - 12 : cacheX + cacheWidth + 12;

        context.save();
        context.fillStyle = color;
        context.globalAlpha = focusOpacity(0.12, style.focused, style.palette);
        roundRect(context, chipX, y - chipHeight / 2, chipWidth, chipHeight, 7);
        context.fill();
        context.restore();

        context.strokeStyle = color;
        context.globalAlpha = focusOpacity(0.34, style.focused, style.palette);
        context.lineWidth = 1.1;
        context.beginPath();
        context.moveTo(lineStartX, y);
        context.lineTo(lineMidX, y);
        context.lineTo(lineEndX, y);
        context.stroke();

        context.textAlign = side === "left" ? "left" : "right";
        context.fillStyle = style.titleColor;
        context.globalAlpha = focusOpacity(0.92, style.focused, style.palette);
        context.font = `9px ${MONO_FONT}`;
        context.fillText(
          trimCanvasText(label, chipWidth - 16, 10),
          textX,
          y + 0.5,
        );
      });

      if (total > labels.length) {
        const y = Math.min(slotsBottom + 10, footerTop - 8);
        context.textAlign = side === "left" ? "left" : "right";
        context.fillStyle = style.subColor;
        context.globalAlpha = focusOpacity(0.76, style.focused, style.palette);
        context.font = `8px ${MONO_FONT}`;
        context.fillText(
          `+${total - labels.length} more`,
          side === "left" ? leftX + 4 : rightX + chipWidth - 4,
          y,
        );
      }
    };

    drawRelationChip(visibleLambdaNames.slice(0, 3), "left", lambdaColor, lambdaNames.length);
    drawRelationChip(visibleSecretNames.slice(0, 3), "right", secretColor, secretNames.length);

    const metricWidth = Math.floor((panelWidth - 12) / 3);
    const metricY = footerTop + 8;
    const metrics = [
      { color: lambdaColor, value: `${lambdaNames.length}`, label: "callers" },
      { color: cacheColor, value: `${readsCount}`, label: "reads" },
      { color: secretColor, value: `${secretNames.length}`, label: "secrets" },
    ];

    metrics.forEach((metric, index) => {
      const cardX = bounds.left + paddingX + index * (metricWidth + 6);

      context.save();
      context.fillStyle = metric.color;
      context.globalAlpha = focusOpacity(0.1, style.focused, style.palette);
      roundRect(context, cardX, metricY, metricWidth, footerHeight - 8, 8);
      context.fill();
      context.restore();

      context.textAlign = "center";
      context.fillStyle = metric.color;
      context.globalAlpha = focusOpacity(0.94, style.focused, style.palette);
      context.font = `11px ${MONO_FONT}`;
      context.fillText(metric.value, cardX + metricWidth / 2, metricY + 11);

      context.fillStyle = style.subColor;
      context.globalAlpha = focusOpacity(0.76, style.focused, style.palette);
      context.font = `8px ${MONO_FONT}`;
      context.fillText(metric.label, cardX + metricWidth / 2, metricY + 24);
    });

    context.restore();
  }

  function drawBucketStandardSummary(
    context: CanvasRenderingContext2D,
    node: ConnectionNode,
    bounds: ReturnType<typeof nodeBounds>,
    style: {
      palette: TopologyCanvasPalette;
      stroke: string;
      fill: string;
      titleColor: string;
      subColor: string;
      hovered: boolean;
      focused: boolean;
      time: number;
    },
    previewObjects: NonNullable<ConnectionNode["bucket"]>["previewObjects"],
    paddingX: number,
    startY: number,
  ) {
    const previewRows = (previewObjects ?? []).slice(0, node.size === "large" ? 4 : 3);
    const panelTop = startY - 7;
    const panelHeight = Math.max(
      node.size === "large" ? 108 : 100,
      bounds.bottom - panelTop - paddingX,
    );
    const panelWidth = bounds.right - bounds.left - paddingX * 2;
    const headerY = panelTop + 15;
    const itemSlots = Math.max(previewRows.length, node.size === "large" ? 4 : 3);
    const itemsTop = panelTop + (node.size === "large" ? 42 : 46);
    const itemsBottom = panelTop + panelHeight - 12;
    const rowBandHeight = Math.max(
      node.size === "large" ? 24 : 22,
      (itemsBottom - itemsTop) / Math.max(1, itemSlots),
    );
    const rowHeight = Math.min(
      node.size === "large" ? 28 : 24,
      Math.max(node.size === "large" ? 22 : 20, rowBandHeight - 8),
    );

    context.save();
    context.fillStyle = style.stroke;
    context.globalAlpha = focusOpacity(0.08, style.focused, style.palette);
    roundRect(
      context,
      bounds.left + paddingX,
      panelTop,
      panelWidth,
      panelHeight,
      10,
    );
    context.fill();
    context.restore();

    context.textAlign = "left";
    context.textBaseline = "middle";
    context.fillStyle = style.subColor;
    context.globalAlpha = focusOpacity(0.9, style.focused, style.palette);
    context.font = `${node.size === "large" ? 11 : 10}px ${MONO_FONT}`;
    context.fillText(
      "Preview dataset",
      bounds.left + paddingX + 10,
      headerY,
    );

    context.textAlign = "right";
    context.fillStyle = style.stroke;
    context.globalAlpha = focusOpacity(0.86, style.focused, style.palette);
    context.fillText(
      `${previewRows.length} recent`,
      bounds.right - paddingX - 10,
      headerY,
    );

    context.textAlign = "left";
    context.fillStyle = style.titleColor;
    context.globalAlpha = focusOpacity(0.92, style.focused, style.palette);
    context.font = `${node.size === "large" ? 10 : 9}px ${MONO_FONT}`;
    if (previewRows.length > 0) {
      previewRows.forEach((object, index) => {
        const itemTop = itemsTop + rowBandHeight * index + rowBandHeight / 2;

        context.save();
        context.fillStyle = style.stroke;
        context.globalAlpha = focusOpacity(0.08, style.focused, style.palette);
        roundRect(
          context,
          bounds.left + paddingX + 8,
          itemTop - rowHeight / 2,
          panelWidth - 16,
          rowHeight,
          8,
        );
        context.fill();
        context.restore();

        context.fillStyle = style.titleColor;
        context.globalAlpha = focusOpacity(0.92, style.focused, style.palette);
        context.fillText(
          `${index + 1}. ${trimCanvasText(
            bucketObjectLabel(object.key),
            panelWidth - 84,
            node.size === "large" ? 30 : 22,
          )}`,
          bounds.left + paddingX + 10,
          itemTop,
        );

        context.textAlign = "right";
        context.fillStyle = style.subColor;
        context.globalAlpha = focusOpacity(0.8, style.focused, style.palette);
        context.fillText(
          formatBytes(object.size),
          bounds.right - paddingX - 10,
          itemTop,
        );
        context.textAlign = "left";
      });
      return;
    }

    const messageLines = splitCanvasTextLines(
      "Switch to the recent artifacts or artifact grid view for a richer bucket preview.",
      panelWidth - 20,
      node.size === "large" ? 4 : 3,
      node.size === "large" ? 34 : 28,
    );
    messageLines.forEach((line, index) => {
      context.fillText(
        line,
        bounds.left + paddingX + 10,
        itemsTop + 10 + index * 16,
      );
    });
  }

  function drawBucketRecentArtifacts(
    context: CanvasRenderingContext2D,
    node: ConnectionNode,
    bounds: ReturnType<typeof nodeBounds>,
    style: {
      palette: TopologyCanvasPalette;
      stroke: string;
      fill: string;
      titleColor: string;
      subColor: string;
      hovered: boolean;
      focused: boolean;
      time: number;
    },
    previewObjects: NonNullable<ConnectionNode["bucket"]>["previewObjects"],
    paddingX: number,
    startY: number,
  ) {
    const rows = (previewObjects ?? []).slice(0, node.size === "large" ? 4 : 3);
    const rowHeight = node.size === "large" ? 26 : 22;
    const rowWidth = bounds.right - bounds.left - paddingX * 2;

    if (rows.length === 0) {
      context.textAlign = "left";
      context.textBaseline = "middle";
      context.fillStyle = style.subColor;
      context.globalAlpha = focusOpacity(0.88, style.focused, style.palette);
      context.font = `10px ${MONO_FONT}`;
      context.fillText(
        "No recent artifacts available for this bucket yet.",
        bounds.left + paddingX,
        startY + 10,
      );
      return;
    }

    rows.forEach((object, index) => {
      const rowTop = startY + index * (rowHeight + 7);

      context.save();
      context.fillStyle = style.stroke;
      context.globalAlpha = focusOpacity(0.08, style.focused, style.palette);
      roundRect(context, bounds.left + paddingX, rowTop, rowWidth, rowHeight, 9);
      context.fill();
      context.restore();

      context.textBaseline = "middle";
      context.textAlign = "left";
      context.fillStyle = style.titleColor;
      context.globalAlpha = focusOpacity(0.94, style.focused, style.palette);
      context.font = `${node.size === "large" ? 10 : 9}px ${MONO_FONT}`;
      context.fillText(
        trimCanvasText(
          bucketObjectLabel(object.key),
          rowWidth - 78,
          node.size === "large" ? 24 : 18,
        ),
        bounds.left + paddingX + 10,
        rowTop + rowHeight / 2,
      );

      context.textAlign = "right";
      context.fillStyle = style.subColor;
      context.globalAlpha = focusOpacity(0.82, style.focused, style.palette);
      context.fillText(
        formatBytes(object.size),
        bounds.right - paddingX - 10,
        rowTop + rowHeight / 2,
      );
    });
  }

  function drawBucketArtifactGrid(
    context: CanvasRenderingContext2D,
    node: ConnectionNode,
    bounds: ReturnType<typeof nodeBounds>,
    style: {
      palette: TopologyCanvasPalette;
      stroke: string;
      fill: string;
      titleColor: string;
      subColor: string;
      hovered: boolean;
      focused: boolean;
      time: number;
    },
    previewObjects: NonNullable<ConnectionNode["bucket"]>["previewObjects"],
    paddingX: number,
    startY: number,
  ) {
    const gridTop = startY;
    const gridBottom = bounds.bottom - paddingX;
    const gridLeft = bounds.left + paddingX;
    const gridRight = bounds.right - paddingX;
    const gridWidth = gridRight - gridLeft;
    const gridHeight = gridBottom - gridTop;
    const columns = node.size === "large" ? 6 : 5;
    const rows = node.size === "large" ? 5 : 4;
    const slots = columns * rows;
    const gap = node.size === "large" ? 8 : 7;
    const cellWidth = Math.max(
      14,
      (gridWidth - gap * (columns - 1)) / columns,
    );
    const cellHeight = Math.max(
      14,
      (gridHeight - gap * (rows - 1)) / rows,
    );
    const filledCount = Math.min(previewObjects?.length ?? 0, slots);

    context.save();
    context.fillStyle = style.stroke;
    context.globalAlpha = focusOpacity(0.06, style.focused, style.palette);
    roundRect(
      context,
      gridLeft - 4,
      gridTop - 4,
      gridWidth + 8,
      gridHeight + 8,
      12,
    );
    context.fill();
    context.restore();

    for (let index = 0; index < slots; index += 1) {
      const object = (previewObjects ?? [])[index];
      const col = index % columns;
      const row = Math.floor(index / columns);
      const x = gridLeft + col * (cellWidth + gap);
      const y = gridTop + row * (cellHeight + gap);
      const isFilled = index < filledCount;

      context.save();
      context.fillStyle = isFilled
        ? bucketArtifactColor(object?.contentType ?? "", style.palette)
        : style.palette.mutedForeground;
      context.globalAlpha = isFilled ? 0.88 : 0.14;
      roundRect(
        context,
        x,
        y,
        cellWidth,
        cellHeight,
        Math.min(8, Math.min(cellWidth, cellHeight) * 0.22),
      );
      context.fill();

      if (isFilled) {
        context.strokeStyle = style.fill;
        context.globalAlpha = 0.22;
        context.lineWidth = 1;
        context.stroke();
      }
      context.restore();
    }
  }

  function bucketObjectLabel(key: string): string {
    const segments = key.split("/").filter(Boolean);
    return segments[segments.length - 1] ?? key;
  }

  function compactTimestamp(value: string): string {
    const normalized = value?.trim();
    if (!normalized) return "recently";
    const [datePart, timePartRaw = ""] = normalized.replace("Z", "").split("T");
    const timePart = timePartRaw.slice(0, 5);
    return timePart ? `${datePart} ${timePart}` : datePart;
  }

  function compactScheduleTime(value?: string): string {
    if (!value) return "pending";
    return compactTimestamp(value);
  }

  function normalizeExternalKindLabel(kind: string): string {
    switch (kind?.toLowerCase()) {
      case "postgres":
      case "postgresql":
        return "PostgreSQL";
      case "mysql":
        return "MySQL";
      case "redis":
        return "Redis";
      case "mongodb":
      case "mongo":
        return "MongoDB";
      case "http":
      case "https":
        return "HTTP";
      case "docker":
        return "Docker";
      default:
        return kind || "External";
    }
  }

  function trimCanvasText(text: string, width: number, approxChars: number): string {
    const maxChars = Math.max(6, Math.min(approxChars, Math.floor(width / 6.5)));
    return text.length <= maxChars ? text : `${text.slice(0, maxChars - 1)}…`;
  }

  function splitCanvasTextLines(
    text: string,
    width: number,
    maxLines: number,
    approxChars: number,
  ): string[] {
    const maxChars = Math.max(10, Math.min(approxChars, Math.floor(width / 6.5)));
    const words = text.split(/\s+/).filter(Boolean);
    const lines: string[] = [];
    let currentLine = "";

    for (const word of words) {
      const nextLine = currentLine ? `${currentLine} ${word}` : word;
      if (nextLine.length <= maxChars) {
        currentLine = nextLine;
        continue;
      }

      if (currentLine) {
        lines.push(currentLine);
      } else {
        lines.push(trimCanvasText(word, width, maxChars));
      }

      currentLine = word;
      if (lines.length === maxLines - 1) break;
    }

    if (lines.length < maxLines && currentLine) {
      lines.push(currentLine);
    }

    if (lines.length > maxLines) {
      return lines.slice(0, maxLines);
    }

    if (words.length > 0 && lines.length === maxLines) {
      lines[maxLines - 1] = trimCanvasText(lines[maxLines - 1], width, maxChars);
    }

    return lines;
  }

  function bucketArtifactColor(
    contentType: string,
    palette: TopologyCanvasPalette,
  ): string {
    if (contentType.startsWith("image/")) return palette.chart4;
    if (contentType.includes("json")) return palette.chart5;
    if (contentType.startsWith("text/")) return palette.primary;
    if (contentType.includes("pdf") || contentType.includes("zip")) return palette.warning;
    return palette.chart2;
  }

  function drawPlacementConfirmation(
    context: CanvasRenderingContext2D,
    node: ConnectionNode,
    palette: TopologyCanvasPalette,
    time: number,
  ) {
    if (!placementConfirmation) return;

    const now = typeof performance !== "undefined" ? performance.now() : time;
    const elapsed = now - placementConfirmation.startedAt;
    if (elapsed < 0 || elapsed > placementConfirmation.durationMs) return;

    const flashDuration =
      placementConfirmation.durationMs / placementConfirmation.flashes;
    const cycleProgress = (elapsed % flashDuration) / flashDuration;
    const pulseWindow = 0.56;
    if (cycleProgress > pulseWindow) return;

    const pulse = Math.sin((cycleProgress / pulseWindow) * Math.PI);
    const bounds = nodeBounds(node);
    const inset = 3 + pulse * 2;

    context.save();
    context.strokeStyle = palette.warning;
    context.globalAlpha = 0.18 + pulse * 0.38;
    context.lineWidth = 1.4 + pulse * 0.8;
    context.shadowColor = palette.warning;
    context.shadowBlur = 5 + pulse * 5;
    roundRect(
      context,
      bounds.left - inset,
      bounds.top - inset,
      bounds.right - bounds.left + inset * 2,
      bounds.bottom - bounds.top + inset * 2,
      node.kind === "extension" ? 11 : 10,
    );
    context.stroke();
    context.restore();
  }

  function drawActiveDragBorder(
    context: CanvasRenderingContext2D,
    node: ConnectionNode,
    palette: TopologyCanvasPalette,
    now: number,
  ) {
    const bounds = nodeBounds(node);
    const pulseAlpha = palette.isDark
      ? 0.42 + 0.12 * (0.5 + 0.5 * Math.sin(now / 520))
      : 0.58 + 0.1 * (0.5 + 0.5 * Math.sin(now / 520));
    const stroke =
      node.kind === "infra"
        ? palette.warning
        : kindColor(node.kind, palette);

    context.save();
    context.strokeStyle = stroke;
    context.globalAlpha = pulseAlpha;
    context.lineWidth = 1.5;
    context.setLineDash([7, 5]);
    context.lineDashOffset = -(now / 95);
    roundRect(
      context,
      bounds.left - 5,
      bounds.top - 5,
      bounds.right - bounds.left + 10,
      bounds.bottom - bounds.top + 10,
      10,
    );
    context.stroke();
    context.restore();
  }

  function roundRect(
    context: CanvasRenderingContext2D,
    x: number,
    y: number,
    width: number,
    height: number,
    radius: number,
  ) {
    context.beginPath();
    context.moveTo(x + radius, y);
    context.lineTo(x + width - radius, y);
    context.quadraticCurveTo(x + width, y, x + width, y + radius);
    context.lineTo(x + width, y + height - radius);
    context.quadraticCurveTo(x + width, y + height, x + width - radius, y + height);
    context.lineTo(x + radius, y + height);
    context.quadraticCurveTo(x, y + height, x, y + height - radius);
    context.lineTo(x, y + radius);
    context.quadraticCurveTo(x, y, x + radius, y);
    context.closePath();
  }
</script>

<Layer {render} />
