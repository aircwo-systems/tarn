<script lang="ts">
  import { CheckIcon, CopyIcon, ListBulletsIcon, PathIcon } from "phosphor-svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcStat from "$lib/components/rack/rc-stat.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import RcTonePill, { type Tone } from "$lib/components/rack/rc-tone-pill.svelte";
  import FunctionInvocations from "./function-invocations.svelte";
  import FunctionLinksList, { type LinkItem } from "./function-links-list.svelte";
  import FunctionLogs from "./function-logs.svelte";
  import {
    configuredCallers,
    dependencies,
    inputExamples,
    invocationsFromTraces,
    observedCallers,
    percentile,
    type FunctionCaller,
  } from "$lib/function-links";
  import { functionToHcl } from "$lib/function-hcl";
  import { formatMs, spanKindLabel } from "$lib/trace-utils";
  import { formatBytes, formatDate, timeAgo } from "$lib/utils";
  import type { FunctionSummary, OverviewResponse } from "$lib/types";

  let { fn, data }: { fn: FunctionSummary; data: OverviewResponse | null } = $props();

  const numberFormatter = new Intl.NumberFormat("en-GB");

  const invocations = $derived(invocationsFromTraces(fn, data?.recentTraces));
  const configured = $derived(configuredCallers(fn, data));
  const observed = $derived(observedCallers(invocations));
  const deps = $derived(dependencies(fn, data, invocations));
  const examples = $derived(inputExamples(fn, data));

  const durations = $derived(invocations.map((i) => i.durationMs));
  const errorCount = $derived(invocations.filter((i) => i.status === "error").length);
  const errorRate = $derived(invocations.length ? Math.round((errorCount / invocations.length) * 100) : 0);

  const stateTone = $derived.by((): Tone => {
    const s = fn.state.toLowerCase();
    if (s === "active") return "green";
    if (s === "pending") return "amber";
    if (s === "failed" || s === "inactive") return "red";
    return "neutral";
  });

  const CALLER_TAG: Record<FunctionCaller["kind"], string> = {
    gateway: "API",
    queue: "SQS",
    stream: "Stream",
    eventbridge: "Rule",
    topic: "SNS",
    stepfunctions: "SFN",
    observed: "Seen",
  };

  function callerTone(state: FunctionCaller["state"]): LinkItem["tone"] {
    if (state === "error") return "red";
    if (state === "warn") return "amber";
    if (state === "off") return "muted";
    return undefined;
  }

  const callerItems = $derived<LinkItem[]>([
    ...configured.map((c) => ({
      key: c.key,
      tag: CALLER_TAG[c.kind],
      label: c.label,
      detail: c.detail,
      href: c.href,
      tone: callerTone(c.state),
      trailing: c.state === "off" ? "off" : undefined,
    })),
    // Observed callers the config doesn't already explain (e.g. direct SDK invokes).
    ...observed
      .filter((o) => !configured.some((c) => o.label === c.label || o.label.endsWith(c.label)))
      .map((o) => ({ key: o.key, tag: CALLER_TAG.observed, label: o.label, detail: o.detail })),
  ]);

  const depItems = $derived<LinkItem[]>(
    deps.map((d) => ({
      key: d.key,
      tag: spanKindLabel(d.kind),
      label: d.name,
      detail: d.detail,
      tone: d.errors > 0 ? "red" : undefined,
      trailing: d.calls ? `${d.calls}×${d.errors ? ` · ${d.errors} err` : ""}` : undefined,
    })),
  );

  const config = $derived([
    { label: "Runtime", value: fn.runtime, mono: true },
    { label: "Memory", value: `${fn.memoryMB} MB`, mono: true },
    { label: "Timeout", value: `${fn.timeoutSec}s`, mono: true },
    { label: "Code size", value: formatBytes(fn.codeSize), mono: true },
    { label: "Layers", value: fn.layers > 0 ? String(fn.layers) : "none", mono: true, dim: fn.layers === 0 },
    { label: "Version", value: fn.version, mono: true },
    { label: "Modified", value: formatDate(fn.lastModified) },
    { label: "ARN", value: fn.arn, mono: true, dim: true },
  ]);

  const tags = $derived(Object.entries(fn.tags ?? {}));

  let copied = $state<"arn" | "hcl" | null>(null);
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  async function copy(kind: "arn" | "hcl") {
    await navigator.clipboard.writeText(kind === "arn" ? fn.arn : functionToHcl(fn));
    copied = kind;
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => (copied = null), 1600);
  }

  function openTrace(traceId: string) {
    window.location.hash = `xray?trace=${encodeURIComponent(traceId)}`;
  }

  const logsHref = $derived(`#logs?groups=${encodeURIComponent(`/aws/lambda/${fn.name}`)}`);
</script>

