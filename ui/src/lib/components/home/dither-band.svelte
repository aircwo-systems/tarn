<script lang="ts" module>
  export interface Lamp {
    /** the window's centre and opening, as fractions of the picture */
    x: number;
    y: number;
    w: number;
    h: number;
    /** where the far shore meets the water, as a fraction of its height */
    waterline: number;
    /** how bright, 0 to 1: lower by day */
    strength: number;
  }
</script>

<script lang="ts">
  import { BAYER8, colourOf } from "./dither";

  // DitherBand draws a picture with a halftone grain: an ordered (Bayer)
  // dither of its lightness, strongest along edges and in gradients and
  // faint in flat areas, so it reads as the painting first and the dots
  // second. Toward its foot the picture dissolves dot by dot into the stage
  // behind it, in light and dark alike. It renders once per size on a small
  // canvas scaled up with hard pixels; no timers, no animation frames.
  // Ported from Berth's DitherBand. Two more modes: ink draws the dots in
  // one colour (the accent) on the stage, and smooth shows the picture as
  // is, fading out at its foot.
  // A lamp, when given, is drawn on a layer of its own over the picture, on
  // the same grid of dots: a lit window, its light thrown on the wall about
  // it and its broken reflection in the water. Showing it is a fade, not a
  // redraw.

  let {
    src,
    position = 0.5,
    cell = 2,
    levels = 6,
    fade = 0.4,
    mute = 0,
    mode = "dither",
    lamp,
    lampLit = false,
    class: className = "",
  }: {
    src: string;
    /** where the picture sits when cropped: 0 its top, 1 its foot */
    position?: number;
    /** CSS px per dot */
    cell?: number;
    /** steps of lightness the dots move between */
    levels?: number;
    /** how much of the band, from its foot, dissolves */
    fade?: number;
    /** how far to mute the picture toward the stage, 0 to 1 */
    mute?: number;
    mode?: "dither" | "ink" | "smooth";
    lamp?: Lamp;
    lampLit?: boolean;
    class?: string;
  } = $props();


  let wrap = $state<HTMLDivElement>();
  let canvas = $state<HTMLCanvasElement>();
  let lampCanvas = $state<HTMLCanvasElement>();
  let drawn = $state(false);

  $effect(() => {
    const el = wrap;
    const cv = canvas;
    const lampCv = lampCanvas;
    // Read every input so a change redraws.
    const opts = { src, position, cell, levels, fade, mute, mode, lamp: lamp ? { ...lamp } : undefined };
    if (!el || !cv || opts.mode === "smooth") return;
    let alive = true;
    let pending = false;
    let timer = 0;
    let size = "";

    const draw = async (force = false) => {
      if (document.hidden) {
        pending = true;
        return;
      }
      pending = false;
      const w = Math.ceil(el.clientWidth / opts.cell);
      const h = Math.ceil(el.clientHeight / opts.cell);
      if (!w || !h || (!force && `${w}x${h}` === size)) return;
      const img = new Image();
      img.src = opts.src;
      try {
        await img.decode();
      } catch {
        return;
      }
      if (!alive) return;
      size = `${w}x${h}`;
      cv.width = w;
      cv.height = h;
      cv.style.width = `${w * opts.cell}px`;
      cv.style.height = `${h * opts.cell}px`;
      const ctx = cv.getContext("2d", { willReadFrequently: true });
      if (!ctx) return;
      const scale = Math.max(w / img.naturalWidth, h / img.naturalHeight);
      const dw = img.naturalWidth * scale;
      const dh = img.naturalHeight * scale;
      ctx.clearRect(0, 0, w, h);
      // Drawn small, then up: the brushwork softens to its forms, so the
      // grain is the dither's, not the paint's.
      const soft = document.createElement("canvas");
      soft.width = Math.max(1, Math.round(w * 0.6));
      soft.height = Math.max(1, Math.round(h * 0.6));
      const sctx = soft.getContext("2d");
      if (sctx) {
        sctx.imageSmoothingQuality = "high";
        sctx.drawImage(img, ((w - dw) / 2) * 0.6, (h - dh) * opts.position * 0.6, dw * 0.6, dh * 0.6);
        ctx.imageSmoothingQuality = "high";
        ctx.drawImage(soft, 0, 0, w, h);
        soft.width = soft.height = 0;
      }
      const frame = ctx.getImageData(0, 0, w, h);
      const stage = colourOf(el, "--bg-stage");
      dither(frame, opts.levels, opts.fade, opts.mute, stage, opts.mode === "ink" ? colourOf(el, "--accent-green") : null);
      ctx.putImageData(frame, 0, 0);
      if (opts.lamp && lampCv) {
        lampCv.width = w;
        lampCv.height = h;
        lampCv.style.width = cv.style.width;
        lampCv.style.height = cv.style.height;
        paintLamp(lampCv, frame, opts.lamp, (w - dw) / 2, (h - dh) * opts.position, dw, dh);
      }
      drawn = true;
    };

    const later = () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(() => void draw(), 120);
    };
    const onVisible = () => {
      if (pending && !document.hidden) void draw();
    };
    // The stage colour changes with the theme: redraw to dissolve into it.
    const themed = new MutationObserver(() => void draw(true));
    themed.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
    const ro = new ResizeObserver(later);
    ro.observe(el);
    document.addEventListener("visibilitychange", onVisible);
    void draw();
    return () => {
      alive = false;
      window.clearTimeout(timer);
      ro.disconnect();
      themed.disconnect();
      document.removeEventListener("visibilitychange", onVisible);
      cv.width = 0;
      cv.height = 0;
      if (lampCv) lampCv.width = lampCv.height = 0;
    };
  });

  // dither moves each dot's lightness to one of a few steps against a
  // Bayer threshold, by as much as the picture changes there. Then it
  // clears the dots below a ramp toward the foot, with the matrix shifted
  // so the dissolve's pattern does not line up with the grain's.
  // With ink, each dot is the ink or the stage instead: the picture's light
  // parts in ink on a dark stage, its dark parts on a light one.
  function dither(frame: ImageData, levels: number, fade: number, mute: number, bg: [number, number, number], ink: [number, number, number] | null) {
    const darkStage = 0.299 * bg[0] + 0.587 * bg[1] + 0.114 * bg[2] < 128;
    const { data, width: w, height: h } = frame;
    const L = levels - 1;
    const from = h * (1 - fade);
    const lum = new Float32Array(w * h);
    for (let i = 0, p = 0; p < w * h; i += 4, p++) {
      if (mute) {
        data[i] += (bg[0] - data[i]) * mute;
        data[i + 1] += (bg[1] - data[i + 1]) * mute;
        data[i + 2] += (bg[2] - data[i + 2]) * mute;
      }
      lum[p] = 0.299 * data[i] + 0.587 * data[i + 1] + 0.114 * data[i + 2];
    }
    for (let y = 0; y < h; y++) {
      let keep = 1;
      if (y > from) {
        const t = 1 - (y - from) / (h - from);
        keep = t * t * (3 - 2 * t);
      }
      const row = (y & 7) * 8;
      const rowB = ((y + 3) & 7) * 8;
      const up = Math.max(0, y - 1) * w;
      const down = Math.min(h - 1, y + 1) * w;
      for (let x = 0; x < w; x++) {
        const p = y * w + x;
        const i = p * 4;
        if (BAYER8[rowB + ((x + 5) & 7)] >= keep) {
          data[i + 3] = 0;
          continue;
        }
        const Y = lum[p];
        const q = (Math.floor((Y / 255) * L + BAYER8[row + (x & 7)]) / L) * 255;
        if (ink) {
          const t = darkStage ? q / 255 : 1 - q / 255;
          data[i] = bg[0] + (ink[0] - bg[0]) * t;
          data[i + 1] = bg[1] + (ink[1] - bg[1]) * t;
          data[i + 2] = bg[2] + (ink[2] - bg[2]) * t;
          data[i + 3] = 255;
          continue;
        }
        const edge = Math.abs(lum[y * w + Math.min(w - 1, x + 1)] - lum[y * w + Math.max(0, x - 1)]) + Math.abs(lum[down + x] - lum[up + x]);
        const amount = Math.min(1, 0.35 + edge / 40);
        const d = (q - Y) * amount;
        data[i] += d;
        data[i + 1] += d;
        data[i + 2] += d;
        data[i + 3] = 255;
      }
    }
  }

  // Lamplight in fixed warm tones, not theme colours: the same light in
  // either theme, on any plate.
  const LAMP_HEART: [number, number, number] = [255, 238, 184];
  const LAMP_RIM: [number, number, number] = [240, 160, 70];
  const LAMP_GLOW: [number, number, number] = [255, 190, 104];

  // paintLamp lights the window on a layer over the dithered picture, on its
  // grid: the opening filled, paler at its heart; around it the wall
  // warmed, fading with distance; under the waterline
  // a broken streak where the water mirrors it. Dots the dissolve cleared
  // stay clear.
  function paintLamp(cv: HTMLCanvasElement, base: ImageData, lamp: Lamp, ox: number, oy: number, dw: number, dh: number) {
    const ctx = cv.getContext("2d");
    if (!ctx) return;
    const { width: w, height: h, data: src } = base;
    const out = ctx.createImageData(w, h);
    const d = out.data;
    const s = Math.max(0, Math.min(1, lamp.strength));
    const x0 = ox + (lamp.x - lamp.w / 2) * dw;
    const x1 = ox + (lamp.x + lamp.w / 2) * dw;
    const y0 = oy + (lamp.y - lamp.h / 2) * dh;
    const y1 = oy + (lamp.y + lamp.h / 2) * dh;
    const cx = (x0 + x1) / 2;
    const ww = Math.max(1, x1 - x0);
    const wh = Math.max(1, y1 - y0);
    const water = oy + lamp.waterline * dh;
    const reach = Math.max(3, Math.max(ww, wh) * 1.6);
    // The reflection: the window mirrored about the waterline, drawn out.
    const r0 = 2 * water - y1;
    const rLen = wh * 2.6;
    const mix = (i: number, c: [number, number, number], k: number) => {
      d[i] = src[i] + (c[0] - src[i]) * k;
      d[i + 1] = src[i + 1] + (c[1] - src[i + 1]) * k;
      d[i + 2] = src[i + 2] + (c[2] - src[i + 2]) * k;
      d[i + 3] = 255;
    };
    const top = Math.max(0, Math.floor(y0 - reach));
    const foot = Math.min(h, Math.ceil(Math.max(y1 + reach, r0 + rLen)));
    const left = Math.max(0, Math.floor(Math.min(x0 - reach, cx - ww * 1.4)));
    const right = Math.min(w, Math.ceil(Math.max(x1 + reach, cx + ww * 1.4)));
    for (let y = top; y < foot; y++) {
      const py = y + 0.5;
      for (let x = left; x < right; x++) {
        const i = (y * w + x) * 4;
        if (src[i + 3] === 0) continue;
        const th = BAYER8[(y & 7) * 8 + (x & 7)];
        const px = x + 0.5;
        if (px >= x0 && px <= x1 && py >= y0 && py <= y1) {
          const u = Math.abs(px - cx) / (ww / 2);
          const v = Math.abs((py - y0) / wh - 0.58) / 0.58;
          const heart = 1 - Math.max(u, v);
          // Dimmer lamps (daylight) show more of the deep rim than the heart.
          mix(i, heart * (0.4 + 0.6 * s) > th * 0.7 ? LAMP_HEART : LAMP_RIM, 0.75 + 0.25 * s);
          continue;
        }
        if (py < water) {
          // On the wall, an even warming that fades out; dots scattered
          // here would read as a sparkle rather than lamplight.
          const r = Math.hypot(Math.max(x0 - px, 0, px - x1), Math.max(y0 - py, 0, py - y1)) / reach;
          if (r < 1) mix(i, LAMP_GLOW, (1 - r) * (1 - r) * 0.55 * s);
          continue;
        }
        const t = (py - r0) / rLen;
        const row = Math.floor(py - r0);
        if (t < 0 || t >= 1 || row % 3 === 2) continue;
        const wobble = Math.sin(row * 2.3) * ww * 0.35;
        const half = (ww / 2) * (1.25 - t * 0.45);
        if (Math.abs(px - cx - wobble) >= half) continue;
        const g = (1 - t) * 0.9 * s;
        if (g > th) mix(i, LAMP_GLOW, 0.72);
        else mix(i, LAMP_GLOW, g * 0.3);
      }
    }
    ctx.putImageData(out, 0, 0);
  }
