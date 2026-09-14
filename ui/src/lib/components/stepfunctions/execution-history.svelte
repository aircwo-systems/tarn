<script lang="ts">
  import { untrack } from "svelte";
  import { ArrowUpRightIcon, CaretDownIcon } from "phosphor-svelte";
  import RcTonePill, { type Tone } from "$lib/components/rack/rc-tone-pill.svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import { formatJSONForViewer } from "$lib/json-format";
  import type {
    StateMachineExecutionSummary,
    StateMachineEventSummary,
  } from "$lib/types";

  let { execution }: { execution: StateMachineExecutionSummary } = $props();

  const events = $derived(execution.events ?? []);

  // ── Grouped (contextualised) history ────────────────────────────────
  // Events emitted inside a Map iteration carry details.iteration (stamped by
  // the interpreter), and MapIterationStarted/Succeeded/Failed frame each
  // iteration. We fold the flat stream into per-item groups so a 100-cert run
  // reads as "N certificates, each with its own steps" instead of one long
  // chain. A toggle drops back to the full raw history.

  type IterGroup = {
    index: number;
    label: string;
    status: "succeeded" | "failed" | "running";
    events: StateMachineEventSummary[];
    start?: string;
    end?: string;
    error?: string;
  };

  const ITER_MARKERS = new Set([
    "MapIterationStarted",
    "MapIterationSucceeded",
    "MapIterationFailed",
  ]);

  function iterationOf(ev: StateMachineEventSummary): number | null {
    const it = (ev.details ?? {}).iteration;
    return typeof it === "number" ? it : null;
  }

  const model = $derived.by(() => {
    const groups = new Map<number, IterGroup>();
    const order: number[] = [];
    const top: StateMachineEventSummary[] = [];
    let firstIterId = Number.POSITIVE_INFINITY;

    for (const ev of events) {
      const it = iterationOf(ev);
      if (it === null) {
        top.push(ev);
        continue;
      }
      firstIterId = Math.min(firstIterId, ev.id);
      let g = groups.get(it);
      if (!g) {
        g = { index: it, label: `Item ${it}`, status: "running", events: [] };
        groups.set(it, g);
        order.push(it);
      }
      if (ev.type === "MapIterationStarted") {
        const input = (ev.details ?? {}).input as Record<string, unknown> | undefined;
        const cid = input && typeof input.certificateId === "string" ? input.certificateId : "";
        if (cid) g.label = cid;
        g.start = ev.timestamp;
        continue;
      }
      if (ev.type === "MapIterationSucceeded") {
        g.status = "succeeded";
        g.end = ev.timestamp;
        continue;
      }
      if (ev.type === "MapIterationFailed") {
        g.status = "failed";
        g.end = ev.timestamp;
        const e = (ev.details ?? {}).error;
        if (typeof e === "string") g.error = e;
        continue;
      }
      g.events.push(ev);
      if (ev.type.includes("Failed") && g.status !== "failed") g.status = "failed";
    }

    const orderedGroups = order.sort((a, b) => a - b).map((i) => groups.get(i)!);
    const pre = top.filter((ev) => ev.id < firstIterId);
    const post = top.filter((ev) => ev.id >= firstIterId);
    return { pre, post, groups: orderedGroups };
  });

  const hasGroups = $derived(model.groups.length > 0);
  const groupStats = $derived({
    total: model.groups.length,
    ok: model.groups.filter((g) => g.status === "succeeded").length,
    failed: model.groups.filter((g) => g.status === "failed").length,
  });

  let viewMode = $state<"grouped" | "full">("grouped");
  let openGroups = $state<Set<number>>(new Set());

  // Reset per execution: default collapsed, but auto-open failed items.
  //
  // Depend only on the execution's ARN (a primitive), not the `execution`
  // object itself. The dashboard replaces `execution` with a freshly cloned
  // object on every poll (default every 5s) even when nothing changed, and
  // reading `execution.arn` directly here would re-track the whole object,
  // re-running this effect — and collapsing every open group / resetting the
  // view — on every poll tick instead of only when the user picks a
  // different execution. `model.groups` is read via `untrack` for the same
  // reason: it's recomputed from a new `events` array reference each poll,
  // and we don't want that recomputation to retrigger the reset either.
  const executionArn = $derived(execution.arn);
  $effect(() => {
    executionArn;
    viewMode = "grouped";
    const failed = new Set<number>();
    for (const g of untrack(() => model.groups)) if (g.status === "failed") failed.add(g.index);
    openGroups = failed;
  });

  function toggleGroup(i: number) {
    const next = new Set(openGroups);
    if (next.has(i)) next.delete(i);
    else next.add(i);
    openGroups = next;
  }

  function groupTone(s: IterGroup["status"]): Tone {
    if (s === "succeeded") return "green";
    if (s === "failed") return "red";
    return "amber";
  }

  function groupDuration(g: IterGroup): string {
    const first = g.start ?? g.events[0]?.timestamp;
    const last = g.end ?? g.events[g.events.length - 1]?.timestamp;
    if (!first || !last) return "";
    const ms = new Date(last).getTime() - new Date(first).getTime();
    if (!Number.isFinite(ms) || ms < 0) return "";
    return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`;
  }

  const traceHash = $derived(
    execution.traceId
      ? `xray?trace=${encodeURIComponent(execution.traceId)}`
      : null,
  );

  function detailString(
    ev: StateMachineEventSummary,
    key: string,
  ): string {
    const value = (ev.details ?? {})[key];
    return typeof value === "string" ? value : "";
  }

  /** Deep-link to the exact log stream this Lambda invocation wrote to. */
  function lambdaLogHash(ev: StateMachineEventSummary): string | null {
    const group = detailString(ev, "logGroup");
    if (!group) return null;
    const stream = detailString(ev, "logStream");
    let hash = `logs?group=${encodeURIComponent(group)}`;
    if (stream) hash += `&stream=${encodeURIComponent(stream)}`;
    if (ev.timestamp) hash += `&ts=${encodeURIComponent(ev.timestamp)}`;
    return hash;
  }

  function eventRequestId(ev: StateMachineEventSummary): string {
    return detailString(ev, "requestId");
  }

  function navigate(hash: string) {
    window.location.hash = hash;
  }

  const outputFormatted = $derived(
    execution.output ? formatJSONForViewer(execution.output) : null,
  );

  function execTone(status: string): Tone {
    switch (status) {
      case "SUCCEEDED":
        return "green";
      case "RUNNING":
        return "amber";
      case "FAILED":
      case "TIMED_OUT":
      case "ABORTED":
        return "red";
      default:
        return "neutral";
    }
  }

  /** Log-style outcome tag for a history event. */
  function eventTag(type: string): { label: string; tone: Tone } {
    if (type.includes("Failed") || type.includes("TimedOut") || type.includes("Aborted"))
      return { label: "err", tone: "red" };
    if (type.includes("Succeeded")) return { label: "ok", tone: "green" };
    if (type.includes("Scheduled") || type.includes("Started") || type.endsWith("StateEntered"))
      return { label: "run", tone: "amber" };
    return { label: "···", tone: "neutral" };
  }

  function stateName(ev: StateMachineEventSummary): string {
    const details = ev.details ?? {};
    if (typeof details.name === "string") return details.name;
    if (typeof details.stateName === "string") return details.stateName;
    return "";
  }

  function eventLabel(ev: StateMachineEventSummary): string {
    const type = ev.type;
    if (type.endsWith("StateEntered")) return `Entered ${stateName(ev)}`.trim();
    if (type.endsWith("StateExited")) return `Exited ${stateName(ev)}`.trim();
    switch (type) {
      case "ExecutionStarted":
        return "Execution started";
      case "ExecutionSucceeded":
        return "Execution succeeded";
      case "ExecutionFailed":
        return "Execution failed";
      case "ExecutionAborted":
        return "Execution aborted";
      case "ExecutionTimedOut":
        return "Execution timed out";
      case "LambdaFunctionScheduled":
        return "Lambda scheduled";
      case "LambdaFunctionSucceeded":
        return "Lambda succeeded";
      case "LambdaFunctionFailed":
        return "Lambda failed";
      default:
        return type;
    }
  }

  function eventDetail(ev: StateMachineEventSummary): string {
    const details = ev.details ?? {};
    if (typeof details.resource === "string") return details.resource;
    if (typeof details.error === "string") return details.error;
    return "";
  }

  function compactTime(value?: string): string {
    if (!value) return "";
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return value;
    const p = (n: number, w = 2) => String(n).padStart(w, "0");
    return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`;
  }
