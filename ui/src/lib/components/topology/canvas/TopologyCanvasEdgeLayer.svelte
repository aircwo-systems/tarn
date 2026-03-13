<script lang="ts">
  import { Layer, type Render } from "svelte-canvas";
  import {
    activityOpacity,
    activityWidth,
    infraKindTone,
    type ViewportTransform,
    type HoverFocusState,
    type TopologyGraphModel,
  } from "../topology-connection-model";
  import type { TopologyCanvasPalette } from "../topology-canvas-theme";

  const MONO_FONT = '"JetBrains Mono Variable", "SF Mono", ui-monospace, monospace';
  const pathCache = new Map<string, Path2D>();

  let {
    model,
    hoverFocus,
    palette,
    viewportTransform,
  }: {
    model: TopologyGraphModel;
    hoverFocus: HoverFocusState;
    palette: TopologyCanvasPalette;
    viewportTransform: ViewportTransform;
  } = $props();

  const render: Render = ({ context, time }) => {
    if (!model.hasData) return;

    context.save();
    context.translate(viewportTransform.offsetX, viewportTransform.offsetY);
    context.scale(viewportTransform.scale, viewportTransform.scale);

    // Infra lane sits below nodes but above background.
    if (model.nodes.infra.length > 0) {
      const hasInfraFocus = model.nodes.infra.some((node) => hoverFocus.nodeIds.has(node.id));
      context.save();
      context.fillStyle = palette.muted;
      context.globalAlpha = focusOpacity(0.42, hoverFocus.active ? hasInfraFocus : true, palette);
      context.strokeStyle = palette.isDark ? palette.border : palette.foreground;
      context.lineWidth = 1;
      roundRect(
        context,
        model.infraLane.x,
        model.infraLane.y,
        model.infraLane.width,
        model.infraLane.height,
        12,
      );
      context.fill();
      context.globalAlpha = focusOpacity(0.78, hoverFocus.active ? hasInfraFocus : true, palette);
      context.stroke();
      context.restore();
    }

    for (const edge of model.edges.functionToInfra) {
      const edgeColor = edge.isConnected
        ? toneColor(infraKindTone(edge.probe?.kind ?? ""), palette)
        : palette.destructive;
      const isActive = !!edge.activity && edge.isConnected;
      const stroke = isActive && edge.activity?.hasError ? palette.destructive : edgeColor;
      const isFocused = isEdgeFocused(edge.id);

      drawPath(context, edge.path, {
        stroke,
        width: activityWidth(isActive ? edge.activity : undefined, 1.0),
        opacity: focusOpacity(
          activityOpacity(
            isActive ? edge.activity : undefined,
            edge.isConnected ? 0.28 : 0.16,
          ),
          isFocused,
          palette,
        ),
        dash: isActive ? [6, 3] : edge.isConnected ? [5, 4] : [3, 3],
        animateDash: isActive,
        time,
      });

      if (isActive && edge.activity) {
        context.save();
        context.fillStyle = edgeColor;
        context.globalAlpha = focusOpacity(0.76, isFocused, palette);
        context.font = `6.5px ${MONO_FONT}`;
        context.textAlign = "center";
        context.textBaseline = "middle";
        const laneOffset =
          edge.laneCount > 1 ? (edge.lane - (edge.laneCount - 1) / 2) * 11 : 0;
        context.fillText(
          `${edge.activity.latestMs}ms`,
          model.infraRoute.x + laneOffset,
          model.infraRoute.y - 5,
        );
        context.restore();
      }
    }

    for (const edge of model.edges.apigwToQueue) {
      const isFocused = isEdgeFocused(edge.id);
      drawPath(context, edge.path, {
        stroke: edge.activity?.hasError
          ? palette.destructive
          : edge.activity
            ? palette.primary
            : palette.chart1,
        width: activityWidth(edge.activity, edge.active ? 1.45 : 1.2),
        opacity: focusOpacity(activityOpacity(edge.activity, edge.active ? 0.74 : 0.5), isFocused, palette),
        dash: edge.activity ? [6, 3] : edge.active ? [] : [5, 3],
        animateDash: !!edge.activity,
        time,
      });
    }

    for (const edge of model.edges.apigwToFunction) {
      const isFocused = isEdgeFocused(edge.id);
      drawPath(context, edge.path, {
        stroke: edge.activity?.hasError
          ? palette.destructive
          : edge.activity
            ? palette.primary
            : palette.chart1,
        width: activityWidth(edge.activity, edge.active ? 1.45 : 1.2),
        opacity: focusOpacity(activityOpacity(edge.activity, edge.active ? 0.82 : 0.58), isFocused, palette),
        dash: edge.activity ? [6, 3] : [],
        animateDash: !!edge.activity,
        time,
      });

      if (edge.activity) {
        const mid = midpoint(edge.from, edge.to);
        context.save();
        context.fillStyle = palette.primary;
        context.globalAlpha = focusOpacity(0.88, isFocused, palette);
        context.font = `7px ${MONO_FONT}`;
        context.textAlign = "center";
        context.textBaseline = "middle";
        context.fillText(
          `${edge.activity.count} req · ${edge.activity.latestMs}ms`,
          mid.x,
          mid.y - 7,
        );
        context.restore();
      }
    }

    for (const edge of model.edges.queueToFunction) {
      const isFocused = isEdgeFocused(edge.id);
      drawPath(context, edge.path, {
        stroke: edge.activity?.hasError
          ? palette.destructive
          : edge.activity
            ? palette.primary
            : palette.chart4,
        width: activityWidth(edge.activity, 1.35),
        opacity: focusOpacity(activityOpacity(edge.activity, 0.72), isFocused, palette),
        dash: edge.activity ? [6, 3] : [],
        animateDash: !!edge.activity,
        time,
      });

      const mid = midpoint(edge.from, edge.to);
      if (edge.activity) {
        context.save();
        context.fillStyle = palette.warning;
        context.globalAlpha = focusOpacity(0.88, isFocused, palette);
        context.font = `7px ${MONO_FONT}`;
        context.textAlign = "center";
        context.textBaseline = "middle";
        context.fillText(
          `${edge.activity.count} msg · ${edge.activity.latestMs}ms`,
          mid.x,
          mid.y - 7,
        );
        context.restore();
      } else if (edge.filterLabel) {
        context.save();
        context.fillStyle = palette.warning;
        context.globalAlpha = focusOpacity(0.68, isFocused, palette);
        context.font = `6.5px ${MONO_FONT}`;
        context.textAlign = "center";
        context.textBaseline = "middle";
        context.fillText(`⊘ ${edge.filterLabel}`, mid.x, mid.y - 6);
        context.restore();
      }
    }

    for (const edge of model.edges.queueToDlq) {
      const isFocused = isEdgeFocused(edge.id);
      drawPath(context, edge.path, {
        stroke: palette.destructive,
        width: activityWidth(edge.activity, 1.2),
        opacity: focusOpacity(activityOpacity(edge.activity, 0.45), isFocused, palette),
        dash: edge.activity ? [5, 2] : [3, 3],
        animateDash: !!edge.activity,
        time,
      });

      if (edge.activity) {
        context.save();
        context.fillStyle = palette.destructive;
        context.globalAlpha = focusOpacity(0.88, isFocused, palette);
        context.font = `7px ${MONO_FONT}`;
        context.textAlign = "center";
        context.textBaseline = "middle";
        context.fillText(
          `${edge.activity.count} dlq`,
          edge.from.x - 80,
          (edge.from.y + edge.to.y) / 2,
        );
        context.restore();
      }
    }

    for (const edge of model.edges.bucketToFunction) {
      drawPath(context, edge.path, {
        stroke: palette.chart5,
        width: 1.35,
        opacity: focusOpacity(0.68, isEdgeFocused(edge.id), palette),
        dash: [4, 3],
      });
    }

    for (const edge of model.edges.functionToCache) {
      drawPath(context, edge.path, {
        stroke: edge.activity?.hasError ? palette.destructive : palette.chart2,
        width: activityWidth(edge.activity, 1.1),
        opacity: focusOpacity(activityOpacity(edge.activity, 0.48), isEdgeFocused(edge.id), palette),
        dash: edge.activity ? [6, 3] : [5, 4],
        animateDash: !!edge.activity,
        time,
      });
    }

    if (model.traces.cacheActivity && model.nodes.cacheExtension) {
      const activity = model.traces.cacheActivity;
      const cacheFocused = hoverFocus.nodeIds.has(model.nodes.cacheExtension.id);
      context.save();
      context.fillStyle = palette.blue;
      context.globalAlpha = focusOpacity(0.84, cacheFocused, palette);
      context.font = `7px ${MONO_FONT}`;
      context.textAlign = "center";
      context.textBaseline = "middle";
      context.fillText(
        `${activity.count} call${activity.count !== 1 ? "s" : ""} · ${activity.latestMs}ms`,
        model.nodes.cacheExtension.x,
        model.nodes.cacheExtension.y - 26,
      );
      context.restore();
    }

    for (const edge of model.edges.cacheToSecret) {
      drawPath(context, edge.path, {
        stroke: model.traces.cacheActivity?.hasError ? palette.destructive : palette.chart2,
        width: activityWidth(model.traces.cacheActivity, 1.2),
        opacity: focusOpacity(
          activityOpacity(model.traces.cacheActivity, 0.58),
          isEdgeFocused(edge.id),
          palette,
        ),
        dash: model.traces.cacheActivity ? [6, 3] : [],
        animateDash: !!model.traces.cacheActivity,
        time,
      });
    }

    context.restore();
  };

  function isEdgeFocused(edgeId: string): boolean {
    if (!hoverFocus.active) return true;
    return hoverFocus.edgeIds.has(edgeId);
  }

  function focusOpacity(
    base: number,
    isFocused: boolean,
    palette: TopologyCanvasPalette,
  ): number {
    const boostedBase = palette.isDark ? base : Math.min(1, base * 1.38 + 0.05);
    if (!hoverFocus.active) return boostedBase;

    const dimmed = boostedBase * (palette.isDark ? 0.18 : 0.1);
    return isFocused ? boostedBase : dimmed;
  }

  function drawPath(
    context: CanvasRenderingContext2D,
    path: string,
    options: {
      stroke: string;
      width: number;
      opacity: number;
      dash?: number[];
      animateDash?: boolean;
      time?: number;
    },
  ) {
    context.save();
    context.strokeStyle = options.stroke;
    context.lineWidth = options.width;
    context.globalAlpha = options.opacity;
    context.lineCap = "round";
    context.lineJoin = "round";
    context.setLineDash(options.dash ?? []);
    context.lineDashOffset = options.animateDash ? -((options.time ?? 0) / 90) : 0;
    context.stroke(getPath(path));
    context.restore();
  }

  function getPath(path: string): Path2D {
    const cached = pathCache.get(path);
    if (cached) return cached;
    const parsed = new Path2D(path);
    pathCache.set(path, parsed);
    return parsed;
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

  function midpoint(from: { x: number; y: number }, to: { x: number; y: number }) {
    return {
      x: (from.x + to.x) / 2,
      y: (from.y + to.y) / 2,
    };
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
