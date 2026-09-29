<script lang="ts">
  import { ArrowUpRightIcon, CaretRightIcon, RowsIcon, WarningIcon } from "phosphor-svelte";
  import { slide } from "svelte/transition";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import EmptyState from "$lib/components/common/empty-state.svelte";
  import { fetchLogSummary, type LogSummary, type LogSummaryGroup, type LogSummarySample } from "$lib/api";
  import { highlightJSON, highlightSearchText } from "$lib/json-format";
  import type { LogEvent } from "$lib/types";

  let {
    groups = [],
    level = "",
    pattern = "",
    stream = "",
    events = [],
    reloadToken = 0,
    onDrill,
    onLoadingChange,
  }: {
    /** Log groups to summarise; empty means every group. */
    groups?: string[];
    level?: string;
    pattern?: string;
    stream?: string;
    /** Loaded raw events, used only to suggest fields to group by. */
    events?: LogEvent[];
    /** Bump to reload with the same settings. */
    reloadToken?: number;
    /** Open the raw events for one group. */
    onDrill: (key: string) => void;
    onLoadingChange?: (loading: boolean) => void;
  } = $props();

  const STORAGE_KEY = "tarn-log-summary";
  const LEVEL_ORDER = ["ERROR", "WARN", "INFO", "DEBUG"] as const;

  type Settings = { groupBy: string; fields: string; flattenKind: boolean };

  function readSettings(): Settings {
    const fallback: Settings = { groupBy: "correlationId", fields: "", flattenKind: true };
    if (typeof localStorage === "undefined") return fallback;
    try {
      const raw = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}") as Partial<Settings>;
      return {
        groupBy: typeof raw.groupBy === "string" && raw.groupBy.trim() ? raw.groupBy : fallback.groupBy,
        fields: typeof raw.fields === "string" ? raw.fields : fallback.fields,
        flattenKind: typeof raw.flattenKind === "boolean" ? raw.flattenKind : fallback.flattenKind,
      };
    } catch {
      return fallback;
    }
  }

  const initial = readSettings();
  // Drafts are what the inputs show; applied values drive the request.
  let groupByDraft = $state(initial.groupBy);
  let fieldsDraft = $state(initial.fields);
  let groupBy = $state(initial.groupBy);
  let fields = $state(initial.fields);
  let flattenKind = $state(initial.flattenKind);

  let summary = $state<LogSummary | null>(null);
  let loading = $state(false);
  let error = $state("");
  let expanded = $state<Set<string>>(new Set());
  let showUngrouped = $state(false);

  $effect(() => {
    if (typeof localStorage === "undefined") return;
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ groupBy, fields, flattenKind }));
  });

  $effect(() => {
    onLoadingChange?.(loading);
  });

  let controller: AbortController | null = null;

  async function load(params: {
    groupBy: string;
    fields: string;
    flatten: string;
    groups: string[];
    level: string;
    pattern: string;
    stream: string;
  }) {
    controller?.abort();
    const ctrl = new AbortController();
    controller = ctrl;
    loading = true;
    error = "";
    try {
      const result = await fetchLogSummary(
        {
          groupBy: params.groupBy,
          fields: params.fields || undefined,
          flatten: params.flatten || undefined,
          groups: params.groups,
          level: params.level || undefined,
          pattern: params.pattern || undefined,
          stream: params.stream || undefined,
        },
        ctrl.signal,
      );
      if (ctrl.signal.aborted) return;
      summary = result;
      // Open error groups by default: they are what the summary is for.
      expanded = new Set(result.groups.filter((g) => (g.levels.ERROR ?? 0) > 0).slice(0, 3).map((g) => g.key));
    } catch (err) {
      if (ctrl.signal.aborted) return;
      error = err instanceof Error ? err.message : "Failed to summarise logs";
    } finally {
      if (controller === ctrl) {
        controller = null;
        loading = false;
      }
    }
  }

  $effect(() => {
    void reloadToken;
    load({
      groupBy: groupBy.trim(),
      fields: fields.trim(),
      flatten: flattenKind ? "kind" : "",
      groups: [...groups],
      level,
      pattern,
      stream,
    });
  });

  function applyGroupBy(value = groupByDraft) {
    const next = value.trim();
    if (!next) {
      groupByDraft = groupBy;
      return;
    }
    groupByDraft = next;
    groupBy = next;
  }

  function applyFields() {
    fields = fieldsDraft.trim();
  }

  function toggle(key: string) {
    const next = new Set(expanded);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    expanded = next;
  }

  // Suggest top-level scalar keys that appear in the loaded JSON lines, most
  // common first. "message" is always offered: it groups by line type.
  const suggestions = $derived.by(() => {
    const counts = new Map<string, number>();
    for (const ev of events.slice(0, 500)) {
      const msg = ev.message.trimStart();
      if (!msg.startsWith("{")) continue;
      let doc: unknown;
      try {
        doc = JSON.parse(msg);
      } catch {
        continue;
      }
      if (!doc || typeof doc !== "object" || Array.isArray(doc)) continue;
      for (const [k, v] of Object.entries(doc as Record<string, unknown>)) {
        if (k === "message" || k === "level" || k === "timestamp" || k === "time") continue;
        if (typeof v === "string" || typeof v === "number" || typeof v === "boolean") {
          counts.set(k, (counts.get(k) ?? 0) + 1);
        }
      }
    }
    const ranked = [...counts.entries()]
      .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
      .slice(0, 6)
      .map(([k]) => k);
    const preferred = ["correlationId", "message"];
    return [...new Set([...preferred, ...ranked])];
  });

  function levelEntries(g: LogSummaryGroup): { level: string; count: number }[] {
    const known = LEVEL_ORDER.filter((l) => g.levels[l]).map((l) => ({ level: l as string, count: g.levels[l] }));
    const other = Object.entries(g.levels)
      .filter(([l, n]) => n && !(LEVEL_ORDER as readonly string[]).includes(l))
      .map(([level, count]) => ({ level: level || "—", count }));
    return [...known, ...other];
  }

  function errorCount(g: LogSummaryGroup): number {
    return g.levels.ERROR ?? 0;
  }

  function shortGroup(name: string): string {
    const parts = name.split("/").filter(Boolean);
    return parts.length > 0 ? parts[parts.length - 1] : name;
  }

  function formatTime(ts?: string): string {
    if (!ts) return "—";
    const d = new Date(ts);
    if (Number.isNaN(d.getTime())) return ts;
    const hms = d.toLocaleTimeString([], { hour12: false });
    return `${hms}.${String(d.getMilliseconds()).padStart(3, "0")}`;
  }

  function span(g: LogSummaryGroup): string {
    const ms = new Date(g.lastAt).getTime() - new Date(g.firstAt).getTime();
    if (!Number.isFinite(ms) || ms <= 0) return "";
    if (ms < 1000) return `${ms}ms`;
    if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
    return `${Math.round(ms / 60_000)}m`;
  }

  /** Splits a sample into its message line and the remaining fields. */
  function sampleParts(s: LogSummarySample): { message: string; rest: string | null } {
    const { timestamp: _t, logGroup: _g, message, level: _l, ...rest } = s;
    // The group key is already the row title.
    const key = summary?.groupBy ?? "";
    if (key && key !== "message" && !key.includes(".")) delete rest[key];
    const text = typeof message === "string" ? message : message === undefined ? "" : JSON.stringify(message);
    const keys = Object.keys(rest);
    return { message: text, rest: keys.length > 0 ? JSON.stringify(rest, null, 2) : null };
  }

  const totalsLine = $derived.by(() => {
    if (!summary) return [];
    const t = summary.totals;
    const parts = [
      `${t.eventsScanned.toLocaleString()} events`,
      `${t.groups.toLocaleString()} ${t.groups === 1 ? "group" : "groups"}`,
    ];
    if (t.groupsReturned < t.groups) parts.push(`top ${t.groupsReturned} shown`);
    if (t.runtimeFiltered) parts.push(`${t.runtimeFiltered.toLocaleString()} runtime hidden`);
    return parts;
  });