</script>

<div class="history">
  <div class="history-head">
    <span class="history-name" title={execution.name}>{execution.name}</span>
    <RcTonePill tone={execTone(execution.status)}>{execution.status.toLowerCase()}</RcTonePill>
  </div>

  {#if execution.error}
    <div class="history-error">
      <p class="history-error-title">{execution.error}</p>
      {#if execution.cause}
        <p class="history-error-cause">{execution.cause}</p>
      {/if}
    </div>
  {/if}

  {#if execution.output}
    <div class="history-output">
      <p class="history-label">Output</p>
      <FormattedMessageViewer
        raw={execution.output}
        formatted={outputFormatted?.formatted}
        formattedHtml={outputFormatted?.formattedHtml}
        formattedLabel="JSON"
        rawLabel="Raw"
        formattedContentClass="text-[11px] text-foreground"
        rawContentClass="text-[11px] text-muted-foreground"
        formattedMaxHeightClass="max-h-100"
        rawMaxHeightClass="max-h-100"
      />
    </div>
  {/if}

  <div class="history-stream">
    <div class="history-stream-head">
      <p class="history-label">History</p>
      <div class="history-stream-tools">
        {#if hasGroups}
          <button
            type="button"
            onclick={() => (viewMode = viewMode === "grouped" ? "full" : "grouped")}
            class="stream-tool"
            title={viewMode === "grouped" ? "Show the full raw event history" : "Group events by certificate"}
          >
            {viewMode === "grouped" ? "Full history" : "Grouped"}
          </button>
        {/if}
        {#if traceHash}
          <button
            type="button"
            onclick={() => navigate(traceHash)}
            class="stream-tool accent"
            title="View this execution in Traces"
          >
            <ArrowUpRightIcon size={10} />
            Traces
          </button>
        {/if}
      </div>
    </div>

    {#snippet eventRow(ev: StateMachineEventSummary)}
      {@const logHash = lambdaLogHash(ev)}
      {@const tag = eventTag(ev.type)}
      {@const detail = eventDetail(ev)}
      <li class="ev" data-tone={tag.tone}>
        <span class="ev-tick" aria-hidden="true"></span>
        <span class="ev-time">{compactTime(ev.timestamp)}</span>
        <span class="ev-tag">{tag.label}</span>
        <span class="ev-msg" title={detail || eventLabel(ev)}>
          {eventLabel(ev)}
          {#if detail}<span class="ev-detail">{detail}</span>{/if}
        </span>
        {#if logHash}
          <button
            type="button"
            onclick={() => navigate(logHash)}
            class="ev-link"
            title={eventRequestId(ev)
              ? `View invoked logs (RequestId ${eventRequestId(ev)})`
              : "View invoked logs"}
          >
            <ArrowUpRightIcon size={9} />
            logs
          </button>
        {/if}
      </li>
    {/snippet}

    {#if events.length === 0}
      <p class="empty">No history recorded for this execution.</p>
    {:else if viewMode === "grouped" && hasGroups}
      <p class="group-stats">
        {groupStats.total} certificate{groupStats.total === 1 ? "" : "s"} · {groupStats.ok} ok{groupStats.failed > 0 ? ` · ${groupStats.failed} failed` : ""}
      </p>
      {#if model.pre.length > 0}
        <ol class="ev-list">
          {#each model.pre as ev (ev.id)}{@render eventRow(ev)}{/each}
        </ol>
      {/if}
      <ul class="group-list">
        {#each model.groups as g (g.index)}
          {@const open = openGroups.has(g.index)}
          <li class="group">
            <button
              type="button"
              onclick={() => toggleGroup(g.index)}
              class="group-head"
              aria-expanded={open}
            >
              <span class="ev-tick" data-tone={groupTone(g.status)} aria-hidden="true"></span>
              <span class="group-label" title={g.label}>{g.label}</span>
              <span class="group-meta">{g.events.length} steps</span>
              {#if groupDuration(g)}
                <span class="group-meta">{groupDuration(g)}</span>
              {/if}
              <RcTonePill tone={groupTone(g.status)}>{g.status}</RcTonePill>
              <span class="group-caret {open ? 'open' : ''}"><CaretDownIcon size={11} /></span>
            </button>
            {#if open}
              <ol class="ev-list indented">
                {#each g.events as ev (ev.id)}{@render eventRow(ev)}{/each}
                {#if g.error}
                  <li class="ev" data-tone="red">
                    <span class="ev-tick" aria-hidden="true"></span>
                    <span class="ev-time"></span>
                    <span class="ev-tag">err</span>
                    <span class="ev-msg">{g.error}</span>
                  </li>
                {/if}
              </ol>
            {/if}
          </li>
        {/each}
      </ul>
      {#if model.post.length > 0}
        <ol class="ev-list">
          {#each model.post as ev (ev.id)}{@render eventRow(ev)}{/each}
        </ol>
      {/if}
    {:else}
      <ol class="ev-list">
        {#each events as ev (ev.id)}{@render eventRow(ev)}{/each}
      </ol>
    {/if}
  </div>
</div>

<style>
  .history {
    display: flex; flex-direction: column; gap: 12px;
    font-family: var(--font-ui-mono, var(--font-mono, monospace)); font-size: 12px;
  }

  .history-head { display: flex; align-items: center; gap: 8px; min-width: 0; }
  .history-name {
    min-width: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    color: var(--text-primary);
  }

  .history-label {
    font-size: 10.5px; letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-tertiary);
  }

  .empty { font-size: 11.5px; color: var(--text-tertiary); font-family: var(--font-sans, ui-sans-serif, system-ui, sans-serif); }

  .history-error {
    border: 1px solid color-mix(in srgb, var(--accent-red) 30%, transparent);
    background: color-mix(in srgb, var(--accent-red) 5%, transparent);
    border-radius: 8px; padding: 8px 10px;
  }
  .history-error-title { font-size: 12px; font-weight: 600; color: var(--accent-red); font-family: var(--font-sans, ui-sans-serif, system-ui, sans-serif); }
  .history-error-cause {
    margin-top: 4px; white-space: pre-wrap; word-break: break-all;
    font-size: 11px; color: var(--text-secondary);
  }

  .history-output { display: flex; flex-direction: column; gap: 6px; }

  .history-stream { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .history-stream-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  .history-stream-tools { display: flex; align-items: center; gap: 10px; }
  .stream-tool {
    display: inline-flex; align-items: center; gap: 4px;
    font-size: 10.5px; color: var(--text-tertiary);
    transition: color 100ms ease;
  }
  .stream-tool:hover { color: var(--text-primary); }
  .stream-tool.accent { color: var(--accent-green); opacity: 0.85; }
  .stream-tool.accent:hover { color: var(--accent-green); opacity: 1; }

  /* ─── Log-style event rows ─── */
  .ev-list { display: flex; flex-direction: column; }
  .ev-list.indented { border-top: 1px solid var(--border-subtle); padding: 2px 0 4px 8px; }

  .ev {
    position: relative; display: flex; align-items: center; gap: 8px;
    min-height: 24px; padding: 3px 6px 3px 14px; border-radius: 4px;
    white-space: nowrap; transition: background 100ms ease;
  }
  .ev:hover { background: var(--bg-element-hover); }

  .ev-tick {
    position: absolute; left: 5px; top: 9px; width: 2px; height: 6px;
    border-radius: 2px; background: transparent;
  }
  .ev[data-tone="red"] .ev-tick { background: var(--accent-red); }
  .ev[data-tone="amber"] .ev-tick { background: var(--accent-amber); }

  .ev-time {
    flex-shrink: 0; font-size: 11px; color: var(--text-tertiary);
    font-variant-numeric: tabular-nums; user-select: none;
  }
  .ev:hover .ev-time { color: var(--text-secondary); }

  .ev-tag {
    width: 30px; flex-shrink: 0; font-size: 10px; font-weight: 600;
    letter-spacing: 0.04em; color: var(--text-tertiary); user-select: none;
  }
  .ev[data-tone="green"] .ev-tag { color: var(--accent-green); opacity: 0.8; }
  .ev[data-tone="amber"] .ev-tag { color: var(--accent-amber); }
  .ev[data-tone="red"] .ev-tag { color: var(--accent-red); }

  .ev-msg {
    flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis;
    color: var(--text-primary); opacity: 0.82; transition: opacity 100ms ease;
  }
  .ev[data-tone="red"] .ev-msg { color: var(--accent-red); opacity: 0.9; }
  .ev:hover .ev-msg { opacity: 1; }
  .ev-detail { color: var(--text-tertiary); opacity: 0.7; margin-left: 8px; }

  .ev-link {
    flex-shrink: 0; display: inline-flex; align-items: center; gap: 3px;
    font-size: 10px; color: var(--text-tertiary); opacity: 0;
    transition: opacity 100ms ease, color 100ms ease;
  }
  .ev:hover .ev-link, .ev-link:focus-visible { opacity: 1; }
  .ev-link:hover { color: var(--accent-green); }

  /* ─── Group headers ─── */
  .group-list { display: flex; flex-direction: column; gap: 4px; }
  .group { border: 1px solid var(--border-subtle); border-radius: 8px; overflow: hidden; }
  .group-head {
    position: relative; display: flex; align-items: center; gap: 8px; width: 100%;
    padding: 5px 8px 5px 14px; text-align: left; transition: background 100ms ease;
  }
  .group-head:hover { background: var(--bg-element-hover); }
  .group-head .ev-tick { top: 11px; }
  .group-head .ev-tick[data-tone="green"] { background: var(--accent-green); }
  .group-head .ev-tick[data-tone="red"] { background: var(--accent-red); }
  .group-head .ev-tick[data-tone="amber"] { background: var(--accent-amber); }
  .group-label {
    flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    color: var(--text-primary);
  }
  .group-meta { flex-shrink: 0; font-size: 10px; color: var(--text-tertiary); font-variant-numeric: tabular-nums; }
  .group-caret { flex-shrink: 0; display: inline-flex; color: var(--text-tertiary); transition: transform 180ms var(--ease-snappy); }
  .group-caret.open { transform: rotate(180deg); }

  .group-stats { font-size: 10.5px; color: var(--text-tertiary); }

  @media (prefers-reduced-motion: reduce) {
    .ev, .ev-msg, .ev-link, .group-head, .group-caret { transition: none; }
  }
</style>
