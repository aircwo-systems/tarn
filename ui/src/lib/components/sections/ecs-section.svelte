<script lang="ts">
  import { onMount } from "svelte";
  import { PlayIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import EcsList from "$lib/components/ecs/ecs-list.svelte";
  import EcsDetail from "$lib/components/ecs/ecs-detail.svelte";
  import EcsRunTaskDialog from "$lib/components/ecs/ecs-run-task-dialog.svelte";
  import { getDashboard, refresh } from "$lib/state.svelte";
  import { ecsKey, type EcsSelection } from "$lib/ecs";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const ecs = $derived(dashboard.data?.ecs);
  const clusters = $derived(ecs?.clusters ?? []);
  const services = $derived(ecs?.services ?? []);
  const tasks = $derived(ecs?.tasks ?? []);
  const taskDefinitions = $derived(ecs?.taskDefinitions ?? []);

  // Keyed by kind + ARN: polling replaces the objects, so holding one would freeze the panel.
  let selectedKey = $state<string | null>(null);
  const selected = $derived.by((): EcsSelection | null => {
    const match = (kind: EcsSelection["kind"], arn: string) => ecsKey(kind, arn) === selectedKey;
    const cluster = clusters.find((c) => match("cluster", c.arn));
    if (cluster) return { kind: "cluster", resource: cluster };
    const service = services.find((s) => match("service", s.arn));
    if (service) return { kind: "service", resource: service };
    const task = tasks.find((t) => match("task", t.arn));
    if (task) return { kind: "task", resource: task };
    return clusters[0] ? { kind: "cluster", resource: clusters[0] } : null;
  });

  const runningTasks = $derived(tasks.filter((t) => t.lastStatus?.toUpperCase() === "RUNNING").length);

  function select(sel: EcsSelection) {
    selectedKey = ecsKey(sel.kind, sel.resource.arn);
    history.replaceState(null, "", `#ecs?sel=${encodeURIComponent(selectedKey)}`);
  }

  // Run-task dialog presets follow the current selection: a selected cluster
  // (or service/task) pre-fills its cluster, and a service/task pre-fills
  // its task definition for one-click re-runs.
  let runOpen = $state(false);
  const runClusterArn = $derived.by((): string | null => {
    if (!selected) return null;
    if (selected.kind === "cluster") return selected.resource.arn;
    return selected.resource.clusterArn ?? null;
  });
  const runTaskDefArn = $derived.by((): string | null => {
    if (!selected || selected.kind === "cluster") return null;
    return selected.resource.taskDefinitionArn ?? null;
  });

  async function handleLaunched(taskArns: string[]) {
    runOpen = false;
    await refresh();
    const launched = (dashboard.data?.ecs?.tasks ?? []).find((t) => taskArns.includes(t.arn));
    if (launched) select({ kind: "task", resource: launched });
  }

  onMount(() => {
    const qs = window.location.hash.split("?")[1];
    const sel = qs ? new URLSearchParams(qs).get("sel") : null;
    if (sel) selectedKey = sel;
  });
</script>

<div class="ecs">
  <SectionHeader
    title="ECS"
    description="{clusters.length} clusters · {services.length} services · {runningTasks} running tasks"
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      {#if clusters.length > 0 && taskDefinitions.length > 0}
        <button type="button" class="run-btn" onclick={() => (runOpen = true)}>
          <PlayIcon size={12} weight="fill" />Run task
        </button>
      {/if}
    {/snippet}
  </SectionHeader>

  {#if runOpen}
    <EcsRunTaskDialog
      {clusters}
      {taskDefinitions}
      initialClusterArn={runClusterArn}
      initialTaskDefinitionArn={runTaskDefArn}
      onclose={() => (runOpen = false)}
      onlaunched={(arns) => void handleLaunched(arns)}
    />
  {/if}

  {#if dashboard.loading && !dashboard.data}
    <div class="layout">
      <div class="skeleton-list">
        {#each Array(6) as _, i (i)}<span style:--i={i}></span>{/each}
      </div>
    </div>
  {:else if !ecs || clusters.length === 0}
    <div class="blank">
      <h2>No ECS clusters yet</h2>
      <p>Create one with <code>aws ecs create-cluster</code> or deploy through your IaC, and it appears here.</p>
    </div>
  {:else}
    <div class="layout">
      <aside class="list">
        <EcsList {ecs} selectedKey={selected ? ecsKey(selected.kind, selected.resource.arn) : null} onselect={select} />
      </aside>
      {#if selected}
        {#key ecsKey(selected.kind, selected.resource.arn)}
          <EcsDetail sel={selected} {ecs} onselect={select} />
        {/key}
      {/if}
    </div>
  {/if}
</div>

<style>
  .ecs { display: flex; flex-direction: column; min-height: 100%; }
  .run-btn {
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 11px; border-radius: 8px;
    border: 1px solid color-mix(in srgb, var(--accent-green) 40%, transparent);
    font-size: 11.5px; color: var(--accent-green);
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .run-btn:hover { background: color-mix(in srgb, var(--accent-green) 10%, transparent); }
  .run-btn:active { transform: scale(0.96); }
  .run-btn:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }
  .layout {
    display: grid; grid-template-columns: 260px minmax(0, 1fr); gap: 28px; padding: 20px 0 48px;
    max-width: 1320px;
  }
  .list { position: sticky; top: 0; align-self: start; max-height: calc(100vh - 140px); overflow-y: auto; padding-right: 2px; }
  @media (max-width: 900px) {
    .layout { grid-template-columns: minmax(0, 1fr); }
    .list { position: static; max-height: 260px; }
  }

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 4px; font-size: 12px; color: var(--text-secondary); }
  .blank code { font-family: var(--font-mono, ui-monospace, monospace); font-size: 11.5px; color: var(--text-primary); }

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
