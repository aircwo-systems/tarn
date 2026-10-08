<script lang="ts">
  import { Layer, type Render } from "svelte-canvas";
  import {
    type ViewportTransform,
    type TopologyGraphModel,
  } from "../topology-connection-model";
  import type { TopologyCanvasPalette } from "../topology-canvas-theme";

  const GRID_STEP = 30;
  const MONO_FONT = '"JetBrains Mono Variable", "SF Mono", ui-monospace, monospace';
  // The dot grid as a tile drawn once per colour and repeated, so panning
  // and zooming don't rebuild thousands of dots. Drawn 4x up for crispness.
  const TILE_SCALE = 4;
  let dotTile: { colour: string; pattern: CanvasPattern } | undefined;

  function dotPattern(context: CanvasRenderingContext2D, colour: string): CanvasPattern | null {
    if (dotTile?.colour === colour) return dotTile.pattern;
    const tile = document.createElement("canvas");
    tile.width = tile.height = GRID_STEP * TILE_SCALE;
    const tc = tile.getContext("2d");
    if (!tc) return null;
    tc.fillStyle = colour;
    tc.beginPath();
    tc.arc((GRID_STEP / 2) * TILE_SCALE, (GRID_STEP / 2) * TILE_SCALE, 1.1 * TILE_SCALE, 0, Math.PI * 2);
    tc.fill();
    const pattern = context.createPattern(tile, "repeat");
    if (!pattern) return null;
    pattern.setTransform(new DOMMatrix().scale(1 / TILE_SCALE));
    dotTile = { colour, pattern };
    return pattern;
  }

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

    const pattern = dotPattern(context, palette.foreground);
    if (pattern) {
      context.fillStyle = pattern;
      context.globalAlpha = palette.isDark ? 0.14 : 0.45;
      // Tiles start at canvas 0,0, dots at their centres: 15, 45, 75...
      context.fillRect(startX - 15, startY - 15, endX - startX + 30, endY - startY + 30);
      context.globalAlpha = 1;
    }

    // Each kind's run in its column: a faint frame and a small label.
    context.font = `600 15px ${MONO_FONT}`;
    context.textAlign = "left";
    context.textBaseline = "middle";
    for (const section of model.sections) {
      context.globalAlpha = palette.isDark ? 0.16 : 0.22;
      context.strokeStyle = palette.foreground;
      context.lineWidth = 1.25;
      context.beginPath();
      context.roundRect(section.x, section.y - 6, section.width, section.height + 6, 14);
      context.stroke();
      context.globalAlpha = palette.isDark ? 0.75 : 0.85;
      context.fillStyle = palette.mutedForeground;
      context.fillText(`${section.label.toUpperCase()} · ${section.count}`, section.x + 18, section.y + 16);
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
