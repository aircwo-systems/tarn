<script lang="ts">
  import { ArrowUpRightIcon, CaretDownIcon } from "phosphor-svelte";
  import { Badge } from "$lib/components/ui/badge";
  import LedDot from "$lib/components/common/led-dot.svelte";
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
  $effect(() => {
    execution.arn;
    viewMode = "grouped";
    const failed = new Set<number>();
    for (const g of model.groups) if (g.status === "failed") failed.add(g.index);
    openGroups = failed;
  });

  function toggleGroup(i: number) {
    const next = new Set(openGroups);
    if (next.has(i)) next.delete(i);
    else next.add(i);
    openGroups = next;
  }

  function groupDotColor(s: IterGroup["status"]): "green" | "amber" | "red" | "gray" {
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

  function statusColor(status: string): "green" | "amber" | "red" | "gray" {
    switch (status) {
      case "SUCCEEDED":
        return "green";
      case "RUNNING":
        return "amber";
      case "FAILED":
      case "TIMED_OUT":
        return "red";
      default:
        return "gray";
    }
  }

  function statusVariant(
    status: string,
  ): "default" | "destructive" | "amber" | "outline" {
    switch (status) {
      case "SUCCEEDED":
        return "default";
      case "FAILED":
      case "TIMED_OUT":
        return "destructive";
      case "RUNNING":
        return "amber";
      default:
        return "outline";
    }
  }

  function eventColor(type: string): "green" | "amber" | "red" | "gray" {
    if (type.includes("Succeeded")) return "green";
    if (type.includes("Failed") || type.includes("TimedOut")) return "red";
    if (type.includes("Scheduled") || type.includes("Started")) return "amber";
    return "gray";
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

  function formatTime(value?: string): string {
    if (!value) return "";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleTimeString();
  }
</script>

<div class="flex flex-col gap-3 p-3">
  <div class="flex flex-wrap items-center gap-2">
    <LedDot color={statusColor(execution.status)} />
    <span
      class="min-w-0 truncate font-mono text-xs text-foreground"
      title={execution.name}
    >
      {execution.name}
    </span>
    <Badge variant={statusVariant(execution.status)}>{execution.status}</Badge>
  </div>

  {#if execution.error}
    <div class="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2">
      <p class="text-xs font-semibold text-destructive">{execution.error}</p>
      {#if execution.cause}
        <p class="mt-1 whitespace-pre-wrap break-words font-mono text-[11px] text-muted-foreground">
          {execution.cause}
        </p>
      {/if}
    </div>
  {/if}

  {#if execution.output}
    <div class="space-y-1">
      <h5 class="text-[10px] font-semibold uppercase tracking-[0.2em] text-muted-foreground/70">
        Output
      </h5>
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

  <div class="space-y-1.5">
    <div class="flex items-center justify-between gap-2">
      <h5 class="text-[10px] font-semibold uppercase tracking-[0.2em] text-muted-foreground/70">
        History
      </h5>
      <div class="flex items-center gap-3">
        {#if hasGroups}
          <button
            type="button"
            onclick={() => (viewMode = viewMode === "grouped" ? "full" : "grouped")}
            class="text-[10px] font-mono text-muted-foreground/70 hover:text-foreground transition-colors"
            title={viewMode === "grouped" ? "Show the full raw event history" : "Group events by certificate"}
          >
            {viewMode === "grouped" ? "Full history" : "Grouped"}
          </button>
        {/if}
        {#if traceHash}
          <button
            type="button"
            onclick={() => navigate(traceHash)}
            class="inline-flex items-center gap-1 text-[10px] font-mono text-primary/70 hover:text-primary transition-colors"
            title="View this execution in X-Ray"
          >
            <ArrowUpRightIcon size={10} />
            View X-Ray trace
          </button>
        {/if}
      </div>
    </div>

    {#snippet eventRow(ev: StateMachineEventSummary)}
      {@const logHash = lambdaLogHash(ev)}
      <li class="flex items-start gap-2 py-1">
        <span class="mt-1 shrink-0"><LedDot color={eventColor(ev.type)} /></span>
        <div class="min-w-0 flex-1">
          <div class="flex items-baseline gap-2">
            <span class="text-xs text-foreground">{eventLabel(ev)}</span>
            {#if eventDetail(ev)}
              <span
                class="min-w-0 truncate font-mono text-[10px] text-muted-foreground/70"
                title={eventDetail(ev)}
              >
                {eventDetail(ev)}
              </span>
            {/if}
          </div>
          <div class="flex items-center gap-2 font-mono text-[10px] text-muted-foreground/50">
            <span>{formatTime(ev.timestamp)}</span>
            {#if logHash}
              <button
                type="button"
                onclick={() => navigate(logHash)}
                class="inline-flex items-center gap-1 text-primary/70 hover:text-primary transition-colors"
                title={eventRequestId(ev)
                  ? `View invoked logs (RequestId ${eventRequestId(ev)})`
                  : "View invoked logs"}
              >
                <ArrowUpRightIcon size={9} />
                View invoked logs
              </button>
            {/if}
          </div>
        </div>
      </li>
    {/snippet}

    {#if events.length === 0}
      <p class="text-xs text-muted-foreground/70">
        No history recorded for this execution.
      </p>
    {:else if viewMode === "grouped" && hasGroups}
      <p class="font-mono text-[10px] text-muted-foreground/70">
        {groupStats.total} certificate{groupStats.total === 1 ? "" : "s"} · {groupStats.ok} ok{groupStats.failed > 0 ? ` · ${groupStats.failed} failed` : ""}
      </p>
      {#if model.pre.length > 0}
        <ol class="space-y-0">
          {#each model.pre as ev (ev.id)}{@render eventRow(ev)}{/each}
        </ol>
      {/if}
      <ul class="space-y-1">
        {#each model.groups as g (g.index)}
          {@const open = openGroups.has(g.index)}
          <li class="overflow-hidden rounded-md border border-border/60">
            <button
              type="button"
              onclick={() => toggleGroup(g.index)}
              class="flex w-full items-center gap-2 px-2 py-1.5 text-left transition-colors hover:bg-muted/50"
            >
              <LedDot color={groupDotColor(g.status)} />
              <span class="min-w-0 flex-1 truncate font-mono text-[11px] text-foreground" title={g.label}>
                {g.label}
              </span>
              <span class="shrink-0 font-mono text-[10px] text-muted-foreground/55">{g.events.length} steps</span>
              {#if groupDuration(g)}
                <span class="shrink-0 font-mono text-[10px] text-muted-foreground/45">{groupDuration(g)}</span>
              {/if}
              <Badge
                variant={g.status === "failed" ? "destructive" : g.status === "succeeded" ? "default" : "amber"}
                class="shrink-0 text-[9px]"
              >
                {g.status}
              </Badge>
              <CaretDownIcon size={11} class="shrink-0 text-muted-foreground/60 transition-transform {open ? 'rotate-180' : ''}" />
            </button>
            {#if open}
              <ol class="space-y-0 border-t border-border/40 px-2 py-1">
                {#each g.events as ev (ev.id)}{@render eventRow(ev)}{/each}
                {#if g.error}
                  <li class="py-1 font-mono text-[10px] text-destructive">{g.error}</li>
                {/if}
              </ol>
            {/if}
          </li>
        {/each}
      </ul>
      {#if model.post.length > 0}
        <ol class="space-y-0">
          {#each model.post as ev (ev.id)}{@render eventRow(ev)}{/each}
        </ol>
      {/if}
    {:else}
      <ol class="space-y-0">
        {#each events as ev (ev.id)}{@render eventRow(ev)}{/each}
      </ol>
    {/if}
  </div>
</div>
