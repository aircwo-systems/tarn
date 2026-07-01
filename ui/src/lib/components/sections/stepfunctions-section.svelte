<script lang="ts">
  import { FlowArrowIcon } from "phosphor-svelte";
  import {
    Table,
    TableHeader,
    TableBody,
    TableHead,
    TableCell,
    TableRow,
  } from "$lib/components/ui/table";
  import { Badge } from "$lib/components/ui/badge";
  import LedDot from "$lib/components/common/led-dot.svelte";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import EmptyState from "$lib/components/common/empty-state.svelte";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import SectionHeader from "./section-header.svelte";
  import StateMachineDetail from "./state-machine-detail.svelte";
  import { getDashboard } from "$lib/state.svelte";
  import type { StateMachineSummary } from "$lib/types";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const machines = $derived(dashboard.data?.stateMachines ?? []);

  // selectedArn holds the user's explicit choice; the effective selection falls
  // back to the first machine (and recovers if the chosen one disappears),
  // derived without mutating state.
  let selectedArn = $state("");

  const selectedMachine = $derived(
    machines.find((machine) => machine.arn === selectedArn) ?? machines[0] ?? null,
  );

  function machineColor(status: string): "green" | "amber" | "red" | "gray" {
    if (status === "ACTIVE") return "green";
    if (status === "DELETING") return "amber";
    return "gray";
  }

  function executionCount(machine: StateMachineSummary): number {
    return machine.executions?.length ?? 0;
  }

  function formatTime(value?: string): string {
    if (!value) return "—";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
  }
</script>

<div class="flex flex-col gap-4" style="height: calc(100vh - 2rem)">
  <SectionHeader
    title="Step Functions"
    description={`${machines.length} state machine${machines.length === 1 ? "" : "s"}`}
    icon={FlowArrowIcon}
    {sidebarCollapsed}
    {onToggleSidebar}
  />

  <!-- Machines list — horizontal, on top -->
  <div class="shrink-0 overflow-hidden rounded-lg border border-border/70 bg-background/60">
    {#if dashboard.loading && !dashboard.data}
      <div class="space-y-2 p-3">
        {#each Array(3) as _, index (index)}
          <Skeleton class="h-9 w-full" />
        {/each}
      </div>
    {:else if machines.length === 0}
      <div class="flex min-h-40 items-center justify-center">
        <EmptyState
          message="No state machines yet. Create one with the AWS CLI, SDK, or Terraform."
          icon={FlowArrowIcon}
        />
      </div>
    {:else}
      <div class="max-h-[34vh] overflow-auto">
        <Table>
          <TableHeader class="sticky top-0 z-10 bg-background/95 backdrop-blur [&_th]:bg-background/95">
            <TableRow class="hover:bg-transparent">
              <TableHead class="w-[24rem]">State machine</TableHead>
              <TableHead>Type</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Executions</TableHead>
              <TableHead>Created</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each machines as machine (machine.arn)}
              <TableRow
                class={`cursor-pointer ${selectedMachine?.arn === machine.arn ? "bg-muted/50" : ""}`}
                onclick={() => (selectedArn = machine.arn)}
              >
                <TableCell class="max-w-[24rem] align-top whitespace-normal!">
                  <ArnCell name={machine.name} arn={machine.arn} />
                </TableCell>
                <TableCell>
                  <Badge variant="outline">{machine.type}</Badge>
                </TableCell>
                <TableCell>
                  <span class="inline-flex items-center gap-1.5 text-xs">
                    <LedDot color={machineColor(machine.status)} />
                    <span class="text-muted-foreground">{machine.status}</span>
                  </span>
                </TableCell>
                <TableCell class="font-mono text-xs text-muted-foreground">
                  {executionCount(machine)}
                </TableCell>
                <TableCell class="text-[11px] text-muted-foreground/80">
                  {formatTime(machine.createdAt)}
                </TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
      </div>
    {/if}
  </div>

  <!-- Detail — full width, fills remaining height -->
  <div class="min-h-0 flex-1">
    {#if selectedMachine}
      <StateMachineDetail machine={selectedMachine} />
    {:else}
      <section
        class="flex h-full min-h-0 items-center justify-center rounded-lg border border-border/70 bg-background/60 px-4 py-6 text-center text-sm text-muted-foreground"
      >
        Select a state machine to inspect its definition and execution history.
      </section>
    {/if}
  </div>
</div>
