<script lang="ts">
  import { MagnifyingGlassIcon } from "phosphor-svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import {
    clusterLabel,
    ecsKey,
    isServiceTask,
    serviceLabel,
    statusTone,
    taskDefinitionLabel,
    taskLabel,
    type EcsSelection,
  } from "$lib/ecs";
  import type { ECSOverview, ECSTaskSummary } from "$lib/types";

  let {
    ecs,
    selectedKey,
    onselect,
  }: {
    ecs: ECSOverview;
    selectedKey: string | null;
    onselect: (sel: EcsSelection) => void;
  } = $props();

  let query = $state("");

  const groups = $derived.by(() => {
    const q = query.trim().toLowerCase();
    const hit = (...values: (string | undefined)[]) => !q || values.some((v) => v?.toLowerCase().includes(q));
    const services = ecs.services ?? [];
    const tasks = ecs.tasks ?? [];

    return (ecs.clusters ?? []).map((cluster) => {
      const clusterHit = hit(clusterLabel(cluster));
      return {
        cluster,
        show: clusterHit,
        services: services.filter(
          (s) => s.clusterArn === cluster.arn && (clusterHit || hit(serviceLabel(s), taskDefinitionLabel(ecs, s.taskDefinitionArn))),
        ),
        // Service-owned tasks are reached through their service; list only standalone runs here.
        tasks: tasks.filter(
          (t) => t.clusterArn === cluster.arn && !isServiceTask(t) && (clusterHit || hit(t.arn, t.group, taskDefinitionLabel(ecs, t.taskDefinitionArn))),
        ),
      };
    }).filter((g) => g.show || g.services.length > 0 || g.tasks.length > 0);
  });

  const flat = $derived<EcsSelection[]>(
    groups.flatMap((g) => [
      { kind: "cluster" as const, resource: g.cluster },
      ...g.services.map((s) => ({ kind: "service" as const, resource: s })),
      ...g.tasks.map((t) => ({ kind: "task" as const, resource: t })),
    ]),
  );

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    if (flat.length === 0) return;
    e.preventDefault();
    const idx = flat.findIndex((s) => ecsKey(s.kind, s.resource.arn) === selectedKey);
    const next = e.key === "ArrowDown" ? Math.min(flat.length - 1, idx + 1) : Math.max(0, idx - 1);
    onselect(flat[next]);
  }

  function taskSub(t: ECSTaskSummary): string {
    return taskDefinitionLabel(ecs, t.taskDefinitionArn);
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="ecs-list" onkeydown={onKeydown}>
  <label class="search">
    <MagnifyingGlassIcon size={12} />
    <input placeholder="Filter clusters, services, tasks" bind:value={query} aria-label="Filter ECS resources" />
  </label>

  {#each groups as g (g.cluster.arn)}
    <div class="group">
      <RcListRow
        title={clusterLabel(g.cluster)}
        sub="{g.cluster.runningTasks} running · {g.cluster.activeServices} services"
        selected={ecsKey("cluster", g.cluster.arn) === selectedKey}
        onclick={() => onselect({ kind: "cluster", resource: g.cluster })}
      >
        {#snippet trailing()}
          {#if g.cluster.pendingTasks > 0}<span class="tone" data-tone="amber">{g.cluster.pendingTasks} pending</span>{/if}
        {/snippet}
      </RcListRow>

      {#if g.services.length > 0 || g.tasks.length > 0}
        <div class="children">
          {#if g.services.length > 0}<span class="kind">Services</span>{/if}
          {#each g.services as s (s.arn)}
            {@const tone = s.runningCount < s.desiredCount ? "amber" : statusTone(s.status)}
            <RcListRow
              mono
              title={serviceLabel(s)}
              sub={taskDefinitionLabel(ecs, s.taskDefinitionArn)}
              selected={ecsKey("service", s.arn) === selectedKey}
              onclick={() => onselect({ kind: "service", resource: s })}
            >
              {#snippet trailing()}
                <span class="replicas" data-tone={tone} title="{s.runningCount} running of {s.desiredCount} desired">
                  {s.runningCount}/{s.desiredCount}
                </span>
              {/snippet}
            </RcListRow>
          {/each}

          {#if g.tasks.length > 0}<span class="kind">Tasks</span>{/if}
          {#each g.tasks as t (t.arn)}
            {@const tone = statusTone(t.lastStatus)}
            <RcListRow
              mono
              title={taskLabel(t)}
              sub={taskSub(t)}
              selected={ecsKey("task", t.arn) === selectedKey}
              onclick={() => onselect({ kind: "task", resource: t })}
            >
              {#snippet trailing()}
                {#if tone !== "green"}<span class="tone" data-tone={tone}>{t.lastStatus.toLowerCase()}</span>{/if}
              {/snippet}
            </RcListRow>
          {/each}
        </div>
      {/if}
    </div>
  {:else}
    <p class="none">{query ? `No match for “${query}”` : "No clusters reported."}</p>
  {/each}
</div>

<style>
  .ecs-list { display: flex; flex-direction: column; gap: 10px; min-height: 0; }
  .search {
    display: flex; align-items: center; gap: 7px; height: 30px; padding: 0 10px; border-radius: 8px;
    border: 1px solid var(--border-subtle); background: var(--bg-app); color: var(--text-tertiary);
    transition: border-color 120ms ease;
  }
  .search:hover { border-color: var(--border-default); }
  .search:focus-within { border-color: var(--border-focus); }
  .search input { flex: 1; min-width: 0; background: transparent; border: 0; outline: none; font-size: 12px; color: var(--text-primary); }
  .search input::placeholder { color: var(--text-tertiary); }

  .group { display: flex; flex-direction: column; gap: 2px; }
  .children {
    display: flex; flex-direction: column; gap: 2px; margin-left: 10px; padding-left: 8px;
    border-left: 1px solid var(--border-subtle);
  }
  .kind {
    padding: 8px 10px 3px; font-size: 10px; letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-tertiary);
  }

  .replicas { font: 10.5px var(--font-mono, ui-monospace, monospace); font-variant-numeric: tabular-nums; color: var(--text-tertiary); }
  .replicas[data-tone="amber"] { color: var(--accent-amber); }
  .replicas[data-tone="red"] { color: var(--accent-red); }
  .tone { font-size: 10px; padding: 1px 6px; border-radius: 6px; color: var(--text-tertiary); background: var(--bg-element); }
  .tone[data-tone="amber"] { color: var(--accent-amber); background: color-mix(in srgb, var(--accent-amber) 10%, transparent); }
  .tone[data-tone="red"] { color: var(--accent-red); background: color-mix(in srgb, var(--accent-red) 10%, transparent); }
  .none { padding: 12px 10px; font-size: 11.5px; color: var(--text-tertiary); }
</style>
