<script lang="ts">
  import { matchesResourceType } from "$lib/filter-utils";
  import { onMount } from "svelte";
  import { MagnifyingGlassIcon, SidebarSimpleIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import StateMachineList from "$lib/components/stepfunctions/state-machine-list.svelte";
  import StateMachineDetail from "$lib/components/stepfunctions/state-machine-detail.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import { getDashboard, getDashboardFilters } from "$lib/state.svelte";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const machines = $derived(matchesResourceType("stepfunctions", filters.tagFilter) ? dashboard.data?.stateMachines ?? [] : []);
  const totalExecutions = $derived(
    machines.reduce((sum, machine) => sum + (machine.executions?.length ?? 0), 0),
  );

  let listCollapsed = $state(false);

  function toggleListCollapse() {
    listCollapsed = !listCollapsed;
    try {
      localStorage.setItem("tarn-stepfunctions-list-collapsed", String(listCollapsed));
    } catch {}
  }

  let query = $state("");
  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return machines;
    return machines.filter((machine) =>
      `${machine.name} ${machine.type} ${machine.status}`.toLowerCase().includes(q),
    );
  });

  // Keyed by arn: polling replaces the objects, so holding one would freeze the panel.
  let selectedArn = $state<string | null>(null);
  const selectedMachine = $derived(
    machines.find((machine) => machine.arn === selectedArn) ?? machines[0] ?? null,
  );

  function select(arn: string) {
    selectedArn = arn;
    history.replaceState(null, "", `#stepfunctions?machine=${encodeURIComponent(arn)}`);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((machine) => machine.arn === selectedMachine?.arn);
    const forward = e.key === "ArrowDown" || e.key === "ArrowRight";
    const next = forward ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    select(visible[next].arn);
  }

  onMount(() => {
    const qs = window.location.hash.split("?")[1];
    const machine = qs ? new URLSearchParams(qs).get("machine") : null;
    if (machine) selectedArn = machine;
    try {
      const saved = localStorage.getItem("tarn-stepfunctions-list-collapsed");
      if (saved !== null) {
        listCollapsed = saved === "true";
      }
    } catch {}
  });
</script>

<div class="stepfunctions">
  <SectionHeader
    title="Step Functions"
    description="{machines.length} state machine{machines.length === 1 ? '' : 's'} · {totalExecutions} executions"
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      <div class="header-filter">
        <MagnifyingGlassIcon size={12} />
        <input
          placeholder="Filter state machines..."
          bind:value={query}
          aria-label="Filter state machines"
        />
        {#if query}
          <button
            type="button"
            class="clear-query-btn"
            onclick={() => (query = "")}
            aria-label="Clear filter"
          >
            &times;
          </button>
        {/if}
      </div>
    {/snippet}
  </SectionHeader>

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
    <div class="layout" class:list-collapsed={listCollapsed}>
      {#if listCollapsed}
        <div class="list-toolbar" role="toolbar" aria-label="State machines overview">
          <div class="toolbar-leading">
            <button
              type="button"
              class="expand-list-btn"
              onclick={toggleListCollapse}
              title="Expand state machine list"
              aria-label="Expand state machine list"
            >
              <SidebarSimpleIcon size={13} weight="fill" />
              <span class="expand-label">State machines</span>
              <span class="count-badge">{machines.length}</span>
            </button>

            {#if sidebarCollapsed}
              <span class="toolbar-divider" aria-hidden="true"></span>
              <div class="toolbar-stats">
                <span class="toolbar-stat" title="{totalExecutions} executions">
                  <span class="status-dot green"></span>
                  <span>{totalExecutions} executions</span>
                </span>
              </div>
            {/if}
          </div>

          <div class="toolbar-trailing">
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="chips-row" role="tablist" tabindex="0" aria-label="State machine switcher" onkeydown={onKeydown}>
              {#each visible as machine (machine.arn)}
                <button
                  type="button"
                  role="tab"
                  class="item-chip"
                  class:selected={machine.arn === selectedMachine?.arn}
                  aria-selected={machine.arn === selectedMachine?.arn}
                  onclick={() => select(machine.arn)}
                  title="{machine.name} ({machine.type} · {machine.status})"
                >
                  <span
                    class="chip-dot"
                    style:background={machine.status === "ACTIVE" ? "var(--accent-green, #10b981)" : machine.status === "DELETING" ? "var(--accent-amber, #f59e0b)" : "var(--accent-blue, #3b82f6)"}
                  ></span>
                  <span class="chip-name">{machine.name}</span>
                  <span class="chip-badge">{machine.executions?.length ?? 0} exec</span>
                </button>
              {:else}
                <span class="chips-none">No match for "{query}"</span>
              {/each}
            </div>
          </div>
        </div>
      {/if}

      {#if !listCollapsed}
        <RcResizableAside
          storageKey="tarn-stepfunctions-list-width"
          collapsible={true}
          onToggleCollapse={toggleListCollapse}
        >
          <StateMachineList
            {machines}
            selectedArn={selectedMachine?.arn ?? null}
            onselect={select}
            bind:query
            onToggleCollapse={toggleListCollapse}
          />
        </RcResizableAside>
      {/if}

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
