<script lang="ts">
  import { Layer, type Render } from "svelte-canvas";
  import {
    CONNECTION_CANVAS,
    type ViewportTransform,
    type TopologyGraphModel,
  } from "../topology-connection-model";
  import type { TopologyCanvasPalette } from "../topology-canvas-theme";

  const GRID_STEP = 30;
  const MONO_FONT = '"JetBrains Mono Variable", "SF Mono", ui-monospace, monospace';

  let {
    model,
    palette,
    viewportTransform,
  }: {
    model: TopologyGraphModel;
    palette: TopologyCanvasPalette;
    viewportTransform: ViewportTransform;
  } = $props();

  const render: Render = ({ context, width, height }) => {
    const minX = (0 - viewportTransform.offsetX) / viewportTransform.scale;
    const minY = (0 - viewportTransform.offsetY) / viewportTransform.scale;
    const maxX = (width - viewportTransform.offsetX) / viewportTransform.scale;
    const maxY = (height - viewportTransform.offsetY) / viewportTransform.scale;
    const startX = alignDotGrid(minX) - GRID_STEP * 2;
    const startY = alignDotGrid(minY) - GRID_STEP * 2;
    const endX = alignDotGrid(maxX) + GRID_STEP * 2;
    const endY = alignDotGrid(maxY) + GRID_STEP * 2;

    context.save();
    context.fillStyle = palette.background;
    context.fillRect(0, 0, width, height);
    context.restore();

    context.save();
    context.translate(viewportTransform.offsetX, viewportTransform.offsetY);
    context.scale(viewportTransform.scale, viewportTransform.scale);

    context.fillStyle = palette.isDark ? palette.foreground : palette.border;
    context.globalAlpha = palette.isDark ? 0.14 : 0.28;
    for (let x = startX; x <= endX; x += GRID_STEP) {
      for (let y = startY; y <= endY; y += GRID_STEP) {
        context.beginPath();
        context.arc(x, y, 1.1, 0, Math.PI * 2);
        context.fill();
      }
    }
    context.globalAlpha = 1;

    context.fillStyle = palette.isDark ? palette.mutedForeground : palette.foreground;
    context.font = `11px ${MONO_FONT}`;
    context.textAlign = "center";
    context.textBaseline = "middle";

    if (model.hasData) {
      context.fillText("API Gateway", CONNECTION_CANVAS.colGateway, 96);
      context.fillText("SQS", CONNECTION_CANVAS.colQueue, 96);
      context.fillText("Lambda", CONNECTION_CANVAS.colFunction, 96);

      if (model.nodes.secrets.length > 0 && model.nodes.cacheExtension) {
        context.fillText("Cache Ext", model.nodes.cacheExtension.x, 96);
        context.fillText("Secrets", CONNECTION_CANVAS.colSecret, 96);
      }

      if (model.nodes.buckets.length > 0) {
        context.fillText("S3", CONNECTION_CANVAS.colBucket, 96);
      }

      if (model.nodes.infra.length > 0) {
        context.fillText(
          "Infra",
          model.infraLane.x + model.infraLane.width / 2,
          model.infraLane.y - 18,
        );
      }
    } else {
      context.fillStyle = palette.isDark ? palette.mutedForeground : palette.foreground;
      context.font = `11px ${MONO_FONT}`;
      context.fillText(
        "No architecture data",
        CONNECTION_CANVAS.width / 2,
        CONNECTION_CANVAS.height / 2,
      );
    }

    context.restore();
  };

  function alignDotGrid(value: number) {
    return Math.floor((value - 15) / GRID_STEP) * GRID_STEP + 15;
  }
</script>

<Layer {render} />
