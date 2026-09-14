<script lang="ts">
  import { onMount } from "svelte";
  import SectionHeader from "./section-header.svelte";
  import StateMachineList from "$lib/components/stepfunctions/state-machine-list.svelte";
  import StateMachineDetail from "$lib/components/stepfunctions/state-machine-detail.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import { getDashboard } from "$lib/state.svelte";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const machines = $derived(dashboard.data?.stateMachines ?? []);
  const totalExecutions = $derived(
    machines.reduce((sum, machine) => sum + (machine.executions?.length ?? 0), 0),
  );

  // Keyed by arn: polling replaces the objects, so holding one would freeze the panel.
  let selectedArn = $state<string | null>(null);
  const selectedMachine = $derived(
    machines.find((machine) => machine.arn === selectedArn) ?? machines[0] ?? null,
  );

  function select(arn: string) {
    selectedArn = arn;
    history.replaceState(null, "", `#stepfunctions?machine=${encodeURIComponent(arn)}`);
  }

  onMount(() => {
    const qs = window.location.hash.split("?")[1];
    const machine = qs ? new URLSearchParams(qs).get("machine") : null;
    if (machine) selectedArn = machine;
  });
</script>

<div class="stepfunctions">
  <SectionHeader
    title="Step Functions"
    description="{machines.length} state machine{machines.length === 1 ? '' : 's'} · {totalExecutions} executions"
    {sidebarCollapsed}
    {onToggleSidebar}
  />

  {#if dashboard.loading && !dashboard.data}
    <div class="layout">
      <div class="skeleton-list">
        {#each Array(6) as _, i (i)}<span style:--i={i}></span>{/each}
      </div>
    </div>
  {:else if machines.length === 0}
    <div class="blank">
      <h2>No state machines yet</h2>
      <p>Create one with the AWS CLI, SDK, or Terraform.</p>
    </div>
  {:else}
    <div class="layout">
      <RcResizableAside storageKey="tarn-stepfunctions-list-width">
        <StateMachineList
          machines={machines}
          selectedArn={selectedMachine?.arn ?? null}
          onselect={select}
        />
      </RcResizableAside>
      {#if selectedMachine}
        {#key selectedMachine.arn}
          <StateMachineDetail machine={selectedMachine} />
        {/key}
      {/if}
    </div>
  {/if}
</div>

<style>
  .stepfunctions { display: flex; flex-direction: column; min-height: 100%; }
  .layout {
    display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 28px; padding: 20px 0 48px;
    max-width: 1320px; align-items: start;
  }
  @media (max-width: 900px) {
    .layout { grid-template-columns: minmax(0, 1fr); }
  }

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 4px; font-size: 12px; color: var(--text-secondary); }

  .skeleton-list { display: flex; flex-direction: column; gap: 6px; }
  .skeleton-list span {
    height: 34px; border-radius: 8px; background: var(--bg-element);
    animation: pulse 1.4s ease-in-out infinite; animation-delay: calc(var(--i) * 80ms);
  }
  @keyframes pulse { 50% { opacity: 0.5; } }
  @keyframes fadeUp { from { opacity: 0; transform: translateY(6px); } }
  @media (prefers-reduced-motion: reduce) {
    .blank, .skeleton-list span { animation: none; }
  }
</style>
