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
  const gradients = new Map<string, CanvasGradient>();
  // Line colours for function-to-service routes, [light, dark]: picked to
  // stand apart from each other and from the kind colours on the cards.
  const TUBE_LINES: Array<[string, string]> = [
    ["#0b72d9", "#4ea3ff"],
    ["#c2410c", "#fb923c"],
    ["#7c3aed", "#a78bfa"],
    ["#0f766e", "#2dd4bf"],
    ["#be185d", "#f472b6"],
    ["#4d7c0f", "#a3e635"],
  ];
  const PATH_CACHE_LIMIT = 4096;

  let {
    model,
    hoverFocus,
    palette,
    viewportTransform,
    activityAnimationsEnabled = true,
  }: {
    model: TopologyGraphModel;
    hoverFocus: HoverFocusState;
    palette: TopologyCanvasPalette;
    viewportTransform: ViewportTransform;
    activityAnimationsEnabled?: boolean;
  } = $props();

  const render: Render = ({ context, time }) => {
    if (!model.hasData) return;

    context.save();
    context.translate(viewportTransform.offsetX, viewportTransform.offsetY);
    context.scale(viewportTransform.scale, viewportTransform.scale);

    // Out to services, each function's lines are a tube line of their own
    // colour; whether a service is up shows at its card, so a line to one
    // that's down is fainter rather than a different colour.
    for (const edge of model.edges.functionToInfra) {
      const isConnected = edge.isConnected;
      const isActive = !!edge.activity && isConnected;
      const isFocused = isEdgeFocused(edge.id);
      const line = TUBE_LINES[(edge.lane ?? 0) % TUBE_LINES.length][palette.isDark ? 1 : 0];

      drawPath(context, edge.path, {
        stroke: isActive && edge.activity?.hasError ? palette.destructive : line,
        width: activityWidth(isActive ? edge.activity : undefined, px(2.4, 4)),
        opacity: focusOpacity(
          activityOpacity(isActive ? edge.activity : undefined, isConnected ? 0.85 : 0.4),
          isFocused,
          palette,
        ),
        dash: isActive ? [6, 3] : [],
        animateDash: activityAnimationsEnabled && isActive,
        time,
      });

      if (isActive && edge.activity) {
        context.save();
        context.fillStyle = line;
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
        width: activityWidth(edge.activity, px(1.8, 2.75)),
        opacity: focusOpacity(activityOpacity(edge.activity, edge.active ? 0.74 : 0.5), isFocused, palette),
        dash: edge.activity ? [6, 3] : edge.active ? [] : [5, 3],
        animateDash: activityAnimationsEnabled && !!edge.activity,
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
        width: activityWidth(edge.activity, px(1.8, 2.75)),
        opacity: focusOpacity(activityOpacity(edge.activity, edge.active ? 0.82 : 0.58), isFocused, palette),
        dash: edge.activity ? [6, 3] : [],
        animateDash: activityAnimationsEnabled && !!edge.activity,
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
        width: activityWidth(edge.activity, px(1.8, 2.75)),
        opacity: focusOpacity(activityOpacity(edge.activity, 0.7), isFocused, palette),
        dash: edge.activity ? [6, 3] : [],
        animateDash: activityAnimationsEnabled && !!edge.activity,
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
        width: activityWidth(edge.activity, px(1.8, 2.75)),
        opacity: focusOpacity(activityOpacity(edge.activity, 0.7), isFocused, palette),
        dash: edge.activity ? [6, 3] : [],
        animateDash: activityAnimationsEnabled && !!edge.activity,
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
        width: activityWidth(edge.activity, px(1.8, 2.75)),
        opacity: focusOpacity(activityOpacity(edge.activity, 0.76), isFocused, palette),
        dash: edge.activity ? [6, 3] : [],
        animateDash: activityAnimationsEnabled && !!edge.activity,
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
        width: activityWidth(edge.activity, px(1.8, 2.75)),
        opacity: focusOpacity(activityOpacity(edge.activity, 0.72), isFocused, palette),
        dash: edge.activity ? [6, 3] : [],
        animateDash: activityAnimationsEnabled && !!edge.activity,
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

    for (const edge of model.edges.dynamodbToFunction) {
      const isFocused = isEdgeFocused(edge.id);
      const isErr = !!edge.activity?.hasError;
      drawPath(context, edge.path, {
        stroke: isErr
          ? palette.destructive
          : makeEdgeGradient(context, edge.from, edge.to, kindColor("dynamodb", palette), kindColor("function", palette)),
        width: activityWidth(edge.activity, px(1.8, 2.75)),
        opacity: focusOpacity(activityOpacity(edge.activity, 0.72), isFocused, palette),
        dash: edge.activity ? [6, 3] : [],
        animateDash: activityAnimationsEnabled && !!edge.activity,
        time,
      });

      if (edge.filterLabel) {
        const mid = midpoint(edge.from, edge.to);
        context.save();
        context.fillStyle = kindColor("dynamodb", palette);
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
        width: activityWidth(edge.activity, px(1.4, 2)),
        opacity: focusOpacity(activityOpacity(edge.activity, 0.45), isFocused, palette),
        dash: edge.activity ? [5, 2] : [3, 3],
        animateDash: activityAnimationsEnabled && !!edge.activity,
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
        width: px(1.8, 2.75),
        opacity: focusOpacity(0.68, isEdgeFocused(edge.id), palette),
        dash: [],
      });
    }

    for (const edge of model.edges.functionToDynamodb) {
      const isErr = !!edge.activity?.hasError;
      drawPath(context, edge.path, {
        stroke: isErr
          ? palette.destructive
          : makeEdgeGradient(context, edge.from, edge.to, kindColor("function", palette), kindColor("dynamodb", palette)),
        width: activityWidth(edge.activity, px(2.4, 4)),
        opacity: focusOpacity(activityOpacity(edge.activity, 0.7), isEdgeFocused(edge.id), palette),
        dash: edge.activity ? [6, 3] : [],
        animateDash: activityAnimationsEnabled && !!edge.activity,
        time,
      });
    }

    // Any function may read any secret through the cache: a bracket in from
    // the functions, one out to the secrets, not a line for every pair.
    if (model.cacheBus?.in) {
      drawPath(context, model.cacheBus.in, {
        stroke: kindColor("extension", palette),
        width: px(2.4, 4),
        opacity: focusOpacity(0.65, model.edges.functionToCache.some((edge) => isEdgeFocused(edge.id)), palette),
        dash: [],
        animateDash: false,
        time,
      });
    }
    if (model.cacheBus?.out) {
      drawPath(context, model.cacheBus.out, {
        stroke: kindColor("secret", palette),
        width: px(2, 3),
        opacity: focusOpacity(0.7, model.edges.cacheToSecret.some((edge) => isEdgeFocused(edge.id)), palette),
        dash: [],
        animateDash: false,
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


    context.restore();
  };

  /**
   * A width in canvas units that never draws thinner than `screen` pixels,
   * so lines keep their weight in the zoomed-out, whole-graph view.
   */
  function px(units: number, screen: number): number {
    return Math.max(units, screen / Math.max(0.05, viewportTransform.scale));
  }

  function makeEdgeGradient(
    context: CanvasRenderingContext2D,
    from: ConnectionNode,
    to: ConnectionNode,
    fromColor: string,
    toColor: string,
  ): CanvasGradient {
    const start = portPos(from, "output");
    const end   = portPos(to,   "input");
    // Reused while nothing moves: animating frames would otherwise make a
    // new gradient for every line, every frame.
    const key = `${start.x},${start.y},${end.x},${end.y},${fromColor},${toColor}`;
    const known = gradients.get(key);
    if (known) return known;
    if (gradients.size > 2000) gradients.clear();
    const grad = context.createLinearGradient(start.x, start.y, end.x, end.y);
    grad.addColorStop(0, fromColor);
    grad.addColorStop(1, toColor);
    gradients.set(key, grad);
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
    const shape = getPath(path);
    // Dashes grow with the line, so thick lines keep visible gaps.
    const grow = Math.max(1, options.width / 2);
    context.save();
    context.lineCap = "round";
    context.lineJoin = "round";
    // A casing in the background colour first, like a metro map, so
    // where lines cross each one reads clearly over the other.
    context.strokeStyle = palette.background;
    context.lineWidth = options.width + px(2, 3);
    context.globalAlpha = Math.min(1, options.opacity * 1.4);
    context.stroke(shape);
    context.strokeStyle = options.stroke as string;
    context.lineWidth = options.width;
    context.globalAlpha = options.opacity;
    context.setLineDash((options.dash ?? []).map((d) => d * grow));
    context.lineDashOffset = options.animateDash ? -((options.time ?? 0) / 90) * grow : 0;
    context.stroke(shape);
    context.restore();
  }

  function getPath(path: string): Path2D {
    const cached = pathCache.get(path);
    if (cached) return cached;
    if (pathCache.size >= PATH_CACHE_LIMIT) {
      pathCache.clear();
    }
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
