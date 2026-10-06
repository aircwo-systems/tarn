<script lang="ts" module>
  import type { LogEvent } from "$lib/types";

  export const ROW_H = 24;

  /** Stable identity for an event. Occurrence suffix disambiguates identical lines. */
  export function keyEvents(events: LogEvent[]): string[] {
    const seen = new Map<string, number>();
    const keys = new Array<string>(events.length);
    for (let i = 0; i < events.length; i++) {
      const ev = events[i];
      const base = `${ev.timestamp}|${ev.streamName}|${ev.source ?? ""}|${ev.message}`;
      const n = seen.get(base) ?? 0;
      seen.set(base, n + 1);
      keys[i] = n === 0 ? base : `${base}#${n}`;
    }
    return keys;
  }
</script>

{#snippet highlighted(value: string)}
  {#if highlightPattern && highlightPattern.trim()}
    {#each splitMatches(clip(value), highlightPattern) as seg}
      {#if seg.match}<mark class="log-search-match">{seg.text}</mark>{:else}{seg.text}{/if}
    {/each}
  {:else}
    {clip(value)}
  {/if}
{/snippet}

<script lang="ts">
  import { ArrowUpIcon, ArrowDownIcon, PushPinIcon } from "phosphor-svelte";
  import { buildFlexiblePatternRegex } from "$lib/json-format";
  import { formatDatadogLog, shortTraceId } from "$lib/log-format";
  import { onDestroy } from "svelte";

  const OVERSCAN = 12;
  const FRESH_MS = 1400;

  let {
    events,
    keys,
    selectedKey = null,
    highlightKey = null,
    order = "desc",
    showGroup = false,
    showStream = true,
    highlightPattern = "",
    formatDatadog = false,
    pinnedKeys = new Set<string>(),
    onTogglePin,
    onSelect,
  }: {
    events: LogEvent[];
    keys: string[];
    selectedKey?: string | null;
    highlightKey?: string | null;
    order?: "asc" | "desc";
    showGroup?: boolean;
    showStream?: boolean;
    highlightPattern?: string;
    /** Show Datadog JSON records as their message plus application fields. */
    formatDatadog?: boolean;
    pinnedKeys?: Set<string>;
    onTogglePin?: (event: LogEvent, key: string) => void;
    onSelect: (event: LogEvent, key: string) => void;
  } = $props();

  function clip(value: string): string {
    return value.length > 400 ? value.slice(0, 400) : value;
  }

  function splitMatches(text: string, pattern?: string): { text: string; match: boolean }[] {
    if (!pattern || !pattern.trim()) return [{ text, match: false }];
    const rx = buildFlexiblePatternRegex(pattern);
    if (!rx) return [{ text, match: false }];

    const segments: { text: string; match: boolean }[] = [];
    let lastIdx = 0;
    let m: RegExpExecArray | null;
    while ((m = rx.exec(text)) !== null) {
      if (m.index > lastIdx) {
        segments.push({ text: text.slice(lastIdx, m.index), match: false });
      }
      segments.push({ text: m[0], match: true });
      lastIdx = m.index + m[0].length;
      if (m[0].length === 0) {
        rx.lastIndex++;
      }
    }
    if (lastIdx < text.length) {
      segments.push({ text: text.slice(lastIdx), match: false });
    }
    return segments;
  }

  let scroller = $state<HTMLDivElement | null>(null);
  let viewportH = $state(0);
  let scrollTop = $state(0);

  // ─── Live-edge tracking ───
  // desc: newest at top → edge is scrollTop 0. asc: newest at bottom.
  let atEdge = $state(true);
  let unseen = $state(0);

  const total = $derived(events.length);
  const columns = $derived.by(() => {
    let groupLabel = "";
    let hasTraceColumn = false;
    if (!showGroup && !formatDatadog) return { groupLabel, hasTraceColumn };
    for (const ev of events) {
      const dd = formatDatadog ? formatDatadogLog(ev.message) : null;
      if (dd) hasTraceColumn = true;
      if (showGroup) {
        const label = dd?.service ?? groupTag(ev.streamName);
        if (label.length > groupLabel.length) groupLabel = label;
      }
    }
    return { groupLabel, hasTraceColumn };
  });
  const start = $derived(Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN));
  const end = $derived(Math.min(total, Math.ceil((scrollTop + viewportH) / ROW_H) + OVERSCAN));
  const indexByKey = $derived.by(() => {
    const m = new Map<string, number>();
    for (let i = 0; i < keys.length; i++) m.set(keys[i], i);
    return m;
  });
  const visible = $derived(Array.from({ length: Math.max(0, end - start) }, (_, o) => start + o));
  const selectedIdx = $derived(selectedKey ? (indexByKey.get(selectedKey) ?? -1) : -1);
  const selectedLevel = $derived(selectedIdx >= 0 ? events[selectedIdx]?.level : "");

  function measureEdge() {
    if (!scroller) return;
    atEdge =
      order === "desc"
        ? scroller.scrollTop <= ROW_H
        : scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight <= ROW_H;
    if (atEdge) unseen = 0;
  }

  let scrollRaf = 0;
  function onScroll() {
    if (scrollRaf) return;
    scrollRaf = requestAnimationFrame(() => {
      scrollRaf = 0;
      if (!scroller) return;
      scrollTop = scroller.scrollTop;
      measureEdge();
      if (pointerInside) resolveHover(lastClientY, 0);
    });
  }

  export function jumpToEdge() {
    if (!scroller) return;
    scroller.scrollTo({
      top: order === "desc" ? 0 : scroller.scrollHeight,
      behavior: "smooth",
    });
    unseen = 0;
  }

  export function scrollToIndex(idx: number) {
    if (!scroller) return;
    const top = Math.max(0, idx * ROW_H - viewportH / 2 + ROW_H / 2);
    scroller.scrollTo({ top, behavior: "smooth" });
  }

  // ─── Merge handling: hold the reader's position, flag fresh rows ───
  const arrivals = new Map<string, number>();
  let prevKeys = new Set<string>();
  let prevOrderedKeys: string[] = [];
  let primed = false;
  let instant = $state(false);
  let clock = $state(Date.now());
  let clockHandle: ReturnType<typeof setTimeout> | null = null;

  $effect.pre(() => {
    const nextKeys = keys;
    const el = scroller;
    if (!el) return;

    // Anchor on the first visible row before the DOM updates.
    const anchorIdx = Math.floor(el.scrollTop / ROW_H);
    const anchorKey = prevOrderedKeys[anchorIdx];
    const anchorOffset = el.scrollTop - anchorIdx * ROW_H;
    const wasAtEdge = atEdge;

    const now = Date.now();
    let added = 0;
    const nextSet = new Set(nextKeys);
    if (primed) {
      for (const k of nextKeys) {
        if (!prevKeys.has(k)) {
          arrivals.set(k, now);
          added++;
        }
      }
    }
    for (const k of arrivals.keys()) {
      if (!nextSet.has(k) || now - (arrivals.get(k) ?? 0) > FRESH_MS) arrivals.delete(k);
    }
    prevKeys = nextSet;
    prevOrderedKeys = nextKeys;
    primed = nextKeys.length > 0;

    if (added === 0) return;
    instant = true;
    clock = now;
    if (clockHandle) clearTimeout(clockHandle);
    clockHandle = setTimeout(() => (clock = Date.now()), FRESH_MS);

    queueMicrotask(() => {
      requestAnimationFrame(() => {
        if (!scroller) return;
        if (wasAtEdge) {
          scroller.scrollTop = order === "desc" ? 0 : scroller.scrollHeight;
        } else {
          unseen += added;
          const newIdx = anchorKey ? indexByKey.get(anchorKey) : undefined;
          if (newIdx !== undefined) scroller.scrollTop = newIdx * ROW_H + anchorOffset;
        }
        scrollTop = scroller.scrollTop;
        requestAnimationFrame(() => (instant = false));
      });
    });
  });
  // Reset edge state when the ordering flips.
  $effect(() => {
    void order;
    unseen = 0;
    atEdge = true;
  });

  // ─── Predictive hover (velocity lead, O(1) row lookup) ───
  let hoverIdx = $state(-1);
  let pointerInside = false;
  let lastClientY = 0;
  let lastSampleY = 0;
  let lastSampleT = 0;
  let vy = 0;

  function resolveHover(clientY: number, lead: number) {
    if (!scroller) return;
    const rel = clientY - scroller.getBoundingClientRect().top + scroller.scrollTop;
    const actual = Math.floor(rel / ROW_H);
    const predicted = Math.floor((rel + lead) / ROW_H);
    // The lead may anticipate at most one row so the pill never strays from the cursor.
    const idx = Math.max(actual - 1, Math.min(actual + 1, predicted));
    hoverIdx = idx >= 0 && idx < total ? idx : -1;
  }

  function onPointerMove(e: PointerEvent) {
    pointerInside = true;
    const t = performance.now();
    const dt = t - lastSampleT;
    if (dt > 8) {
      vy = (e.clientY - lastSampleY) / dt;
      lastSampleY = e.clientY;
      lastSampleT = t;
    }
    lastClientY = e.clientY;
    const lead = Math.abs(vy) > 0.18 ? Math.max(-26, Math.min(26, vy * 20)) : 0;
    resolveHover(e.clientY, lead);
  }

  function onPointerLeave() {
    pointerInside = false;
    hoverIdx = -1;
  }

  // ─── Keyboard ───
  function onKeydown(e: KeyboardEvent) {
    if (total === 0) return;
    let next = -1;
    if (e.key === "ArrowDown" || e.key === "j") next = Math.min(total - 1, selectedIdx + 1);
    else if (e.key === "ArrowUp" || e.key === "k") next = Math.max(0, selectedIdx < 0 ? 0 : selectedIdx - 1);
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = total - 1;
    if (next < 0) return;
    e.preventDefault();
    onSelect(events[next], keys[next]);
    if (!scroller) return;
    const top = next * ROW_H;
    if (top < scroller.scrollTop) scroller.scrollTop = top;
    else if (top + ROW_H > scroller.scrollTop + viewportH) scroller.scrollTop = top + ROW_H - viewportH;
  }

  onDestroy(() => {
    if (scrollRaf) cancelAnimationFrame(scrollRaf);
    if (clockHandle) clearTimeout(clockHandle);
  });

  function levelTag(level: string): string {
    const norm = (level || "").toUpperCase();
    switch (norm) {
      case "ERROR": return "ERROR";
      case "WARN": return "WARN";
      case "DEBUG": return "DEBUG";
      case "INFO": return "INFO";
      default: return norm || "···";
    }
  }

  function compactTime(ts: string): string {
    const d = new Date(ts);
    if (Number.isNaN(d.getTime())) return ts;
    const p = (n: number, w = 2) => String(n).padStart(w, "0");
    return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`;
  }

  function statusClass(status: number): string {
    return status >= 500 ? "5xx" : status >= 400 ? "4xx" : "ok";
  }

  function groupTag(stream: string): string {
    return stream.split("/").slice(1, -1).join("/");
  }
</script>

<div class="log-stream">
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div
    class="log-scroller"
    bind:this={scroller}
    bind:clientHeight={viewportH}
    onscroll={onScroll}
    onpointermove={onPointerMove}
    onpointerleave={onPointerLeave}
    onkeydown={onKeydown}
    tabindex="0"
    role="listbox"
    aria-label="Log events"
    aria-activedescendant={selectedIdx >= 0 ? `log-row-${selectedIdx}` : undefined}
  >
    <div class="log-canvas" style:height="{total * ROW_H}px">
      <div
        class="log-hover-pill"
        class:instant
        style:transform="translate3d(0, {Math.max(hoverIdx, 0) * ROW_H}px, 0)"
        style:opacity={hoverIdx >= 0 && hoverIdx !== selectedIdx ? 1 : 0}
        aria-hidden="true"
      ></div>
      <div
        class="log-active-pill"
        class:instant
        data-level={selectedLevel}
        style:transform="translate3d(0, {Math.max(selectedIdx, 0) * ROW_H}px, 0)"
        style:opacity={selectedIdx >= 0 ? 1 : 0}
        aria-hidden="true"
      ></div>

      {#each visible as i (keys[i])}
        {@const ev = events[i]}
        {@const key = keys[i]}
        {@const arrived = arrivals.get(key)}
        {@const age = arrived ? clock - arrived : FRESH_MS}
        {@const dd = formatDatadog ? formatDatadogLog(ev.message) : null}
        {@const text = dd ? dd.message : ev.message}
        {@const traceId = dd ? (dd.traceId ?? ev.traceId ?? "") : ""}
        {@const traceTag = traceId ? shortTraceId(traceId) : ""}
        <div
          id="log-row-{i}"
          class="log-row"
          class:selected={i === selectedIdx}
          class:pinned={pinnedKeys.has(key)}
          class:highlight={key === highlightKey}
          class:fresh={age < FRESH_MS}
          data-level={ev.level}
          style:transform="translateY({i * ROW_H}px)"
          style:animation-delay={age < FRESH_MS ? `-${age}ms` : undefined}
          role="option"
          tabindex="-1"
          aria-selected={i === selectedIdx}
          onclick={() => {
            const sel = window.getSelection();
            if (sel && sel.toString().trim().length > 0) return;
            onSelect(ev, key);
          }}
          onkeydown={() => {}}
        >
          <span class="log-tick" aria-hidden="true"></span>
          {#if pinnedKeys.has(key)}<span class="log-pin-tick" aria-hidden="true"></span>{/if}
          <span class="log-time">{compactTime(ev.timestamp)}</span>
          <span class="log-level">{levelTag(ev.level)}</span>
          {#if showGroup}
            <span class="log-group-column">
              <span class="log-group column-size" aria-hidden="true">{columns.groupLabel}</span>
              <span class="log-group" class:service={!!dd?.service} title={dd?.service ?? ev.streamName}>{dd?.service ?? groupTag(ev.streamName)}</span>
            </span>
          {/if}
          {#if columns.hasTraceColumn}
            <!-- Empty cells keep messages aligned on rows without a trace. -->
            <span class="log-group trace" class:empty={!traceTag} aria-hidden={!traceTag} title={traceTag ? `Trace ${traceId}` : undefined}>{traceTag || "00000000"}</span>
          {/if}
          <span class="log-msg" title={text.length > 120 ? undefined : text}>
            {@render highlighted(text)}
            {#if dd?.extras}<span class="log-extras">{@render highlighted(dd.extras)}</span>{/if}
          </span>
          {#if dd?.status}
            <span class="log-status" data-class={statusClass(dd.status)}>{dd.status}</span>
          {/if}
          {#if dd?.durationMs !== undefined}
            <span
              class="log-duration"
              role="img"
              aria-label={`Request duration ${dd.durationMs} milliseconds`}
              title={`${dd.durationMs} ms`}
              style:--duration-fill={`${Math.min(100, Math.max(0, dd.durationMs / 10))}%`}
            ><span class="log-duration-fill" class:nonzero={dd.durationMs > 0}></span></span>
          {:else if dd}
            <span class="log-duration-slot" aria-hidden="true"></span>
          {/if}
          {#if onTogglePin}
            <button
              type="button"
              class="log-pin"
              class:pinned={pinnedKeys.has(key)}
              aria-label={pinnedKeys.has(key) ? "Unpin log event" : "Pin log event"}
              aria-pressed={pinnedKeys.has(key)}
              title={pinnedKeys.has(key) ? "Unpin log" : "Pin log"}
              onclick={(e) => { e.stopPropagation(); onTogglePin?.(ev, key); }}
              onkeydown={(e) => e.stopPropagation()}
            ><PushPinIcon size={13} weight={pinnedKeys.has(key) ? "fill" : "regular"} /></button>
          {/if}
          {#if showStream && ev.streamName && !showGroup}
            <span class="log-stream-name" title={ev.streamName}>{ev.streamName.slice(-12)}</span>
          {/if}
        </div>
      {/each}
    </div>
  </div>

  {#if !atEdge && unseen > 0}
    <button
      type="button"
      class="log-unseen"
      class:bottom={order === "asc"}
      onclick={jumpToEdge}
    >
      {#if order === "desc"}<ArrowUpIcon size={11} weight="bold" />{:else}<ArrowDownIcon size={11} weight="bold" />{/if}
      {unseen} new
    </button>
  {/if}
</div>

<style>
  .log-stream {
    position: relative;
    container-type: inline-size;
    height: 100%;
    min-height: 0;
  }

  .log-scroller {
    height: 100%;
    overflow-y: auto;
    overflow-x: hidden;
    outline: none;
    overscroll-behavior: contain;
    scrollbar-width: thin;
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 12px;
    contain: strict;
  }

  .log-scroller:focus-visible {
    outline: 2px solid var(--border-focus);
    outline-offset: -2px;
  }

  .log-canvas {
    position: relative;
    width: 100%;
  }

  /* ─── Motion pills ─── */
  .log-hover-pill,
  .log-active-pill {
    position: absolute;
    top: 0;
    left: 2px;
    right: 2px;
    height: 24px;
    border-radius: 4px;
    pointer-events: none;
    will-change: transform, opacity;
  }

  .log-hover-pill {
    background: var(--bg-element-hover);
    border: 1px solid var(--border-subtle);
    transition:
      transform 130ms var(--ease-snappy),
      opacity 80ms ease;
    z-index: 1;
  }

  .log-active-pill {
    background: var(--bg-element);
    border: 1px solid var(--border-default);
    transition:
      transform 200ms var(--ease-snappy),
      opacity 120ms ease;
    z-index: 2;
  }

  .log-active-pill::before {
    content: "";
    position: absolute;
    left: 5px;
    top: 6px;
    bottom: 6px;
    width: 2.5px;
    border-radius: 2px;
    background: var(--text-primary);
    opacity: 0.9;
  }

  .log-active-pill[data-level="ERROR"]::before { background: var(--accent-red); }
  .log-active-pill[data-level="WARN"]::before { background: var(--accent-amber); }

  .log-hover-pill.instant,
  .log-active-pill.instant {
    transition: none;
  }

  /* ─── Rows ─── */
  .log-row {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    height: 24px;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 6px 0 16px;
    cursor: pointer;
    color: var(--text-secondary);
    white-space: nowrap;
    z-index: 3;
    contain: layout style;
  }

  .log-tick {
    position: absolute;
    left: 3px;
    top: 9px;
    width: 2px;
    height: 6px;
    border-radius: 2px;
    background: transparent;
  }

  .log-row[data-level="ERROR"] .log-tick { background: var(--accent-red); }
  .log-row[data-level="WARN"] .log-tick { background: var(--accent-amber); }
  .log-row.selected .log-tick { opacity: 0; }

  .log-pin-tick {
    position: absolute;
    left: 3px;
    top: 9px;
    width: 2px;
    height: 6px;
    border-radius: 2px;
    background: var(--accent-amber);
  }

  .log-row.pinned:is([data-level="ERROR"], [data-level="WARN"]) .log-pin-tick { left: 8px; }
  .log-row.selected.pinned .log-pin-tick { left: 8px; }

  .log-time {
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
    font-size: 11px;
    flex-shrink: 0;
    user-select: none;
    transition: color 100ms ease;
  }

  .log-level {
    width: 40px;
    flex-shrink: 0;
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.04em;
    color: var(--text-tertiary);
    user-select: none;
  }

  .log-row[data-level="INFO"] .log-level { color: var(--accent-green); opacity: 0.75; }
  .log-row[data-level="WARN"] .log-level { color: var(--accent-amber); }
  .log-row[data-level="ERROR"] .log-level { color: var(--accent-red); }

  /* Share the widest label's natural size and align badge ends. */
  .log-group-column {
    display: grid;
    flex-shrink: 0;
  }
  .log-group-column > .log-group {
    grid-area: 1 / 1;
    justify-self: end;
  }
  .log-group.column-size { visibility: hidden; pointer-events: none; }

  /* Size badges from their text, including the current font and letter spacing. */
  .log-group {
    display: inline-flex;
    align-items: center;
    flex-shrink: 0;
    box-sizing: border-box;
    width: max-content;
    white-space: nowrap;
    font-size: 10.5px;
    color: var(--text-tertiary);
    padding: 0 5px;
    border: 1px solid var(--border-subtle);
    border-radius: 4px;
    line-height: 15px;
  }

  .log-msg {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--text-primary);
    opacity: 0.82;
    transition: opacity 100ms ease;
  }

  .log-row[data-level="ERROR"] .log-msg { color: var(--accent-red); opacity: 0.9; }
  .log-extras { margin-left: 12px; color: var(--text-tertiary); }

  /* Datadog rows name their emitting service. */
  .log-group.service { color: var(--text-secondary); border-color: var(--border-default); }
  /* Trace IDs tint toward the accent: the row links to its request in Traces. */
  .log-group.trace {
    font-variant-numeric: tabular-nums;
    color: color-mix(in srgb, var(--accent-green) 45%, var(--text-tertiary));
    border-color: color-mix(in srgb, var(--accent-green) 16%, var(--border-subtle));
  }
  .log-group.trace.empty { visibility: hidden; }
  .log-status {
    flex-shrink: 0;
    height: 16px;
    padding: 0 5px;
    border: 1px solid var(--border-default);
    border-radius: 4px;
    font-size: 10px;
    line-height: 14px;
    font-variant-numeric: tabular-nums;
    color: var(--text-secondary);
  }
  .log-status[data-class="4xx"] {
    color: var(--accent-amber);
    border-color: color-mix(in srgb, var(--accent-amber) 45%, transparent);
  }
  .log-status[data-class="5xx"] {
    color: var(--accent-red);
    border-color: color-mix(in srgb, var(--accent-red) 45%, transparent);
  }
  .log-duration,
  .log-duration-slot {
    flex-shrink: 0;
    width: 24px;
    height: 16px;
  }
  .log-duration {
    display: block;
    border-radius: 2px;
    overflow: hidden;
    background: color-mix(in srgb, var(--text-tertiary) 12%, transparent);
  }
  .log-duration-fill {
    display: block;
    width: var(--duration-fill, 0%);
    height: 100%;
    background: linear-gradient(90deg,
      color-mix(in srgb, var(--text-secondary) 25%, transparent),
      color-mix(in srgb, var(--text-secondary) 85%, transparent));
    opacity: 0.8;
  }
  .log-duration-fill.nonzero { min-width: 2px; }
  .log-row:is(:hover, .selected) .log-duration-fill { opacity: 1; }
  .log-row[data-level="DEBUG"] .log-msg { opacity: 0.55; }

  .log-row:hover .log-msg,
  .log-row.selected .log-msg { opacity: 1; }
  .log-row:hover .log-time,
  .log-row.selected .log-time { color: var(--text-secondary); }

  .log-stream-name {
    flex-shrink: 0;
    font-size: 10.5px;
    color: var(--text-tertiary);
    opacity: 0.7;
  }

  .log-pin {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    flex-shrink: 0;
    border: 0;
    background: transparent;
    color: var(--text-tertiary);
    opacity: 0;
    cursor: pointer;
  }

  .log-row:hover .log-pin,
  .log-pin:focus-visible { opacity: 1; }
  .log-pin:hover,
  .log-pin.pinned { color: var(--accent-amber); }
  .log-pin:focus-visible { outline: 2px solid var(--border-focus); outline-offset: -2px; }

  @container (max-width: 640px) {
    .log-stream-name { display: none; }
  }

  :global(.log-search-match) {
    background: rgba(245, 158, 11, 0.35);
    color: inherit;
    border-radius: 2px;
    padding: 0 1px;
    font-weight: 600;
  }

  .log-row.highlight {
    box-shadow: inset 2px 0 0 var(--accent-amber);
    background: color-mix(in srgb, var(--accent-amber) 8%, transparent);
  }

  /* New arrivals: a soft wash that settles, replayed consistently when virtualized rows remount. */
  .log-row.fresh {
    animation: log-arrive 1400ms var(--ease-snappy) both;
  }

  .log-row.fresh .log-msg {
    animation: log-arrive-text 700ms var(--ease-snappy) both;
    animation-delay: inherit;
  }

  @keyframes log-arrive {
    0% { background: color-mix(in srgb, var(--accent-green) 14%, transparent); }
    100% { background: transparent; }
  }

  @keyframes log-arrive-text {
    0% { transform: translateX(-4px); opacity: 0; }
    100% { transform: translateX(0); }
  }

  @media (prefers-reduced-motion: reduce) {
    .log-row.fresh,
    .log-row.fresh .log-msg { animation: none; }
    .log-hover-pill,
    .log-active-pill { transition: none; }
  }

  /* ─── Unseen arrivals pill ─── */
  .log-unseen {
    position: absolute;
    left: 50%;
    top: 10px;
    transform: translateX(-50%);
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 3px 10px;
    border-radius: 9999px;
    border: 1px solid var(--border-default);
    background: var(--bg-stage);
    color: var(--text-primary);
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 11px;
    cursor: pointer;
    z-index: 10;
    animation: unseen-in 220ms var(--ease-snappy) both;
    transition: border-color 100ms ease, background 100ms ease;
  }

  .log-unseen.bottom {
    top: auto;
    bottom: 10px;
  }

  .log-unseen:hover {
    border-color: var(--border-focus);
    background: var(--bg-element);
  }

  @keyframes unseen-in {
    from { opacity: 0; transform: translate(-50%, -4px); }
    to { opacity: 1; transform: translate(-50%, 0); }
  }
</style>