</script>

<div bind:this={wrap} aria-hidden="true" class="dither-band {className}">
  {#if mode === "smooth"}
    <img {src} alt="" style:object-position="50% {position * 100}%" style:--fade-from="{(1 - fade) * 100}%" />
  {:else}
    <div class="plate">
      <canvas bind:this={canvas} class:drawn></canvas>
      {#if lamp}
        <canvas bind:this={lampCanvas} class="lamp" class:lit={lampLit && drawn}></canvas>
      {/if}
    </div>
  {/if}
</div>

<style>
  .dither-band {
    pointer-events: none;
    overflow: hidden;
  }
  canvas {
    display: block;
    image-rendering: pixelated;
    opacity: 0;
    transition: opacity 500ms ease;
  }
  canvas.drawn { opacity: 1; }
  .plate { position: relative; }
  .lamp {
    position: absolute;
    top: 0;
    left: 0;
    opacity: 0;
    transition: opacity 900ms ease;
  }
  /* Steady, not flickering: an animated layer the size of the band keeps
     the compositor busy every frame. */
  .lamp.lit { opacity: 1; }
  img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: cover;
    mask-image: linear-gradient(to bottom, #000 var(--fade-from), transparent);
  }
  @media (prefers-reduced-motion: reduce) {
    canvas { transition: none; }
  }
</style>
