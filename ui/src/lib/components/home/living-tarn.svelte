<script lang="ts" module>
  export interface Buoy {
    id: string;
    label: string;
    detail: string;
    /** lit: worked lately; resting: idle; trouble: needs a look */
    state: "lit" | "resting" | "trouble";
    /** when it last ran, ms; a newer value while on screen makes it flare */
    ranAt?: number;
  }
</script>

<script lang="ts">
  import { onMount } from "svelte";
  import DitherBand, { type Lamp } from "./dither-band.svelte";
  import Floater from "./floater.svelte";
  import { BUOY, BUOY_PIC, PIC, TARN_ART, TARN_MUTE } from "./tarn-art";
  import { getBannerPrefs } from "./banner-prefs.svelte";

  // The living tarn: the painting with a buoy floating on the lake for
  // each function, nearer ones larger. Ones that ran lately are lit and
  // cast a warm streak on the water, resting ones float unlit, and ones in
  // trouble gather off the hut, whose window lights while anything needs a
  // look. A function that runs while you watch flares: its buoy lifts
  // and brightens. A buoy opens Functions. All motion is CSS and stops
  // under reduced motion.

  let {
    buoys,
    alert = false,
    position = 0.42,
    fade = 0.5,
    onOpen,
    class: className = "",
  }: {
    buoys: Buoy[];
    alert?: boolean;
    position?: number;
    fade?: number;
    onOpen?: (buoy: Buoy) => void;
    class?: string;
  } = $props();

  const prefs = getBannerPrefs();
  const light = $derived(prefs.light);
  const render = $derived(prefs.render ?? { mode: "dither" as const, cell: 2, levels: 6 });
  // The hut's window, lit while anything needs a look. On dots the band
  // draws it into the picture; painted, it is laid over in light.
  const LAMP: Record<"day" | "night", Lamp> = {
    day: { x: PIC.lampX, y: PIC.lampY, w: PIC.lampW, h: PIC.lampH, waterline: PIC.waterline, strength: 0.4 },
    night: { x: PIC.lampX, y: PIC.lampY, w: PIC.lampW, h: PIC.lampH, waterline: PIC.waterline, strength: 1 },
  };
  let w = $state(0);
  let h = $state(0);
  let host = $state<HTMLDivElement>();
  let motionActive = $state(false);

  onMount(() => {
    if (!host) return;
    let inView = false;
    const syncMotion = () => {
      motionActive = inView && !document.hidden;
    };
    const observer = new IntersectionObserver(([entry]) => {
      inView = entry?.isIntersecting ?? false;
      syncMotion();
    });
    observer.observe(host);
    document.addEventListener("visibilitychange", syncMotion);
    return () => {
      observer.disconnect();
      document.removeEventListener("visibilitychange", syncMotion);
    };
  });

  function hash(s: string) {
    let x = 2166136261;
    for (let i = 0; i < s.length; i++) x = Math.imul(x ^ s.charCodeAt(i), 16777619);
    return ((x >>> 0) % 1000) / 1000;
  }

  // The plate's landmarks, mapped onto the band as DitherBand crops it.
  const frame = $derived.by(() => {
    const scale = Math.max(w / PIC.w, h / PIC.h);
    const dw = PIC.w * scale;
    const dh = PIC.h * scale;
    const ox = (w - dw) / 2;
    const oy = (h - dh) * position;
    return {
      lamp: { x: ox + PIC.lampX * dw, y: oy + PIC.lampY * dh },
      waterline: oy + PIC.waterline * dh,
      // The window's opening, for the painted style's light.
      win: { x: ox + (PIC.lampX - PIC.lampW / 2) * dw, y: oy + (PIC.lampY - PIC.lampH / 2) * dh, w: PIC.lampW * dw, h: PIC.lampH * dh },
    };
  });

  // Names stay on the water while there are few enough to read; past that
  // only the lit and troubled keep theirs, the rest show on hover.
  const LABEL_ALL = 8;

  const placed = $derived.by(() => {
    if (!w || !h) return [];
    const { lamp, waterline } = frame;
    // Open water: from just off the far shore to where the band starts to
    // dissolve into the page.
    const top = waterline + 10;
    const bottom = Math.max(top + 24, h * (1 - fade * 0.55));
    const sorted = [...buoys].sort((a, b) => a.label.localeCompare(b.label));
    const trouble = sorted.filter((l) => l.state === "trouble").slice(0, 5);
    const out = sorted.filter((l) => l.state !== "trouble").slice(0, 24);
    const labelAll = buoys.length <= LABEL_ALL;
    const left = Math.max(w * 0.24, lamp.x + 90);
    const span = Math.max(80, w * 0.95 - left);
    const spots = out.map((l, i) => {
      const j = hash(l.id);
      // Alternate near and far so neighbours' names don't collide.
      const d = i % 2 ? 0.12 + j * 0.3 : 0.55 + j * 0.4;
      return {
        l,
        x: left + ((i + 0.5) / Math.max(1, out.length)) * span + (j - 0.5) * Math.min(40, span / Math.max(1, out.length) / 3),
        y: top + d * (bottom - top),
        s: 14 + d * 16,
        delay: -j * 5,
        named: labelAll || l.state === "lit",
      };
    });
    trouble.forEach((l, i) => {
      const d = 0.25 + (i % 3) * 0.25;
      spots.push({ l, x: Math.max(20, lamp.x - 10 + i * 34), y: top + d * (bottom - top), s: 14 + d * 12, delay: -hash(l.id) * 5, named: true });
    });
    return spots;
  });

  // Flares: a buoy whose function runs while it is on screen.
  let flares = $state<Record<string, number>>({});
  const lastRan = new Map<string, number>();
  $effect(() => {
    const now = Date.now();
    for (const l of buoys) {
      const prev = lastRan.get(l.id);
      const ran = l.ranAt ?? 0;
      if (prev !== undefined && ran > prev) {
        flares[l.id] = now;
        setTimeout(() => {
          if (flares[l.id] === now) delete flares[l.id];
        }, 1800);
      }
      lastRan.set(l.id, ran);
    }
  });

  function short(name: string) {
    return name.length > 22 ? `${name.slice(0, 21)}…` : name;
  }

  const litCount = $derived(buoys.filter((l) => l.state === "lit").length);
  const troubleCount = $derived(buoys.filter((l) => l.state === "trouble").length);
