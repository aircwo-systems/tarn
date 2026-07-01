<script lang="ts">
  import { Badge } from "$lib/components/ui/badge";
  import LedDot from "$lib/components/common/led-dot.svelte";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import ExecutionHistory from "./execution-history.svelte";
  import { formatJSONForViewer } from "$lib/json-format";
  import type { StateMachineSummary } from "$lib/types";

  let { machine }: { machine: StateMachineSummary } = $props();

  const executions = $derived(machine.executions ?? []);

  // User's explicit execution choice; falls back to the most recent execution
  // (and recovers when the machine changes) via $derived, no state mutation.
  let selectedExecArn = $state("");
  const selectedExecution = $derived(
    executions.find((execution) => execution.arn === selectedExecArn) ??
      executions[0] ??
      null,
  );

  const definitionFormatted = $derived(
    machine.definition ? formatJSONForViewer(machine.definition) : null,
  );

  function machineColor(status: string): "green" | "amber" | "red" | "gray" {
    if (status === "ACTIVE") return "green";
    if (status === "DELETING") return "amber";
    return "gray";
  }

  function execColor(status: string): "green" | "amber" | "red" | "gray" {
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

  function formatTime(value?: string): string {
    if (!value) return "—";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
  }
</script>

<section class="flex h-full min-h-0 flex-col rounded-lg border border-border/70 bg-background/60">
  <!-- Machine metadata -->
  <div class="shrink-0 space-y-2 border-b border-border px-4 py-3">
    <ArnCell name={machine.name} arn={machine.arn} />
    <div class="flex flex-wrap items-center gap-2">
      <Badge variant="outline">{machine.type}</Badge>
      <span class="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
        <LedDot color={machineColor(machine.status)} />
        {machine.status}
      </span>
      <span class="text-[11px] text-muted-foreground/70">
        created {formatTime(machine.createdAt)}
      </span>
      {#if machine.roleArn}
        <span
          class="min-w-0 truncate font-mono text-[11px] text-muted-foreground/60"
          title={machine.roleArn}
        >
          · {machine.roleArn}
        </span>
      {/if}
    </div>
  </div>

  <!-- Executions (left) + selected execution history (right) — fills height -->
  <div class="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[18rem_minmax(0,1fr)]">
    <div class="flex min-h-0 flex-col border-b border-border lg:border-b-0 lg:border-r">
      <div class="flex shrink-0 items-center justify-between px-3 py-2">
        <h4 class="text-[10px] font-semibold uppercase tracking-[0.2em] text-muted-foreground/70">
          Executions
        </h4>
        <span class="font-mono text-[11px] text-muted-foreground">{executions.length}</span>
      </div>
      <div class="min-h-0 flex-1 overflow-auto">
        {#if executions.length > 0}
          <ul>
            {#each executions as execution (execution.arn)}
              <li>
                <button
                  type="button"
                  onclick={() => (selectedExecArn = execution.arn)}
                  class={`flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors hover:bg-muted/50 ${selectedExecution?.arn === execution.arn ? "bg-muted/60" : ""}`}
                >
                  <LedDot color={execColor(execution.status)} />
                  <span
                    class="min-w-0 flex-1 truncate font-mono text-foreground"
                    title={execution.name}
                  >
                    {execution.name}
                  </span>
                  <span class="shrink-0 text-[10px] text-muted-foreground/60">
                    {formatTime(execution.startDate)}
                  </span>
                </button>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="px-3 py-2 text-xs text-muted-foreground/70">
            No executions yet. Start one with the CLI or SDK.
          </p>
        {/if}
      </div>
    </div>

    <div class="min-h-0 overflow-auto">
      {#if selectedExecution}
        <ExecutionHistory execution={selectedExecution} />
      {:else}
        <p class="px-3 py-3 text-xs text-muted-foreground/70">
          Select an execution to see what happened.
        </p>
      {/if}
    </div>
  </div>

  <!-- Definition — bounded, scrollable footer -->
  <div class="shrink-0 space-y-1.5 border-t border-border px-4 py-3">
    <h4 class="text-[10px] font-semibold uppercase tracking-[0.2em] text-muted-foreground/70">
      Definition
    </h4>
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
      <p class="text-xs text-muted-foreground/70">Definition not available.</p>
    {/if}
  </div>
</section>
