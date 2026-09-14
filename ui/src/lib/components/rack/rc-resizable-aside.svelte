<script lang="ts">
  import { onMount, type Snippet } from "svelte";

  let {
    storageKey,
    defaultWidth = 260,
    min = 220,
    max = 560,
    wideWidth = 420,
    children,
  }: {
    /** localStorage key the width is remembered under */
    storageKey: string;
    defaultWidth?: number;
    min?: number;
    max?: number;
    /** width the handle snaps to on double-click / Enter */
    wideWidth?: number;
    children?: Snippet;
  } = $props();

  let width = $state(260);
  let dragging = $state(false);
  let handleY = $state<number | null>(null);

  const clamp = (w: number) => Math.round(Math.min(max, Math.max(min, w)));
  const expanded = $derived(width > defaultWidth + 8);

  onMount(() => {
    const saved = Number(localStorage.getItem(storageKey));
    width = clamp(saved || defaultWidth);
    return () => document.body.classList.remove("is-resizing");
  });

  function commit(w: number) {
    width = clamp(w);
    localStorage.setItem(storageKey, String(width));
  }

  function toggle() {
    commit(expanded ? defaultWidth : wideWidth);
  }

  function onPointerDown(e: PointerEvent) {
    if (e.button !== 0) return;
    e.preventDefault();
    const handle = e.currentTarget as HTMLElement;
    const startX = e.clientX;
    const startW = width;
    handle.setPointerCapture(e.pointerId);
    dragging = true;
    document.body.classList.add("is-resizing");

    const move = (ev: PointerEvent) => (width = clamp(startW + ev.clientX - startX));
    const up = () => {
      dragging = false;
      document.body.classList.remove("is-resizing");
      commit(width);
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", up);
      handle.removeEventListener("pointercancel", up);
    };
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", up);
    handle.addEventListener("pointercancel", up);
  }

  function trackHandle(e: PointerEvent) {
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const inset = Math.min(28, rect.height / 2);
    handleY = Math.max(inset, Math.min(rect.height - inset, e.clientY - rect.top));
  }

  function onKeydown(e: KeyboardEvent) {
    const step = e.shiftKey ? 60 : 20;
    if (e.key === "ArrowLeft") commit(width - step);
    else if (e.key === "ArrowRight") commit(width + step);
    else if (e.key === "Enter" || e.key === " ") toggle();
    else return;
    e.preventDefault();
  }
</script>

<aside class="rc-aside" class:dragging style:--aside-w="{width}px">
  <div class="body">{@render children?.()}</div>
  <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
  <div
    class="handle"
    role="separator"
    aria-orientation="vertical"
    aria-label="Resize list. Use arrow keys to resize, Enter or Space to {expanded ? 'collapse' : 'expand'}."
    aria-valuenow={width}
    aria-valuemin={min}
    aria-valuemax={max}
    aria-valuetext="{width}px"
    tabindex="0"
    title="Drag to resize · double-click to {expanded ? 'collapse' : 'expand'}"
    onpointerdown={onPointerDown}
    onpointermove={trackHandle}
    ondblclick={toggle}
    onkeydown={onKeydown}
  >
    <div class="resizer-line"></div>
    <div class="resizer-pill-handle" style:top={handleY === null ? undefined : `${handleY}px`}></div>
    {#if dragging}
      <div class="resizer-width-badge" style:top={handleY === null ? undefined : `${handleY}px`}>{width}px</div>
    {/if}
  </div>
</aside>

<style>
  .rc-aside {
    position: sticky; top: 0; align-self: start; display: flex; width: var(--aside-w);
    max-height: calc(100vh - 140px); transition: width 180ms var(--ease-snappy);
  }
  .body { flex: 1; min-width: 0; overflow-y: auto; padding-right: 2px; }
  .handle {
    position: absolute; top: 0; bottom: 0; right: -11px; width: 22px; cursor: col-resize;
    z-index: 10; touch-action: none; transition: right 180ms var(--ease-snappy);
  }
  .dragging { transition: none; user-select: none; }
  .dragging .handle { transition: none; }
  .resizer-line {
    position: absolute; top: 8px; bottom: 8px; left: 14px; width: 1px;
    background: transparent; pointer-events: none;
    transition: background 120ms ease;
  }
  .handle:hover .resizer-line { background: var(--border-default); }
  .dragging .resizer-line { background: var(--text-tertiary); }
  .handle:focus-visible { outline: none; }
  .handle:focus-visible .resizer-line { background: var(--border-focus); }

  .resizer-pill-handle {
    position: absolute; left: 7px; top: 50%; transform: translateY(-50%) scale(0.95);
    width: 4px; height: 44px; border-radius: 9999px; background: var(--text-tertiary);
    opacity: 0; pointer-events: none;
    transition: opacity 140ms ease, transform 140ms ease, background 100ms ease;
  }
  .handle:hover .resizer-pill-handle,
  .dragging .resizer-pill-handle,
  .handle:focus-visible .resizer-pill-handle {
    opacity: 1; transform: translateY(-50%) scale(1);
  }
  .handle:hover .resizer-pill-handle { background: var(--text-secondary); }
  .dragging .resizer-pill-handle { background: var(--text-primary); }

  .resizer-width-badge {
    position: absolute; left: 24px; top: 50%; transform: translateY(-50%);
    background: var(--bg-element); border: 1px solid var(--border-default); color: var(--text-primary);
    font: 10.5px var(--font-mono, ui-monospace, monospace); padding: 2px 7px; border-radius: 6px;
    pointer-events: none; white-space: nowrap; z-index: 10;
  }

  @media (max-width: 900px) {
    .rc-aside { position: static; width: auto; max-height: 260px; }
    .handle { display: none; }
  }
</style>