</script>

<div
  class="living-tarn {className}"
  bind:this={host}
  bind:clientWidth={w}
  bind:clientHeight={h}
  data-light={light}
  style:--tarn-motion={motionActive ? "running" : "paused"}
>
  <DitherBand
    src={TARN_ART[light]}
    {position}
    {fade}
    mute={render.mode === "smooth" ? 0 : TARN_MUTE[light]}
    mode={render.mode}
    cell={render.cell}
    levels={render.levels}
    lamp={LAMP[light]}
    lampLit={alert}
    class="band"
  />

  {#if w > 0}
    {#if render.mode === "smooth"}
      {@const win = frame.win}
      {@const water = frame.waterline}
      <!-- Painted: warm light in the opening, thrown soft on the wall about it
           and drawn down the water below. Blended as light, so it brightens
           the painting rather than covering it. -->
      <div class="lamplight" class:lit={alert} aria-hidden="true">
        <span class="glow" style:left="{win.x + win.w / 2}px" style:top="{win.y + win.h * 0.6}px" style:--r="{Math.max(win.w, win.h) * 2.4}px"></span>
        <span class="window" style:left="{win.x + win.w * 0.1}px" style:top="{win.y + win.h * 0.08}px" style:width="{win.w * 0.8}px" style:height="{win.h * 0.84}px"></span>
        <span
          class="spill"
          style:left="{win.x - win.w * 0.25}px"
          style:top="{2 * water - win.y - win.h}px"
          style:width="{win.w * 1.5}px"
          style:height="{win.h * 2.6}px"
        ></span>
      </div>
    {/if}
    {#each placed as spot (spot.l.id)}
      {@const bw = Math.max(render.cell * 3, Math.round((spot.s * 1.25) / render.cell) * render.cell)}
      <!-- Anchored where the buoy meets the water: it floats above that
           point, its reflection and name sit below. -->
      <button
        type="button"
        class="buoy"
        class:named={spot.named}
        class:flare={!!flares[spot.l.id]}
        data-state={spot.l.state}
        style:left="{spot.x}px"
        style:top="{spot.y}px"
        style:z-index={Math.round(spot.y)}
        style:--s="{spot.s}px"
        style:--delay="{spot.delay}s"
        aria-label="{spot.l.label}: {spot.l.detail}"
        onclick={() => onOpen?.(spot.l)}
      >
        <span class="hit" style:--bw="{bw}px" style:--bh="{(bw * BUOY_PIC.h) / BUOY_PIC.w}px"></span>
        <span class="float">
          <Floater
            src={BUOY[light][spot.l.state]}
            pic={BUOY_PIC}
            glint={spot.l.state === "lit" ? "#ffcb6b" : spot.l.state === "trouble" ? "#ff6b6b" : undefined}
            delay={spot.delay}
            mode={render.mode}
            cell={render.cell}
            levels={render.levels}
            mute={render.mode === "smooth" ? 0 : TARN_MUTE[light]}
            width={bw}
          />
        </span>
        <span class="tag">
          <span class="name">{short(spot.l.label)}</span>
          <span class="detail">{spot.l.detail}</span>
        </span>
      </button>
    {/each}

    <p class="caption">
      {#if !buoys.length}
        Still water · no functions yet
      {:else if !litCount && !troubleCount}
        Still water · {buoys.length} {buoys.length === 1 ? "function" : "functions"} resting
      {:else}
        {litCount} lit{troubleCount ? ` · ${troubleCount} waiting by the hut` : ""} · {buoys.length - litCount - troubleCount} resting
      {/if}
    </p>
  {/if}
</div>

<style>
  .living-tarn {
    position: relative;
    overflow: hidden;
    /* Buoys stack by depth; keep that inside, under overlays like the palette. */
    isolation: isolate;
  }
  .living-tarn :global(.band) {
    position: absolute;
    inset: 0;
  }

  .caption {
    position: absolute;
    top: 18px;
    left: 52px;
    padding: 3px 8px;
    border-radius: 6px;
    background: color-mix(in srgb, var(--bg-stage) 72%, transparent);
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--text-secondary);
    pointer-events: none;
  }

  /* Painted lamplight, in fixed warm tones so it is the same light in
     either theme. Blended, never laid on: the opening is lit inside its
     painted frame, and the wall about it warmed rather than whitened. */
  .lamplight {
    position: absolute;
    inset: 0;
    pointer-events: none;
    opacity: 0;
    transition: opacity 900ms ease;
  }
  /* Steady: blended layers that animate are recomposited every frame. */
  .lamplight.lit { opacity: 1; }
  .lamplight span { position: absolute; mix-blend-mode: screen; }
  .lamplight .glow {
    width: calc(var(--r) * 2.4);
    height: calc(var(--r) * 2);
    translate: -50% -50%;
    background: radial-gradient(closest-side, rgb(255 170 80 / 0.55), rgb(255 150 60 / 0.18) 50%, transparent);
    mix-blend-mode: overlay;
  }
  .window {
    border-radius: 1px;
    background: radial-gradient(ellipse 75% 70% at 50% 60%, rgb(255 214 140), rgb(236 150 64) 65%, rgb(170 88 30));
    mask-image: radial-gradient(ellipse 85% 85% at 50% 55%, #000 70%, transparent);
  }
  .spill {
    background: radial-gradient(ellipse 50% 50% at 50% 25%, rgb(255 190 104 / 0.55), transparent);
    filter: blur(1.5px);
  }
  /* By day a lamp barely shows. */
  [data-light="day"] .lamplight span { opacity: 0.6; }

  /* A buoy is a zero-size anchor at the water; its parts hang off it. */
  .buoy {
    position: absolute;
    width: 0;
    height: 0;
    outline: none;
  }
  .float {
    position: absolute;
    top: 0;
    left: 0;
    transition: filter 160ms ease;
  }
  /* The buoy itself is drawn without pointer events; this is where it is. */
  .hit {
    position: absolute;
    left: calc(var(--bw) / -2);
    top: calc(var(--bh) * -0.9);
    width: var(--bw);
    height: calc(var(--bh) * 0.9);
    cursor: pointer;
  }
  .buoy:hover .float,
  .buoy:focus-visible .float { filter: brightness(1.2); }
  /* By day a lamp's streak on the water is faint. */
  [data-light="day"] .buoy :global(.glint) { filter: opacity(0.5); }

  /* The name floats on the water below; the detail joins it on hover. */
  .tag {
    position: absolute;
    top: calc(var(--s) * 0.42 + 4px);
    left: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 1px 6px;
    border-radius: 6px;
    background: color-mix(in srgb, var(--bg-stage) 78%, transparent);
    white-space: nowrap;
    opacity: 0;
    transform: translate(-50%, 2px);
    transition: opacity 160ms ease, transform 200ms var(--ease-snappy);
    pointer-events: none;
  }
  .buoy.named .tag { opacity: 0.92; transform: translate(-50%, 0); }
  .buoy:hover .tag,
  .buoy:focus-visible .tag {
    opacity: 1;
    transform: translate(-50%, 0);
    z-index: 1;
  }
  .buoy:focus-visible .tag { outline: 1px solid var(--border-focus); }
  .name {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--text-primary);
  }
  .detail {
    display: none;
    font-size: 10px;
    color: var(--text-secondary);
  }
  .buoy:hover .detail,
  .buoy:focus-visible .detail { display: block; }

  /* Ran just now: the buoy lifts on a swell and its lamp burns bright. */
  .buoy.flare .float {
    animation: tarnFlare 1.6s var(--ease-snappy);
    animation-play-state: var(--tarn-motion, running);
  }

  @keyframes tarnFlare {
    0% { translate: 0 0; filter: brightness(1); }
    25% { translate: 0 -6px; filter: brightness(1.5); }
    100% { translate: 0 0; filter: brightness(1); }
  }

  @media (prefers-reduced-motion: reduce) {
    .buoy.flare .float { animation: none; }
  }
</style>
