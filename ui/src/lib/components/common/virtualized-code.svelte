<script lang="ts">
  // Renders code/JSON text with three strategies, chosen by size:
  //
  //  - small        → syntax-highlighted HTML ({@html}) or a wrapped <pre>,
  //                    exactly like before (pretty + readable).
  //  - huge, few \n → plain <pre> with no-wrap + no break-all (skips the
  //                    expensive per-character wrap + span tree).
  //  - many lines   → a VIRTUALIZED window: only the visible lines (+overscan)
  //                    are in the DOM, so a 1000-record / 10k-line blob renders
  //                    ~50 nodes instead of tens of thousands. Fixed line height
  //                    + no wrap keeps the math exact.
  //
  // The component owns its own scroll container so virtualization can measure a
  // bounded viewport; callers should not wrap it in another scroller.

  let {
    text,
    html = null,
    contentClass = "",
    maxHeightClass = "",
  }: {
    text: string;
    html?: string | null;
    contentClass?: string;
    maxHeightClass?: string;
  } = $props();

  // Tuning knobs.
  const LINE_HEIGHT = 18; // px; forced on every virtual row so rows are uniform
  const VIRTUALIZE_LINES = 400; // switch to windowing past this many lines
  const HUGE_CHARS = 50_000; // few lines but very long → plain no-wrap <pre>
  const OVERSCAN = 12; // rows rendered above/below the viewport
  const FALLBACK_MAX_H = "max-h-[60vh]"; // used when the caller passes none

  const baseClass = "bg-[var(--code-bg)] font-mono leading-relaxed";

  const lines = $derived(text ? text.split("\n") : [""]);
  const lineCount = $derived(lines.length);
  const virtualize = $derived(lineCount > VIRTUALIZE_LINES);
  const hugePlain = $derived(!virtualize && text.length > HUGE_CHARS);
  const large = $derived(virtualize || hugePlain);
  // A bounded height is required for windowing; fall back if the caller gave none.
  const scrollerMaxH = $derived(large ? maxHeightClass || FALLBACK_MAX_H : maxHeightClass);

  // ── Virtual window state ────────────────────────────────────────────
  let scrollEl = $state<HTMLDivElement | null>(null);
  let viewportH = $state(0);
  let scrollTop = $state(0);

  const visibleRows = $derived(Math.max(1, Math.ceil((viewportH || 360) / LINE_HEIGHT)));
  const startIndex = $derived(Math.max(0, Math.floor(scrollTop / LINE_HEIGHT) - OVERSCAN));
  const endIndex = $derived(Math.min(lineCount, startIndex + visibleRows + OVERSCAN * 2));
  const offsetY = $derived(startIndex * LINE_HEIGHT);
  const totalHeight = $derived(lineCount * LINE_HEIGHT);
  const windowLines = $derived(lines.slice(startIndex, endIndex));

  let ticking = false;
  function onScroll() {
    if (ticking || !scrollEl) return;
    ticking = true;
    requestAnimationFrame(() => {
      if (scrollEl) scrollTop = scrollEl.scrollTop;
      ticking = false;
    });
  }
</script>

{#if virtualize}
  <div class="relative">
    <div
      bind:this={scrollEl}
      bind:clientHeight={viewportH}
      onscroll={onScroll}
      class={`overflow-auto ${baseClass} ${contentClass} ${scrollerMaxH}`}
    >
      <div class="relative" style={`height:${totalHeight}px`}>
        <div
          class="absolute left-0 top-0 will-change-transform"
          style={`transform:translateY(${offsetY}px)`}
        >
          {#each windowLines as line, i (startIndex + i)}
            <div
              class="whitespace-pre px-3"
              style={`height:${LINE_HEIGHT}px;line-height:${LINE_HEIGHT}px`}
            >{line}</div>
          {/each}
        </div>
      </div>
    </div>
    <div
      class="pointer-events-none absolute bottom-1.5 right-2 rounded border border-border/60 bg-background/80 px-1.5 py-0.5 text-[10px] font-mono text-muted-foreground/70 backdrop-blur-sm"
    >
      {lineCount.toLocaleString()} lines · plain
    </div>
  </div>
{:else if hugePlain}
  <pre
    class={`overflow-auto ${baseClass} px-3 py-3 whitespace-pre ${contentClass} ${scrollerMaxH}`}>{text}</pre>
{:else if html}
  <div
    class={`overflow-auto ${baseClass} px-3 py-3 whitespace-pre-wrap break-all ${contentClass} ${maxHeightClass}`}
  >{@html html}</div>
{:else}
  <pre
    class={`overflow-auto ${baseClass} px-3 py-3 whitespace-pre-wrap break-all ${contentClass} ${maxHeightClass}`}>{text}</pre>
{/if}