</script>

{#snippet sample(s: LogSummarySample, label: string, tone: "error" | "plain" = "plain")}
  {@const parts = sampleParts(s)}
  <div class="ls-sample" class:error={tone === "error"}>
    <div class="ls-sample-head">
      <span class="ls-sample-label">{label}</span>
      <span class="ls-mono ls-dim">{formatTime(s.timestamp)}</span>
      {#if s.logGroup}
        <span class="ls-mono ls-dim truncate" title={s.logGroup}>{shortGroup(s.logGroup)}</span>
      {/if}
    </div>
    {#if parts.message}
      <p class="ls-sample-message">{@html highlightSearchText(parts.message, pattern)}</p>
    {/if}
    {#if parts.rest}
      <pre class="ls-code">{@html highlightJSON(parts.rest, pattern)}</pre>
    {/if}
  </div>
{/snippet}

<div class="ls-root">
  <div class="ls-controls">
    <label class="ls-field ls-groupby">
      <span>Group by</span>
      <input
        type="text"
        spellcheck="false"
        placeholder="correlationId, orderId, outcomes.kind"
        bind:value={groupByDraft}
        onkeydown={(e) => {
          if (e.key === "Enter") applyGroupBy();
        }}
        onblur={() => applyGroupBy()}
      />
    </label>
    <div class="ls-chips" role="group" aria-label="Suggested fields">
      {#each suggestions as s (s)}
        <button type="button" class="ls-chip" class:active={groupBy === s} aria-pressed={groupBy === s} onclick={() => applyGroupBy(s)}>
          {s}
        </button>
      {/each}
    </div>
    <label class="ls-field ls-fields">
      <span>Fields</span>
      <input
        type="text"
        spellcheck="false"
        placeholder="message, group field, level"
        bind:value={fieldsDraft}
        onkeydown={(e) => {
          if (e.key === "Enter") applyFields();
        }}
        onblur={applyFields}
      />
    </label>
    <button
      type="button"
      class="ls-chip ls-flatten"
      class:active={flattenKind}
      aria-pressed={flattenKind}
      title={'Shrink {"kind":"deleted",…} objects to "deleted"'}
      onclick={() => (flattenKind = !flattenKind)}
    >
      Flatten kind
    </button>
  </div>

  {#if error}
    <div class="ls-error" role="alert">{error}</div>
  {/if}

  {#if !summary && loading}
    <div class="ls-panel space-y-1.5 p-3">
      {#each Array(8) as _, i (i)}
        <Skeleton class="h-6 w-full" />
      {/each}
    </div>
  {:else if summary}
    {@const s = summary}
    <div class="ls-totals ls-mono">
      {#each totalsLine as part, i (i)}
        {#if i > 0}<span class="ls-sep">·</span>{/if}
        <span>{part}</span>
      {/each}
      {#if s.totals.errorGroups > 0}
        <span class="ls-sep">·</span>
        <span class="ls-err-text">{s.totals.errorGroups} with errors</span>
      {/if}
      {#if s.totals.truncatedScan}
        <span class="ls-sep">·</span>
        <span class="ls-warn-text" title="Only the newest 50 000 matching events were considered">scan truncated</span>
      {/if}
      {#if s.window.from}
        <span class="ls-window">{formatTime(s.window.from)} → {formatTime(s.window.to)}</span>
      {/if}
    </div>

    <div class="ls-panel ls-list" class:loading>
      {#if s.groups.length === 0}
        <EmptyState
          message={s.totals.eventsScanned === 0
            ? "No log events match these filters."
            : `No lines have a "${s.groupBy}" field. Try another field, or group by message.`}
          icon={RowsIcon}
        />
      {:else}
        {#each s.groups as g, i (g.key)}
          {@const open = expanded.has(g.key)}
          {@const errors = errorCount(g)}
          <div class="ls-group" class:open class:has-error={errors > 0} style:--i={Math.min(i, 12)}>
            <div class="ls-row">
              <button type="button" class="ls-row-main" aria-expanded={open} onclick={() => toggle(g.key)}>
                <CaretRightIcon size={11} class="ls-caret {open ? 'rotated' : ''}" />
                <span class="ls-key ls-mono" title={g.key}>{g.key}</span>
                <span class="ls-levels">
                  {#each levelEntries(g) as { level: l, count } (l)}
                    <span class="ls-level" data-level={l}>{count}<small>{l.toLowerCase()}</small></span>
                  {/each}
                </span>
                <span class="ls-groups" title={g.logGroups.join("\n")}>
                  {g.logGroups.map(shortGroup).join(", ")}
                </span>
                <span class="ls-time ls-mono">
                  {formatTime(g.lastAt)}
                  {#if span(g)}<small>{span(g)}</small>{/if}
                </span>
              </button>
              <button type="button" class="ls-drill" title="Show these events" onclick={() => onDrill(g.key)}>
                Events <ArrowUpRightIcon size={10} />
              </button>
            </div>
            {#if open}
              <div class="ls-body" transition:slide={{ duration: 180 }}>
                {#if g.errors.length > 0}
                  {#each g.errors as e, j (j)}
                    {@render sample(e, e.level && String(e.level).toUpperCase().startsWith("WARN") ? "warn" : "error", "error")}
                  {/each}
                  {#if g.errorsDropped > 0}
                    <p class="ls-dim ls-note">+{g.errorsDropped} more not listed</p>
                  {/if}
                {/if}
                {#if g.count > 1}
                  {@render sample(g.first, "first")}
                {/if}
                {@render sample(g.last, g.count > 1 ? "last" : "only")}
              </div>
            {/if}
          </div>
        {/each}
      {/if}
    </div>

    {#if s.totals.ungrouped > 0}
      <div class="ls-panel ls-ungrouped">
        <button type="button" class="ls-row-main" aria-expanded={showUngrouped} onclick={() => (showUngrouped = !showUngrouped)}>
          <CaretRightIcon size={11} class="ls-caret {showUngrouped ? 'rotated' : ''}" />
          <WarningIcon size={12} class="text-[var(--text-tertiary)]" />
          <span>
            {s.totals.ungrouped.toLocaleString()} {s.totals.ungrouped === 1 ? "line" : "lines"} without
            <span class="ls-mono">{s.groupBy}</span>
          </span>
          <span class="ls-dim ml-auto">newest {s.ungroupedSample.length}</span>
        </button>
        {#if showUngrouped}
          <div class="ls-body" transition:slide={{ duration: 180 }}>
            {#each s.ungroupedSample as u, j (j)}
              {@render sample(u, "line")}
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  {/if}
</div>

<style>
  .ls-root {
    display: flex;
    min-height: 0;
    flex: 1;
    flex-direction: column;
    gap: 10px;
  }

  .ls-mono {
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
  }
  .ls-dim {
    color: var(--text-tertiary);
  }

  /* ─── Controls ─── */
  .ls-controls {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: 8px 10px;
  }
  .ls-field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .ls-field span {
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }
  .ls-field input {
    height: 26px;
    padding: 0 8px;
    border-radius: 8px;
    border: 1px solid var(--border-subtle);
    background: transparent;
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 11.5px;
    color: var(--text-primary);
    outline: none;
    transition: border-color 120ms ease;
  }
  .ls-field input:focus {
    border-color: var(--border-focus);
  }
  .ls-groupby input {
    width: 200px;
  }
  .ls-fields input {
    width: 190px;
  }

  .ls-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .ls-chip {
    height: 26px;
    padding: 0 9px;
    border-radius: 8px;
    border: 1px solid var(--border-subtle);
    background: transparent;
    color: var(--text-secondary);
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 11px;
    cursor: pointer;
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease;
  }
  .ls-chip:hover {
    color: var(--text-primary);
    background: var(--bg-element-hover);
  }
  .ls-chip:active {
    transform: scale(0.96);
  }
  .ls-chip.active {
    color: var(--text-primary);
    border-color: color-mix(in srgb, var(--accent-green) 45%, transparent);
    background: color-mix(in srgb, var(--accent-green) 10%, transparent);
  }
  .ls-flatten {
    font-family: inherit;
  }

  .ls-error {
    padding: 8px 12px;
    border-radius: 8px;
    border: 1px solid color-mix(in srgb, var(--accent-red) 30%, transparent);
    color: var(--accent-red);
    font-size: 12px;
  }

  /* ─── Totals ─── */
  .ls-totals {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    padding: 0 2px;
    font-size: 11px;
    color: var(--text-secondary);
  }
  .ls-sep {
    color: var(--text-tertiary);
  }
  .ls-err-text {
    color: var(--accent-red);
  }
  .ls-warn-text {
    color: var(--accent-amber);
  }
  .ls-window {
    margin-left: auto;
    color: var(--text-tertiary);
  }

  /* ─── Groups ─── */
  .ls-panel {
    border: 1px solid var(--border-subtle);
    border-radius: 12px;
    background: var(--bg-stage);
  }
  .ls-list {
    min-height: 0;
    flex: 1;
    overflow-y: auto;
    transition: opacity 160ms ease;
  }
  .ls-list.loading {
    opacity: 0.6;
  }
  .ls-ungrouped {
    flex-shrink: 0;
    max-height: 40%;
    overflow-y: auto;
  }

  .ls-group {
    position: relative;
    border-bottom: 1px solid var(--border-subtle);
    animation: lsIn 320ms var(--ease-snappy) both;
    animation-delay: calc(var(--i) * 30ms);
  }
  .ls-group:last-child {
    border-bottom: 0;
  }
  .ls-group.has-error::before {
    content: "";
    position: absolute;
    left: 5px;
    top: 9px;
    width: 2.5px;
    height: 16px;
    border-radius: 2px;
    background: var(--accent-red);
  }

  .ls-row {
    display: flex;
    align-items: center;
  }
  .ls-row-main {
    display: flex;
    min-width: 0;
    flex: 1;
    align-items: center;
    gap: 10px;
    padding: 8px 10px 8px 14px;
    border: 0;
    background: transparent;
    color: var(--text-secondary);
    font-size: 12px;
    text-align: left;
    cursor: pointer;
    transition: background 120ms ease, padding-left 200ms var(--ease-snappy);
  }
  .ls-row-main:hover {
    background: var(--bg-element-hover);
  }
  .ls-group.open .ls-row-main {
    padding-left: 18px;
  }
  .ls-row-main:focus-visible,
  .ls-drill:focus-visible {
    outline: 2px solid var(--border-focus);
    outline-offset: -2px;
  }
  .ls-row-main :global(.ls-caret) {
    flex-shrink: 0;
    color: var(--text-tertiary);
    transition: transform 200ms var(--ease-snappy);
  }
  .ls-row-main :global(.ls-caret.rotated) {
    transform: rotate(90deg);
  }

  .ls-key {
    min-width: 0;
    flex: 1 1 auto;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-primary);
    font-size: 12px;
  }
  .ls-levels {
    display: flex;
    flex-shrink: 0;
    gap: 4px;
  }
  .ls-level {
    --lvl: var(--text-secondary);
    display: inline-flex;
    align-items: baseline;
    gap: 3px;
    padding: 1px 6px;
    border-radius: 6px;
    border: 1px solid color-mix(in srgb, var(--lvl) 30%, transparent);
    color: var(--lvl);
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 11px;
  }
  .ls-level small {
    font-size: 9.5px;
    opacity: 0.75;
  }
  .ls-level[data-level="ERROR"] {
    --lvl: var(--accent-red);
    background: color-mix(in srgb, var(--accent-red) 8%, transparent);
  }
  .ls-level[data-level="WARN"] {
    --lvl: var(--accent-amber);
  }
  .ls-level[data-level="INFO"] {
    --lvl: var(--accent-green);
  }
  .ls-groups {
    width: 180px;
    flex-shrink: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-tertiary);
    font-size: 11px;
  }
  .ls-time {
    display: inline-flex;
    width: 118px;
    flex-shrink: 0;
    justify-content: flex-end;
    gap: 6px;
    color: var(--text-tertiary);
    font-size: 10.5px;
  }
  .ls-time small {
    color: var(--text-secondary);
    font-size: 10px;
  }

  .ls-drill {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    margin-right: 8px;
    padding: 3px 8px;
    border-radius: 8px;
    border: 1px solid transparent;
    background: transparent;
    color: var(--text-tertiary);
    font-size: 11px;
    opacity: 0;
    cursor: pointer;
    transition: opacity 120ms ease, color 120ms ease, border-color 120ms ease;
  }
  .ls-group:hover .ls-drill,
  .ls-drill:focus-visible {
    opacity: 1;
  }
  .ls-drill:hover {
    color: var(--text-primary);
    border-color: var(--border-default);
  }

  .ls-body {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 2px 12px 12px 36px;
  }
  .ls-note {
    font-size: 11px;
  }

  .ls-sample {
    padding: 7px 10px;
    border-radius: 8px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-app);
  }
  .ls-sample.error {
    border-color: color-mix(in srgb, var(--accent-red) 30%, transparent);
    background: color-mix(in srgb, var(--accent-red) 4%, var(--bg-app));
  }
  .ls-sample-head {
    display: flex;
    min-width: 0;
    align-items: baseline;
    gap: 8px;
    margin-bottom: 3px;
    font-size: 10.5px;
  }
  .ls-sample-label {
    flex-shrink: 0;
    font-size: 9.5px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }
  .ls-sample.error .ls-sample-label {
    color: var(--accent-red);
  }
  .ls-sample-message {
    color: var(--text-primary);
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 11.5px;
    word-break: break-word;
  }
  .ls-code {
    margin-top: 4px;
    max-height: 240px;
    overflow: auto;
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 11px;
    line-height: 1.55;
    white-space: pre-wrap;
    word-break: break-all;
    color: var(--text-secondary);
  }

  @keyframes lsIn {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  @media (max-width: 760px) {
    .ls-groups,
    .ls-time {
      display: none;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .ls-group {
      animation: none;
    }
    .ls-row-main,
    .ls-row-main :global(.ls-caret) {
      transition: none;
    }
  }
</style>
