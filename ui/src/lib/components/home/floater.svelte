<script lang="ts">
  import { BAYER8, colourOf } from "./dither";

  // Something afloat on the tarn (a function's buoy), drawn the way the band is drawn: through the same dots on a dithered
  // band, as painted on a painted one. It is a zero-size anchor where it
  // meets the water; it sits down to its waterline, its reflection hangs
  // below, broken by the water, and a light it carries can lay a streak on
  // the lake. Where it floats is the tarn's business; this only draws it.

  let {
    src,
    pic,
    glint,
    delay = 0,
    mode,
    cell,
    levels,
    mute = 0,
    width,
  }: {
    src: string;
    /** the sprite's size and where its waterline is, as a fraction of its height */
    pic: { w: number; h: number; waterline: number };
    /** a colour to streak the water with, from under its light */
    glint?: string;
    /** seconds into its bob, so neighbours don't bob as one */
    delay?: number;
    mode: "dither" | "ink" | "smooth";
    cell: number;
    levels: number;
    mute?: number;
    /** CSS px, a whole number of dots */
    width: number;
  } = $props();

  const height = $derived(Math.max(cell, Math.round((width * pic.h) / pic.w / cell) * cell));

  let host = $state<HTMLDivElement>();
  let sprite = $state<HTMLCanvasElement>();
  let mirror = $state<HTMLCanvasElement>();

  $effect(() => {
    const el = host;
    const cv = sprite;
    const mv = mirror;
    const opts = { src, mode, cell, levels, mute, width, height };
    if (!el || !cv || !mv || opts.mode === "smooth") return;
    let alive = true;

    const draw = async () => {
      const w = Math.max(1, Math.round(opts.width / opts.cell));
      const h = Math.max(1, Math.round(opts.height / opts.cell));
      const img = new Image();
      img.src = opts.src;
      try {
        await img.decode();
      } catch {
        return;
      }
      if (!alive) return;
      for (const c of [cv, mv]) {
        c.width = w;
        c.height = h;
      }
      const ctx = cv.getContext("2d", { willReadFrequently: true });
      if (!ctx) return;
      ctx.imageSmoothingQuality = "high";
      ctx.drawImage(img, 0, 0, w, h);
      const frame = ctx.getImageData(0, 0, w, h);
      const stage = colourOf(el, "--bg-stage");
      dither(frame, opts.levels, opts.mute, stage, opts.mode === "ink" ? colourOf(el, "--accent-green") : null);
      ctx.putImageData(frame, 0, 0);
      mv.getContext("2d")?.putImageData(frame, 0, 0);
    };

    // The stage and accent change with the theme.
    const themed = new MutationObserver(() => void draw());
    themed.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
    void draw();
    return () => {
      alive = false;
      themed.disconnect();
    };
  });

  // The band's dither, for a sprite: edges cut hard (dots are in or out),
  // lightness moved to a few steps against the Bayer threshold. With ink,
  // each dot is mixed between the stage and the ink.
  function dither(frame: ImageData, levels: number, mute: number, bg: [number, number, number], ink: [number, number, number] | null) {
    const darkStage = 0.299 * bg[0] + 0.587 * bg[1] + 0.114 * bg[2] < 128;
    const { data, width: w, height: h } = frame;
    const L = levels - 1;
    for (let y = 0; y < h; y++) {
      for (let x = 0; x < w; x++) {
        const i = (y * w + x) * 4;
        if (data[i + 3] < 110) {
          data[i + 3] = 0;
          continue;
        }
        data[i + 3] = 255;
        if (mute) {
          data[i] += (bg[0] - data[i]) * mute;
          data[i + 1] += (bg[1] - data[i + 1]) * mute;
          data[i + 2] += (bg[2] - data[i + 2]) * mute;
        }
        const Y = 0.299 * data[i] + 0.587 * data[i + 1] + 0.114 * data[i + 2];
        const q = (Math.floor((Y / 255) * L + BAYER8[(y & 7) * 8 + (x & 7)]) / L) * 255;
        if (ink) {
          // Never all stage: a floor of ink keeps the boat's shape readable
          // where its tones fall on the stage's side.
          const t = 0.4 + 0.6 * (darkStage ? q / 255 : 1 - q / 255);
          data[i] = bg[0] + (ink[0] - bg[0]) * t;
          data[i + 1] = bg[1] + (ink[1] - bg[1]) * t;
          data[i + 2] = bg[2] + (ink[2] - bg[2]) * t;
          continue;
        }
        const d = (q - Y) * 0.55;
        data[i] += d;
        data[i + 1] += d;
        data[i + 2] += d;
      }
    }
  }
