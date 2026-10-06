<script lang="ts">
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcStat from "$lib/components/rack/rc-stat.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import RcTonePill, { type Tone } from "$lib/components/rack/rc-tone-pill.svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import ExecutionHistory from "./execution-history.svelte";
  import { formatJSONForViewer } from "$lib/json-format";
  import type { StateMachineSummary } from "$lib/types";

  let {
    machine,
  }: {
    machine: StateMachineSummary;
  } = $props();

  const numberFormatter = new Intl.NumberFormat("en-GB");
  const executions = $derived(machine.executions ?? []);

  let selectedExecArn = $state<string | null>(null);
  const selectedExecution = $derived(
    executions.find((execution) => execution.arn === selectedExecArn) ??
      executions[0] ??
      null,
  );

  const succeeded = $derived(executions.filter((e) => e.status === "SUCCEEDED").length);
  const failed = $derived(
    executions.filter((e) => e.status === "FAILED" || e.status === "TIMED_OUT" || e.status === "ABORTED").length,
  );
  const running = $derived(executions.filter((e) => e.status === "RUNNING").length);

  const definitionFormatted = $derived(
    machine.definition ? formatJSONForViewer(machine.definition) : null,
  );

  function formatTime(value?: string): string {
    if (!value) return "—";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
  }

  function machineTone(status: string): Tone {
    if (status === "ACTIVE") return "green";
    if (status === "DELETING") return "amber";
    return "neutral";
  }

  function execTone(status: string): Tone {
    if (status === "SUCCEEDED") return "green";
    if (status === "RUNNING") return "amber";
    if (status === "FAILED" || status === "TIMED_OUT" || status === "ABORTED") return "red";
    return "neutral";
  }

  function formatExecTime(value?: string): string {
    if (!value) return "";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
  }

  const config = $derived([
    { label: "Type", value: machine.type, mono: true },
    { label: "Status", value: machine.status, mono: true },
    { label: "Created", value: formatTime(machine.createdAt), mono: true },
    ...(machine.roleArn ? [{ label: "Role", value: machine.roleArn, mono: true, dim: true }] : []),
    { label: "ARN", value: machine.arn, mono: true, dim: true },
  ]);
</script>

<div class="detail">
  <!-- Hero -->
  <header class="hero">
    <div class="identity">
      <div class="title-row">
        <h1 title={machine.name}>{machine.name}</h1>
        <RcTonePill tone="neutral">{machine.type.toLowerCase()}</RcTonePill>
        <RcTonePill tone={machineTone(machine.status)}>{machine.status.toLowerCase()}</RcTonePill>
      </div>
      <p class="subline">
        <span>{executions.length} execution{executions.length === 1 ? "" : "s"}</span><i></i><span>created {formatTime(machine.createdAt)}</span>
      </p>
    </div>
  </header>

  <!-- Numbers -->
  <div class="stats">
    <RcStat
      label="Executions"
      value={numberFormatter.format(executions.length)}
      sub="total runs"
    />
    <RcStat
      label="Succeeded"
      value={numberFormatter.format(succeeded)}
      sub="completed"
      tone={succeeded > 0 ? "green" : undefined}
    />
    <RcStat
      label="Failed"
      value={numberFormatter.format(failed)}
      sub="failed · timed out · aborted"
      tone={failed > 0 ? "red" : undefined}
    />
    <RcStat
      label="Running"
      value={numberFormatter.format(running)}
      sub="in progress"
      tone={running > 0 ? "amber" : undefined}
    />
  </div>

  <RcPanel
    title="Executions"
    description={executions.length > 0
      ? "Select a run to inspect its history."
      : "Start one with the CLI or SDK."}
    index={0}
  >
    {#if executions.length === 0}
      <p class="empty">No executions yet.</p>
    {:else}
      <div class="rows">
        {#each executions as execution (execution.arn)}
          <RcListRow
            mono
            title={execution.name}
            sub={formatExecTime(execution.startDate)}
            selected={execution.arn === selectedExecution?.arn}
            onclick={() => (selectedExecArn = execution.arn)}
          >
            {#snippet trailing()}
              <RcTonePill tone={execTone(execution.status)}>{execution.status.toLowerCase()}</RcTonePill>
            {/snippet}
          </RcListRow>
        {/each}
      </div>
      {#if selectedExecution}
        {#key selectedExecution.arn}
          <div class="history">
            <ExecutionHistory execution={selectedExecution} />
          </div>
        {/key}
      {/if}
    {/if}
  </RcPanel>

  <RcPanel title="Definition" index={1}>
    {#if machine.definition}
      <FormattedMessageViewer
        raw={machine.definition}
        formatted={definitionFormatted?.formatted}
        formattedHtml={definitionFormatted?.formattedHtml}
        formattedLabel="JSON"
        rawLabel="Raw"
        formattedContentClass="text-[11px] text-foreground"
        rawContentClass="text-[11px] text-muted-foreground"
        formattedMaxHeightClass="max-h-[20rem]"
        rawMaxHeightClass="max-h-[20rem]"
      />
    {:else}
      <p class="empty">Definition not available.</p>
    {/if}
  </RcPanel>

  <RcPanel title="Configuration" index={2}>
    <RcKv items={config} />
  </RcPanel>
</div>

<style>
  .detail { display: flex; flex-direction: column; gap: 14px; min-width: 0; }

  .hero { display: flex; align-items: flex-end; justify-content: space-between; gap: 16px; flex-wrap: wrap; padding: 4px 2px 2px; }
  .identity { min-width: 0; }
  .title-row { display: flex; align-items: center; gap: 8px; min-width: 0; flex-wrap: wrap; }
  h1 {
    font: 600 19px var(--font-mono, ui-monospace, monospace); letter-spacing: -0.02em; color: var(--text-primary);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .subline { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 4px; font-size: 11.5px; color: var(--text-tertiary); }
  .subline i { width: 3px; height: 3px; border-radius: 1px; background: var(--border-default); }

  .stats {
    display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; padding: 14px 2px 10px;
    animation: statsIn 320ms var(--ease-snappy) both;
  }
  @keyframes statsIn { from { opacity: 0; transform: translateY(4px); } }
  @media (max-width: 1100px) { .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); } }

  .empty { font-size: 11.5px; color: var(--text-tertiary); }
  .rows { display: flex; flex-direction: column; gap: 2px; margin-bottom: 4px; }
  .history {
    margin-top: 12px; border-top: 1px solid var(--border-subtle); padding-top: 4px;
    max-height: 32rem; overflow-y: auto;
  }

  @media (prefers-reduced-motion: reduce) { .stats { animation: none; } }
</style>
