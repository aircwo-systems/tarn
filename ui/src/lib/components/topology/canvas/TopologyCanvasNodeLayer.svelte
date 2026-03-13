<script lang="ts">
  import { Layer, type Render } from "svelte-canvas";
  import type { RequestTrace } from "$lib/types";
  import {
    CONNECTION_CANVAS,
    infraKindTone,
    nodeBounds,
    selectedTraceNodes,
    traceStatusTone,
    type HoverFocusState,
    type TopologyGraphModel,
    type ViewportTransform,
  } from "../topology-connection-model";
  import type { TopologyCanvasPalette } from "../topology-canvas-theme";
  import type { ConnectionNode } from "../types";

  const MONO_FONT = '"JetBrains Mono Variable", "SF Mono", ui-monospace, monospace';

  let {
    model,
    selectedTrace = null,
    hoverFocus,
    palette,
    viewportTransform,
    hoveredNodeId = null,
  }: {
    model: TopologyGraphModel;
    selectedTrace?: RequestTrace | null;
    hoverFocus: HoverFocusState;
    palette: TopologyCanvasPalette;
    viewportTransform: ViewportTransform;
    hoveredNodeId?: string | null;
  } = $props();

  const render: Render = ({ context, width, height, time }) => {
    if (!model.hasData) return;

    context.save();
    context.translate(viewportTransform.offsetX, viewportTransform.offsetY);
    context.scale(viewportTransform.scale, viewportTransform.scale);

    for (const node of model.nodes.gateways) {
      drawNode(context, node, {
        palette,
        stroke: palette.chart1,
        fill: palette.popover,
        titleColor: palette.foreground,
        subColor: palette.mutedForeground,
        hovered: hoveredNodeId === node.id,
        focused: isFocused(node.id),
        time,
      });
    }

    for (const node of model.nodes.queues) {
      drawNode(context, node, {
        palette,
        stroke: palette.chart4,
        fill: palette.popover,
        titleColor: palette.foreground,
        subColor: palette.mutedForeground,
        hovered: hoveredNodeId === node.id,
        focused: isFocused(node.id),
        time,
      });
    }

    for (const node of model.nodes.functions) {
      drawNode(context, node, {
        palette,
        stroke: palette.primary,
        fill: palette.popover,
        titleColor: palette.foreground,
        subColor: palette.mutedForeground,
        hovered: hoveredNodeId === node.id,
        focused: isFocused(node.id),
        time,
      });
    }

    if (model.nodes.cacheExtension) {
      drawNode(context, model.nodes.cacheExtension, {
        palette,
        stroke: palette.chart2,
        fill: palette.popover,
        titleColor: palette.foreground,
        subColor: palette.mutedForeground,
        hovered: hoveredNodeId === model.nodes.cacheExtension.id,
        focused: isFocused(model.nodes.cacheExtension.id),
        time,
      });
    }

    for (const node of model.nodes.secrets) {
      drawNode(context, node, {
        palette,
        stroke: palette.chart2,
        fill: palette.popover,
        titleColor: palette.foreground,
        subColor: palette.mutedForeground,
        hovered: hoveredNodeId === node.id,
        focused: isFocused(node.id),
        time,
      });
    }

    for (const node of model.nodes.buckets) {
      drawNode(context, node, {
        palette,
        stroke: palette.primary,
        fill: palette.popover,
        titleColor: palette.foreground,
        subColor: palette.mutedForeground,
        hovered: hoveredNodeId === node.id,
        focused: isFocused(node.id),
        time,
      });
    }

    for (const node of model.nodes.infra) {
      const probe = model.infraById.get(node.id);
      const isConnected = node.status === "connected";
      const stroke = isConnected
        ? toneColor(infraKindTone(probe?.kind ?? ""), palette)
        : palette.destructive;

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

      context.save();
      context.beginPath();
      context.arc(
        node.x - CONNECTION_CANVAS.infraHalfWidth + 14,
        node.y,
        4.5,
        0,
        Math.PI * 2,
      );
      context.fillStyle = stroke;
      context.globalAlpha = focusOpacity(
        isConnected ? 0.35 + 0.45 * (0.5 + 0.5 * Math.sin(time / 380)) : 0.78,
        isFocused(node.id),
        palette,
      );
      context.fill();
      context.restore();
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

    context.fillStyle = style.titleColor;
    context.globalAlpha = focusOpacity(palette.isDark ? 1 : 0.95, style.focused, palette);
    context.font = `11px ${MONO_FONT}`;
    context.textAlign = "center";
    context.textBaseline = "middle";
    context.fillText(node.label, node.x + (node.kind === "infra" ? 6 : 0), node.y - 3);

    if (node.sub) {
      context.fillStyle = style.subColor;
      context.globalAlpha = focusOpacity(palette.isDark ? 0.92 : 0.86, style.focused, palette);
      context.font = `9px ${MONO_FONT}`;
      context.fillText(node.sub, node.x + (node.kind === "infra" ? 6 : 0), node.y + 11);
    }

    context.restore();
  }

  function toneColor(
    tone: "db" | "cache" | "service" | "primary",
    palette: TopologyCanvasPalette,
  ): string {
    switch (tone) {
      case "db":
        return palette.chart2;
      case "cache":
        return palette.chart4;
      case "service":
        return palette.chart5;
      default:
        return palette.primary;
    }
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