<div class="detail">
  <!-- Hero -->
  <header class="hero">
    <div class="identity">
      <div class="title-row">
        <h1 title={fn.name}>{fn.name}</h1>
        <RcTonePill tone={stateTone}>{fn.state.toLowerCase()}</RcTonePill>
      </div>
      <p class="subline">
        <span>{fn.runtime}</span><i></i><span>{fn.memoryMB} MB</span><i></i><span>{fn.timeoutSec}s timeout</span>
        {#if fn.lastInvokedAt}<i></i><span>invoked {timeAgo(fn.lastInvokedAt)}</span>{/if}
      </p>
    </div>
    <div class="hero-actions">
      <a class="btn" href={logsHref}><ListBulletsIcon size={12} />Logs</a>
      <a class="btn" href="#xray"><PathIcon size={12} />Traces</a>
      <button type="button" class="btn" onclick={() => copy("hcl")}>
        {#if copied === "hcl"}<CheckIcon size={12} class="ok" />Copied{:else}<CopyIcon size={12} />Terraform{/if}
      </button>
    </div>
  </header>

  <!-- Numbers -->
  <div class="stats">
    <RcStat label="Invocations" value={numberFormatter.format(fn.invocations ?? 0)} sub="since start" />
    <RcStat label="Messages" value={numberFormatter.format(fn.messagesProcessed)} sub="records handled" />
    <RcStat label="p50" value={durations.length ? formatMs(percentile(durations, 50)) : "--"} sub="{invocations.length} traced" />
    <RcStat label="p95" value={durations.length ? formatMs(percentile(durations, 95)) : "--"} sub="of {fn.timeoutSec}s budget" />
    <RcStat
      label="Errors"
      value={invocations.length ? `${errorRate}%` : "--"}
      sub="{errorCount} of {invocations.length}"
      tone={errorCount > 0 ? "red" : undefined}
    />
  </div>

  <RcPanel title="Recent invocations" description="Reconstructed from recorded traces. Select one to open its trace." index={0}>
    {#if invocations.length === 0}
      <p class="empty">No traced invocations yet. Invoke the function through an API route, queue, rule or the SDK.</p>
    {:else}
      <FunctionInvocations {invocations} timeoutSec={fn.timeoutSec} onopen={openTrace} />
    {/if}
  </RcPanel>

  <div class="split">
    <RcPanel title="Callers" description="Configured triggers and callers seen in traces." index={1}>
      <FunctionLinksList items={callerItems} empty="Nothing is wired to this function yet." />
    </RcPanel>
    <RcPanel title="Reaches" description="Resources this function talks to." index={2}>
      <FunctionLinksList items={depItems} empty="No downstream calls observed." />
    </RcPanel>
  </div>

  {#if examples.length > 0}
    <RcPanel title="Input" description="Example request bodies declared on routes that target this function." index={3}>
      <div class="examples">
        {#each examples as ex (ex.label)}
          <figure>
            <figcaption>{ex.label}</figcaption>
            <pre>{ex.body}</pre>
          </figure>
        {/each}
      </div>
    </RcPanel>
  {/if}

  <RcPanel title="Latest logs" index={4}>
    {#snippet actions()}
      <a class="btn ghost" href={logsHref}>Open in Logs</a>
    {/snippet}
    <FunctionLogs functionName={fn.name} revision={fn.invocations ?? 0} />
  </RcPanel>

  <RcPanel title="Configuration" index={5}>
    {#snippet actions()}
      <button type="button" class="btn ghost" onclick={() => copy("arn")}>
        {#if copied === "arn"}<CheckIcon size={11} class="ok" />Copied{:else}<CopyIcon size={11} />ARN{/if}
      </button>
    {/snippet}
    <RcKv items={config} />
    {#if tags.length > 0}
      <div class="tags">
        {#each tags as [k, v] (k)}
          <span class="tag"><span>{k}</span>{v}</span>
        {/each}
      </div>
    {/if}
  </RcPanel>
</div>

<style>
  .detail { display: flex; flex-direction: column; gap: 14px; min-width: 0; }

  .hero { display: flex; align-items: flex-end; justify-content: space-between; gap: 16px; flex-wrap: wrap; padding: 4px 2px 2px; }
  .identity { min-width: 0; }
  .title-row { display: flex; align-items: center; gap: 10px; min-width: 0; }
  h1 {
    font: 600 19px var(--font-mono, ui-monospace, monospace); letter-spacing: -0.02em; color: var(--text-primary);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .subline { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 4px; font-size: 11.5px; color: var(--text-tertiary); }
  .subline i { width: 3px; height: 3px; border-radius: 1px; background: var(--border-default); }
  .hero-actions { display: flex; gap: 6px; }

  .btn {
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 11px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11.5px; color: var(--text-secondary); text-decoration: none;
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .btn:hover { color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element-hover); }
  .btn:active { transform: scale(0.96); }
  .btn.ghost { height: 24px; padding: 0 9px; font-size: 11px; border-color: transparent; }
  .btn.ghost:hover { border-color: var(--border-subtle); }
  .btn :global(.ok) { color: var(--accent-green); }
  .btn:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }

  .stats {
    display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 16px; padding: 14px 2px 10px;
    animation: statsIn 320ms var(--ease-snappy) both;
  }
  @keyframes statsIn { from { opacity: 0; transform: translateY(4px); } }
  @media (max-width: 1100px) { .stats { grid-template-columns: repeat(3, minmax(0, 1fr)); } }

  .split { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
  @media (max-width: 1100px) { .split { grid-template-columns: minmax(0, 1fr); } }

  .empty { font-size: 11.5px; color: var(--text-tertiary); }

  .examples { display: flex; flex-direction: column; gap: 10px; }
  figcaption { font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); margin-bottom: 6px; }
  pre {
    max-height: 220px; overflow: auto; padding: 10px 12px; border-radius: 8px; background: var(--bg-app);
    font: 11.5px/1.6 var(--font-mono, ui-monospace, monospace); color: var(--text-secondary);
  }

  .tags { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 14px; }
  .tag {
    display: inline-flex; gap: 6px; height: 22px; align-items: center; padding: 0 8px; border-radius: 8px;
    background: var(--bg-element); font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-primary);
  }
  .tag span { color: var(--text-tertiary); }

  @media (prefers-reduced-motion: reduce) { .stats { animation: none; } }
</style>
