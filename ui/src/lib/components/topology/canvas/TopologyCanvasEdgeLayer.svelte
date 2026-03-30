<script lang="ts">
  import { Layer, type Render } from "svelte-canvas";
  import {
    activityOpacity,
    activityWidth,
    portPos,
    type ViewportTransform,
    type HoverFocusState,
    type TopologyGraphModel,
  } from "../topology-connection-model";
  import {
    infraKindColor,
    kindColor,
    type TopologyCanvasPalette,
  } from "../topology-canvas-theme";
  import type { ConnectionNode } from "../types";

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

    for (const edge of model.edges.functionToInfra) {
      const probe = edge.probe;
      const isConnected = edge.isConnected;
      const infraColor = isConnected
        ? infraKindColor(probe?.kind ?? "", palette)
        : palette.chart5;
      const isActive = !!edge.activity && isConnected;
      const isFocused = isEdgeFocused(edge.id);

      const grad = isActive && edge.activity?.hasError
        ? null
        : makeEdgeGradient(context, edge.from, edge.to, kindColor("function", palette), infraColor);

      drawPath(context, edge.path, {
        stroke: isActive && edge.activity?.hasError ? palette.destructive : (grad ?? infraColor),
        width: activityWidth(isActive ? edge.activity : undefined, 1.0),
        opacity: focusOpacity(
          activityOpacity(isActive ? edge.activity : undefined, isConnected ? 0.6 : 0.3),
          isFocused,
          palette,
        ),
        dash: isActive ? [6, 3] : isConnected ? [] : [5, 4],
        animateDash: isActive,
        time,
      });

      if (isActive && edge.activity) {
        context.save();
        context.fillStyle = infraColor;
        context.globalAlpha = focusOpacity(0.76, isFocused, palette);
        context.font = `6.5px ${MONO_FONT}`;
        context.textAlign = "center";
        context.textBaseline = "middle";
        const midX = (edge.from.x + edge.to.x) / 2;
        const midY = (edge.from.y + edge.to.y) / 2;
        context.fillText(`${edge.activity.latestMs}ms`, midX, midY - 7);
        context.restore();
      }
    }

    for (const edge of model.edges.apigwToQueue) {
      const isFocused = isEdgeFocused(edge.id);
      const isErr = !!edge.activity?.hasError;
      drawPath(context, edge.path, {
        stroke: isErr
          ? palette.destructive
          : makeEdgeGradient(context, edge.from, edge.to, kindColor("gateway", palette), kindColor("queue", palette)),
        width: activityWidth(edge.activity, edge.active ? 1.45 : 1.2),
        opacity: focusOpacity(activityOpacity(edge.activity, edge.active ? 0.74 : 0.5), isFocused, palette),
        dash: edge.activity ? [6, 3] : edge.active ? [] : [5, 3],
        animateDash: !!edge.activity,
        time,
      });
    }

    for (const edge of model.edges.apigwToFunction) {
      const isFocused = isEdgeFocused(edge.id);
      const isErr = !!edge.activity?.hasError;
      drawPath(context, edge.path, {
        stroke: isErr
          ? palette.destructive
          : makeEdgeGradient(context, edge.from, edge.to, kindColor("gateway", palette), kindColor("function", palette)),
        width: activityWidth(edge.activity, edge.active ? 1.45 : 1.2),
        opacity: focusOpacity(activityOpacity(edge.activity, edge.active ? 0.82 : 0.58), isFocused, palette),
        dash: edge.activity ? [6, 3] : [],
        animateDash: !!edge.activity,
        time,
      });

      if (edge.activity) {
        const mid = midpoint(edge.from, edge.to);
        context.save();
        context.fillStyle = kindColor("function", palette);
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

    for (const edge of model.edges.eventbridgeToFunction) {
      const isFocused = isEdgeFocused(edge.id);
      const isErr = !!edge.activity?.hasError;
      drawPath(context, edge.path, {
        stroke: isErr
          ? palette.destructive
          : makeEdgeGradient(context, edge.from, edge.to, kindColor("eventbridge", palette), kindColor("function", palette)),
        width: activityWidth(edge.activity, 1.25),
        opacity: focusOpacity(activityOpacity(edge.activity, 0.7), isFocused, palette),
        dash: edge.activity ? [6, 3] : [5, 4],
        animateDash: !!edge.activity,
        time,
      });
    }

    for (const edge of model.edges.snsToQueue) {
      const isFocused = isEdgeFocused(edge.id);
      const isErr = !!edge.activity?.hasError;
      drawPath(context, edge.path, {
        stroke: isErr
          ? palette.destructive
          : makeEdgeGradient(context, edge.from, edge.to, kindColor("topic", palette), kindColor("queue", palette)),
        width: activityWidth(edge.activity, 1.3),
        opacity: focusOpacity(activityOpacity(edge.activity, 0.7), isFocused, palette),
        dash: edge.activity ? [6, 3] : [5, 4],
        animateDash: !!edge.activity,
        time,
      });
    }

    for (const edge of model.edges.snsToFunction) {
      const isFocused = isEdgeFocused(edge.id);
      const isErr = !!edge.activity?.hasError;
      drawPath(context, edge.path, {
        stroke: isErr
          ? palette.destructive
          : makeEdgeGradient(context, edge.from, edge.to, kindColor("topic", palette), kindColor("function", palette)),
        width: activityWidth(edge.activity, 1.3),
        opacity: focusOpacity(activityOpacity(edge.activity, 0.76), isFocused, palette),
        dash: edge.activity ? [6, 3] : [5, 4],
        animateDash: !!edge.activity,
        time,
      });
    }

    for (const edge of model.edges.queueToFunction) {
      const isFocused = isEdgeFocused(edge.id);
      const isErr = !!edge.activity?.hasError;
      drawPath(context, edge.path, {
        stroke: isErr
          ? palette.destructive
          : makeEdgeGradient(context, edge.from, edge.to, kindColor("queue", palette), kindColor("function", palette)),
        width: activityWidth(edge.activity, 1.35),
        opacity: focusOpacity(activityOpacity(edge.activity, 0.72), isFocused, palette),
        dash: edge.activity ? [6, 3] : [],
        animateDash: !!edge.activity,
        time,
      });

      const mid = midpoint(edge.from, edge.to);
      if (edge.activity) {
        context.save();
        context.fillStyle = kindColor("queue", palette);
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
        context.fillStyle = kindColor("queue", palette);
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
        stroke: makeEdgeGradient(context, edge.from, edge.to, kindColor("bucket", palette), kindColor("function", palette)),
        width: 1.35,
        opacity: focusOpacity(0.68, isEdgeFocused(edge.id), palette),
        dash: [4, 3],
      });
    }

    for (const edge of model.edges.functionToCache) {
      const isErr = !!edge.activity?.hasError;
      drawPath(context, edge.path, {
        stroke: isErr
          ? palette.destructive
          : makeEdgeGradient(context, edge.from, edge.to, kindColor("function", palette), kindColor("extension", palette)),
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
      context.fillStyle = kindColor("extension", palette);
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
      const isErr = !!model.traces.cacheActivity?.hasError;
      drawPath(context, edge.path, {
        stroke: isErr
          ? palette.destructive
          : makeEdgeGradient(context, edge.from, edge.to, kindColor("extension", palette), kindColor("secret", palette)),
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

  function makeEdgeGradient(
    context: CanvasRenderingContext2D,
    from: ConnectionNode,
    to: ConnectionNode,
    fromColor: string,
    toColor: string,
  ): CanvasGradient {
    const start = portPos(from, "output");
    const end   = portPos(to,   "input");
    const grad = context.createLinearGradient(start.x, start.y, end.x, end.y);
    grad.addColorStop(0, fromColor);
    grad.addColorStop(1, toColor);
    return grad;
  }

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
      stroke: string | CanvasGradient;
      width: number;
      opacity: number;
      dash?: number[];
      animateDash?: boolean;
      time?: number;
    },
  ) {
    context.save();
    context.strokeStyle = options.stroke as string;
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

  function midpoint(from: { x: number; y: number }, to: { x: number; y: number }) {
    return {
      x: (from.x + to.x) / 2,
      y: (from.y + to.y) / 2,
    };
  }
</script>

<Layer {render} />
