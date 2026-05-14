<script lang="ts">
  import { Layer, type Render } from "svelte-canvas";
  import {
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

    context.fillStyle = palette.foreground;
    context.globalAlpha = palette.isDark ? 0.14 : 0.45;
    for (let x = startX; x <= endX; x += GRID_STEP) {
      for (let y = startY; y <= endY; y += GRID_STEP) {
        context.beginPath();
        context.arc(x, y, 1.1, 0, Math.PI * 2);
        context.fill();
      }
    }
    context.globalAlpha = 1;

    if (!model.hasData) {
      context.fillStyle = palette.isDark ? palette.mutedForeground : palette.foreground;
      context.globalAlpha = 0.5;
      context.font = `11px ${MONO_FONT}`;
      context.textAlign = "center";
      context.textBaseline = "middle";
      context.fillText(
        "No architecture data",
        model.canvasSize.width / 2,
        model.canvasSize.height / 2,
      );
    }

    context.restore();
  };

  function alignDotGrid(value: number) {
    return Math.floor((value - 15) / GRID_STEP) * GRID_STEP + 15;
  }
</script>

<Layer {render} />
