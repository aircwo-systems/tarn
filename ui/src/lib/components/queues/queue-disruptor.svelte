<script lang="ts">
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcTonePill from "$lib/components/rack/rc-tone-pill.svelte";
  import { clearDisruptorRules, setDisruptorRules } from "$lib/api";
  import { refresh } from "$lib/state.svelte";
  import type { QueueSummary } from "$lib/types";

  let {
    targets,
    index = 0,
  }: {
    /** One queue (detail panel) or many (bulk action). */
    targets: QueueSummary[];
    index?: number;
  } = $props();

  const CODES = [
    { value: "ServiceUnavailable", label: "503 ServiceUnavailable", hint: "retriable" },
    { value: "InternalError", label: "500 InternalError", hint: "retriable" },
    { value: "Throttling", label: "400 Throttling", hint: "backoff" },
    { value: "OverLimit", label: "400 OverLimit", hint: "quota" },
  ];

  const names = $derived(targets.map((t) => t.name));
  const armed = $derived(targets.filter((t) => t.disruptEnabled));
  const allArmed = $derived(targets.length > 0 && armed.length === targets.length);

  let failureRate = $state(50);
  let code = $state("ServiceUnavailable");
  let busy = $state(false);
  let status = $state("");
  let statusTone: "ok" | "err" = $state("ok");

  // Seed controls from the current rule when a single armed queue is shown.
  $effect(() => {
    if (targets.length === 1 && targets[0].disruptEnabled) {
      failureRate = targets[0].disruptFailureRate ?? failureRate;
      if (targets[0].disruptCode) code = targets[0].disruptCode;
    }
  });

  function targetLabel(): string {
    if (targets.length === 1) return targets[0].name;
    return `${targets.length} queues`;
  }

  async function apply() {
    if (names.length === 0 || busy) return;
    busy = true;
    status = "";
    try {
      await setDisruptorRules({
        queues: names,
        enabled: true,
        failureRate,
        code,
      });
      statusTone = "ok";
      status = `Failing ~${failureRate}% of sends to ${targetLabel()} (${code}).`;
      await refresh();
    } catch (error) {
      statusTone = "err";
      status = error instanceof Error ? error.message : "Failed to arm disruptor";
    } finally {
      busy = false;
    }
  }

  async function disarm() {
    if (names.length === 0 || busy) return;
    busy = true;
    status = "";
    try {
      await clearDisruptorRules(names);
      statusTone = "ok";
      status = `Disruptor off for ${targetLabel()}. Sends succeed normally.`;
      await refresh();
    } catch (error) {
      statusTone = "err";
      status = error instanceof Error ? error.message : "Failed to disarm disruptor";
    } finally {
      busy = false;
    }
  }
</script>

<RcPanel
  title="Disruptor"
  description={targets.length <= 1
    ? "Deliberately fail publishes to test consumer retries."
    : `Deliberately fail publishes to ${targets.length} queues at once.`}
  {index}
>
  {#snippet actions()}
    {#if allArmed}
      <RcTonePill tone="red">armed</RcTonePill>
    {:else if armed.length > 0}
      <RcTonePill tone="amber">{armed.length}/{targets.length} armed</RcTonePill>
    {:else}
      <RcTonePill tone="neutral">off</RcTonePill>
    {/if}
  {/snippet}

  <div class="disruptor">
    {#if targets.length > 1}
      <p class="targets" title={names.join(", ")}>
        {names.slice(0, 3).join(", ")}{#if names.length > 3} +{names.length - 3} more{/if}
      </p>
    {/if}

    <div class="row">
      <label class="rate">
        <span class="field-label">Failure rate <strong>{failureRate}%</strong></span>
        <input
          type="range"
          min="1"
          max="100"
          step="1"
          bind:value={failureRate}
          disabled={busy}
          aria-label="Failure rate percent"
        />
      </label>
      <label class="code">
        <span class="field-label">Error</span>
        <select bind:value={code} disabled={busy} aria-label="Error code">
          {#each CODES as c (c.value)}
            <option value={c.value}>{c.label}</option>
          {/each}
        </select>
      </label>
    </div>

    <div class="controls">
      <button type="button" class="btn arm" onclick={apply} disabled={busy}>
        {busy ? "Applying…" : allArmed ? "Update" : "Arm disruptor"}
      </button>
      <button
        type="button"
        class="btn ghost"
        onclick={disarm}
        disabled={busy || armed.length === 0}
      >
        Disarm
      </button>
    </div>

    {#if status}
      <p class="status" class:err={statusTone === "err"}>{status}</p>
    {:else}
      <p class="hint">Sends fail before reaching the queue — SDKs see real {code} errors, including partial batch failures.</p>
    {/if}
  </div>
</RcPanel>

<style>
  .disruptor { display: flex; flex-direction: column; gap: 10px; }
  .targets {
    font-family: var(--font-mono, ui-monospace, monospace); font-size: 11px;
    color: var(--text-tertiary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .row { display: flex; gap: 12px; flex-wrap: wrap; }
  .rate { flex: 1 1 180px; display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .code { flex: 0 1 220px; display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .field-label { font-size: 11px; color: var(--text-tertiary); }
  .field-label strong { color: var(--text-primary); font-variant-numeric: tabular-nums; }
  input[type="range"] { width: 100%; accent-color: var(--accent-red); }
  select {
    height: 28px; padding: 0 8px; border-radius: 8px; border: 1px solid var(--border-subtle);
    background: var(--bg-app); color: var(--text-primary); font-size: 11.5px;
  }
  .controls { display: flex; gap: 6px; }
  .btn {
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 11px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11.5px; color: var(--text-secondary);
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .btn:hover:not(:disabled) { color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element-hover); }
  .btn:active:not(:disabled) { transform: scale(0.96); }
  .btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn.arm:not(:disabled) { color: var(--accent-red); border-color: color-mix(in srgb, var(--accent-red) 40%, transparent); }
  .btn.ghost { border-color: transparent; }
  .btn.ghost:hover:not(:disabled) { border-color: var(--border-subtle); }
  .btn:focus-visible, select:focus-visible, input:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }
  .status { font-size: 11.5px; color: var(--text-secondary); }
  .status.err { color: var(--accent-red); }
  .hint { font-size: 11px; color: var(--text-tertiary); }
</style>