</script>

<div
  bind:this={host}
  class="floater"
  data-mode={mode}
  aria-hidden="true"
  style:--w="{width}px"
  style:--h="{height}px"
  style:--wl={pic.waterline}
  style:--delay="{delay}s"
  style:--row="{mode === 'smooth' ? 2 : cell}px"
>
  {#if glint}
    <span class="glint" style:--glint={glint}></span>
  {/if}
  <!-- The reflection holds still: it is masked, and a moving thing under a
       mask is redrawn through it every frame. -->
  <div class="reflection">
    <div class="mirror">
      {#if mode === "smooth"}
        <img {src} alt="" />
      {:else}
        <canvas bind:this={mirror}></canvas>
      {/if}
    </div>
  </div>
  <div class="hull">
    <div class="bob">
      {#if mode === "smooth"}
        <img {src} alt="" />
      {:else}
        <canvas bind:this={sprite}></canvas>
      {/if}
    </div>
  </div>
</div>

<style>
  .floater {
    position: absolute;
    width: 0;
    height: 0;
    pointer-events: none;
  }
  canvas,
  img {
    display: block;
    width: var(--w);
    height: var(--h);
  }
  canvas { image-rendering: pixelated; }

  /* Above the water: the boat down to its waterline; the hull below it is
     under the lake. */
  .hull {
    position: absolute;
    left: calc(var(--w) / -2);
    bottom: 0;
    width: var(--w);
    height: calc(var(--h) * var(--wl));
    overflow: hidden;
  }

  /* Below: the same boat upside down, short, faint and broken into rows. */
  .reflection {
    position: absolute;
    left: calc(var(--w) / -2);
    top: 0;
    width: var(--w);
    height: calc(var(--h) * var(--wl) * 0.75);
    overflow: hidden;
    opacity: 0.32;
    mask-image:
      repeating-linear-gradient(to bottom, #000 0 var(--row), rgb(0 0 0 / 0.3) var(--row) calc(var(--row) * 2)),
      linear-gradient(to bottom, #000, transparent);
    mask-composite: intersect;
  }
  .mirror {
    position: absolute;
    left: 0;
    top: calc(var(--h) * (var(--wl) - 1));
    transform: scaleY(-1);
  }

  .bob {
    animation: floatBob 4.6s ease-in-out infinite;
    animation-delay: var(--delay);
    animation-play-state: var(--tarn-motion, running);
  }
  /* On dots it rides a dot up and down, never between them. */
  .floater:not([data-mode="smooth"]) .bob {
    animation-name: floatBobDots;
    animation-timing-function: step-end;
  }

  /* At night the lantern lays a warm, broken streak on the water. */
  .glint {
    position: absolute;
    left: 0;
    top: 1px;
    width: calc(var(--w) * 0.1);
    height: calc(var(--h) * 0.5);
    translate: -50% 0;
    background: repeating-linear-gradient(to bottom, var(--glint) 0 var(--row), transparent var(--row) calc(var(--row) * 2));
    mask-image: linear-gradient(to bottom, #000, transparent);
    opacity: 0.55;
  }
  .floater[data-mode="smooth"] .glint { filter: blur(1px); }

  @keyframes floatBob {
    0%, 100% { translate: 0 0; rotate: -0.8deg; }
    50% { translate: 0 1.5px; rotate: 0.8deg; }
  }
  @keyframes floatBobDots {
    0%, 100% { translate: 0 0; }
    50% { translate: 0 var(--row); }
  }

  @media (prefers-reduced-motion: reduce) {
    .bob { animation: none; }
  }
</style>
