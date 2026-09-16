<script lang="ts">
  import { CheckIcon, CopyIcon, ListBulletsIcon, StopIcon } from "phosphor-svelte";
  import RcButton from "$lib/components/rack/rc-button.svelte";
  import { stopECSTask } from "$lib/api";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcStat from "$lib/components/rack/rc-stat.svelte";
  import RcKv, { type KvItem } from "$lib/components/rack/rc-kv.svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import RcTonePill, { type Tone } from "$lib/components/rack/rc-tone-pill.svelte";
  import FunctionLogs from "$lib/components/functions/function-logs.svelte";
  import {
    clusterLabel,
    findTaskDefinition,
    isServiceTask,
    logGroupFor,
    serviceLabel,
    statusTone,
    tail,
    taskDefinitionLabel,
    taskLabel,
    tasksForService,
    type EcsSelection,
  } from "$lib/ecs";
  import { formatDate, timeAgo } from "$lib/utils";
  import type { ECSOverview, ECSTaskSummary } from "$lib/types";

  let {
    sel,
    ecs,
    onselect,
    onstopped,
  }: {
    sel: EcsSelection;
    ecs: ECSOverview;
    onselect: (sel: EcsSelection) => void;
    onstopped?: () => void | Promise<void>;
  } = $props();

  // Stopping: offered while the task is neither stopped nor already asked to
  // stop. Uses the same runner as the AWS StopTask API (stopCode UserInitiated).
  let confirmStop = $state(false);
  let stopReason = $state("");
  let stopping = $state(false);
  let stopError = $state("");
  const canStop = $derived(
    sel.kind === "task" &&
      sel.resource.lastStatus?.toUpperCase() !== "STOPPED" &&
      sel.resource.desiredStatus?.toUpperCase() !== "STOPPED",
  );

  async function stopTask() {
    if (sel.kind !== "task" || stopping) return;
    stopping = true;
    stopError = "";
    try {
      await stopECSTask({ cluster: sel.resource.clusterArn, task: sel.resource.arn, reason: stopReason.trim() || undefined });
      confirmStop = false;
      stopReason = "";
      await onstopped?.();
    } catch (error) {
      stopError = error instanceof Error ? error.message : String(error);
    } finally {
      stopping = false;
    }
  }

  const allTasks = $derived(ecs.tasks ?? []);

  const title = $derived(
    sel.kind === "cluster" ? clusterLabel(sel.resource) : sel.kind === "service" ? serviceLabel(sel.resource) : tail(sel.resource.arn),
  );
  const status = $derived(sel.kind === "task" ? sel.resource.lastStatus : sel.resource.status);
  const clusterName = $derived(sel.kind === "cluster" ? null : tail(sel.resource.clusterArn));
  const defArn = $derived(sel.kind === "cluster" ? undefined : sel.resource.taskDefinitionArn);
  const taskDef = $derived(findTaskDefinition(ecs, defArn));
  const logGroup = $derived(logGroupFor(ecs, defArn));
  const logsHref = $derived(logGroup ? `#logs?groups=${encodeURIComponent(logGroup)}` : null);

  const childTasks = $derived.by((): ECSTaskSummary[] => {
    if (sel.kind === "cluster") return allTasks.filter((t) => t.clusterArn === sel.resource.arn);
    if (sel.kind === "service") return tasksForService(allTasks, sel.resource);
    return [];
  });

  const clusterServices = $derived(
    sel.kind === "cluster" ? (ecs.services ?? []).filter((s) => s.clusterArn === sel.resource.arn) : [],
  );

  const parentService = $derived.by(() => {
    if (sel.kind !== "task" || !isServiceTask(sel.resource)) return null;
    const name = sel.resource.group!.slice("service:".length);
    const clusterArn = sel.resource.clusterArn;
    return (ecs.services ?? []).find((s) => s.name === name && s.clusterArn === clusterArn) ?? null;
  });

  const config = $derived.by((): KvItem[] => {
    const r = sel.resource;
    const items: KvItem[] = [];
    if (sel.kind === "service") {
      items.push({ label: "Launch type", value: sel.resource.launchType || "--", mono: true, dim: !sel.resource.launchType });
    }
    if (sel.kind === "task") {
      const t = sel.resource;
      items.push(
        { label: "Desired status", value: t.desiredStatus || "--", mono: true },
        { label: "Launch type", value: t.launchType || "--", mono: true, dim: !t.launchType },
        { label: "Group", value: t.group || "--", mono: true, dim: !t.group },
        { label: "Started", value: t.startedAt ? formatDate(t.startedAt) : "--" },
        { label: "Stopped", value: t.stoppedAt ? formatDate(t.stoppedAt) : "--", dim: !t.stoppedAt },
      );
    }
    if (sel.kind !== "cluster") items.push({ label: "Cluster ARN", value: sel.resource.clusterArn || "--", mono: true, dim: true });
    items.push({ label: "ARN", value: r.arn || "--", mono: true, dim: true });
    return items;
  });

  const defItems = $derived<KvItem[]>([
    { label: "Revision", value: taskDefinitionLabel(ecs, defArn), mono: true },
    { label: "Status", value: taskDef?.status || "--", mono: true, dim: !taskDef },
    { label: "Log group", value: logGroup ?? "--", mono: true },
    { label: "ARN", value: defArn || "--", mono: true, dim: true },
  ]);

  let copied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  async function copyArn() {
    await navigator.clipboard.writeText(sel.resource.arn);
    copied = true;
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => (copied = false), 1600);
  }

  function healthTone(health: string): Tone {
    const h = health.toUpperCase();
    return h === "HEALTHY" ? "green" : h === "UNHEALTHY" ? "red" : "neutral";
  }

  function taskSub(t: ECSTaskSummary): string {
    const when = t.stoppedAt ? `stopped ${timeAgo(t.stoppedAt)}` : t.startedAt ? `started ${timeAgo(t.startedAt)}` : "not started";
    return `${taskDefinitionLabel(ecs, t.taskDefinitionArn)} · ${when}`;
  }
</script>

<div class="detail">
  <header class="hero">
    <div class="identity">
      <div class="title-row">
        <h1 title={sel.resource.arn}>{title}</h1>
        <RcTonePill tone={statusTone(status)}>{(status || "unknown").toLowerCase()}</RcTonePill>
      </div>
      <p class="subline">
        <span>{sel.kind === "cluster" ? "ECS cluster" : sel.kind === "service" ? "ECS service" : "ECS task"}</span>
        {#if clusterName}<i></i><span>{clusterName}</span>{/if}
        {#if defArn}<i></i><span class="mono">{taskDefinitionLabel(ecs, defArn)}</span>{/if}
        {#if sel.kind === "task" && sel.resource.startedAt}<i></i><span>started {timeAgo(sel.resource.startedAt)}</span>{/if}
      </p>
    </div>
    <div class="hero-actions">
      {#if canStop && !confirmStop}
        <button type="button" class="btn danger" onclick={() => (confirmStop = true)}><StopIcon size={12} weight="fill" />Stop</button>
      {/if}
      {#if logsHref}<a class="btn" href={logsHref}><ListBulletsIcon size={12} />Logs</a>{/if}
      <button type="button" class="btn" onclick={copyArn}>
        {#if copied}<CheckIcon size={12} class="ok" />Copied{:else}<CopyIcon size={12} />ARN{/if}
      </button>
    </div>
  </header>

  {#if sel.kind === "task" && canStop && confirmStop}
    <div class="stop-confirm">
      <div class="stop-copy">
        <span class="stop-title">Stop this task?</span>
        <span class="stop-note">
          {#if parentService}
            Its containers get SIGTERM, then SIGKILL after their stop timeout. Service <code>{serviceLabel(parentService)}</code> will launch a replacement.
          {:else}
            Its containers get SIGTERM, then SIGKILL after their stop timeout.
          {/if}
        </span>
      </div>
      <div class="stop-controls">
        <input
          type="text"
          placeholder="Reason (optional)"
          aria-label="Stop reason"
          bind:value={stopReason}
          disabled={stopping}
          onkeydown={(e) => {
            if (e.key === "Enter") void stopTask();
            if (e.key === "Escape") confirmStop = false;
          }}
        />
        <RcButton variant="ghost" small disabled={stopping} onclick={() => { confirmStop = false; stopError = ""; }}>Keep</RcButton>
        <RcButton variant="danger" small disabled={stopping} onclick={stopTask}>
          <StopIcon size={11} weight="fill" />{stopping ? "Stopping…" : "Stop task"}
        </RcButton>
      </div>
      {#if stopError}<p class="stop-error">{stopError}</p>{/if}
    </div>
  {/if}

  <div class="stats">
    {#if sel.kind === "cluster"}
      <RcStat label="Running" value={sel.resource.runningTasks} sub="tasks" tone={sel.resource.runningTasks > 0 ? "green" : undefined} />
      <RcStat label="Pending" value={sel.resource.pendingTasks} sub="tasks" tone={sel.resource.pendingTasks > 0 ? "amber" : undefined} />
      <RcStat label="Services" value={sel.resource.activeServices} sub="active" />
      <RcStat label="Tasks" value={childTasks.length} sub="reported" />
    {:else if sel.kind === "service"}
      {@const s = sel.resource}
      <RcStat label="Desired" value={s.desiredCount} sub="replicas" />
      <RcStat label="Running" value={s.runningCount} sub="of {s.desiredCount}" tone={s.runningCount < s.desiredCount ? "amber" : s.runningCount > 0 ? "green" : undefined} />
      <RcStat label="Pending" value={s.pendingCount} sub="replicas" tone={s.pendingCount > 0 ? "amber" : undefined} />
      <RcStat label="Launch" value={s.launchType || "--"} sub="type" />
    {:else}
      {@const t = sel.resource}
      <RcStat label="Last status" value={t.lastStatus || "--"} tone={statusTone(t.lastStatus) === "neutral" ? undefined : (statusTone(t.lastStatus) as "green" | "amber" | "red")} />
      <RcStat label="Desired" value={t.desiredStatus || "--"} />
      <RcStat label="Started" value={t.startedAt ? timeAgo(t.startedAt) : "--"} sub={t.startedAt ? formatDate(t.startedAt) : ""} />
      <RcStat label="Launch" value={t.launchType || "--"} sub="type" />
    {/if}
  </div>

  {#if sel.kind === "task" && sel.resource.stoppedReason}
    <div class="reason">
      <span class="reason-label">Stopped reason</span>
      <p>{sel.resource.stoppedReason}</p>
    </div>
  {/if}

  {#if sel.kind === "task"}
    {@const containers = sel.resource.containers ?? []}
    <RcPanel title="Containers" description="Each container in this task, with its exit code and published ports." index={0}>
      {#if containers.length === 0}
        <p class="empty">No containers reported.</p>
      {:else}
        <div class="containers">
          {#each containers as c (c.name)}
            <div class="container">
              <div class="container-main">
                <span class="container-name mono" title={c.name}>{c.name}</span>
                <span class="container-sub">
                  {#if c.exitCode !== undefined && c.exitCode !== null}
                    <span class="mono" class:bad={c.exitCode !== 0}>exit {c.exitCode}</span>
                  {:else if c.lastStatus?.toUpperCase() === "STOPPED"}
                    <span>no exit code</span>
                  {/if}
                  {#each c.networkBindings ?? [] as nb (`${nb.hostPort}/${nb.protocol ?? "tcp"}`)}
                    <a
                      class="port mono"
                      href="http://127.0.0.1:{nb.hostPort}"
                      target="_blank"
                      rel="noreferrer"
                      title="Container port {nb.containerPort} published on 127.0.0.1:{nb.hostPort}"
                    >{nb.containerPort} → 127.0.0.1:{nb.hostPort}</a>
                  {/each}
                </span>
                {#if c.reason && c.reason !== sel.resource.stoppedReason}<p class="container-reason">{c.reason}</p>{/if}
              </div>
              <div class="container-pills">
                {#if c.healthStatus}
                  <RcTonePill tone={healthTone(c.healthStatus)}>{c.healthStatus.toLowerCase()}</RcTonePill>
                {/if}
                <RcTonePill tone={statusTone(c.lastStatus)}>{(c.lastStatus || "unknown").toLowerCase()}</RcTonePill>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </RcPanel>
  {/if}

  {#if sel.kind === "task" && parentService}
    {@const svc = parentService}
    <RcPanel title="Service" description="This task is managed by a service." index={1}>
      <RcListRow
        mono
        title={serviceLabel(svc)}
        sub="{svc.runningCount}/{svc.desiredCount} running · {taskDefinitionLabel(ecs, svc.taskDefinitionArn)}"
        onclick={() => onselect({ kind: "service", resource: svc })}
      />
    </RcPanel>
  {/if}

  {#if sel.kind === "cluster"}
    <RcPanel title="Services" description="Services scheduled on this cluster." index={0}>
      {#if clusterServices.length === 0}
        <p class="empty">No services on this cluster.</p>
      {:else}
        <div class="rows">
          {#each clusterServices as s (s.arn)}
            <RcListRow
              mono
              title={serviceLabel(s)}
              sub={taskDefinitionLabel(ecs, s.taskDefinitionArn)}
              onclick={() => onselect({ kind: "service", resource: s })}
            >
              {#snippet trailing()}
                <span class="count" class:warn={s.runningCount < s.desiredCount}>{s.runningCount}/{s.desiredCount}</span>
              {/snippet}
            </RcListRow>
          {/each}
        </div>
      {/if}
    </RcPanel>
  {/if}

  {#if sel.kind !== "task"}
    <RcPanel
      title="Tasks"
      description={sel.kind === "service" ? "Tasks launched by this service." : "Every task on this cluster."}
      index={1}
    >
      {#if childTasks.length === 0}
        <p class="empty">No tasks reported.</p>
      {:else}
        <div class="rows">
          {#each childTasks as t (t.arn)}
            {@const tone = statusTone(t.lastStatus)}
            <RcListRow mono title={taskLabel(t)} sub={taskSub(t)} onclick={() => onselect({ kind: "task", resource: t })}>
              {#snippet trailing()}
                <RcTonePill {tone}>{t.lastStatus.toLowerCase()}</RcTonePill>
              {/snippet}
            </RcListRow>
          {/each}
        </div>
      {/if}
    </RcPanel>
  {/if}

  {#if logGroup}
    <RcPanel title="Latest logs" index={2}>
      {#snippet actions()}
        <a class="btn ghost" href={logsHref}>Open in Logs</a>
      {/snippet}
      <FunctionLogs {logGroup} revision={allTasks.length} />
    </RcPanel>
  {/if}

  <div class="split" class:single={!defArn}>
    {#if defArn}
      <RcPanel title="Task definition" index={3}>
        <RcKv items={defItems} />
      </RcPanel>
    {/if}
    <RcPanel title="Configuration" index={4}>
      <RcKv items={config} />
    </RcPanel>
  </div>
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
  .mono { font-family: var(--font-mono, ui-monospace, monospace); }
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
  .btn.danger { color: var(--accent-red); border-color: color-mix(in srgb, var(--accent-red) 35%, transparent); }
  .btn.danger:hover { color: var(--accent-red); border-color: var(--accent-red); background: color-mix(in srgb, var(--accent-red) 10%, transparent); }

  .stop-confirm {
    display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 10px 16px;
    padding: 10px 12px 10px 18px; position: relative; border-radius: 12px;
    border: 1px solid color-mix(in srgb, var(--accent-red) 35%, transparent);
    background: color-mix(in srgb, var(--accent-red) 6%, transparent);
    animation: stopIn 220ms var(--ease-snappy) both;
  }
  .stop-confirm::before {
    content: ""; position: absolute; left: 6px; top: 10px; bottom: 10px; width: 2.5px; border-radius: 2px; background: var(--accent-red);
  }
  @keyframes stopIn { from { opacity: 0; transform: translateY(-3px); } }
  .stop-copy { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .stop-title { font-size: 12.5px; font-weight: 600; color: var(--text-primary); }
  .stop-note { font-size: 11.5px; color: var(--text-secondary); }
  .stop-note code { font-family: var(--font-mono, ui-monospace, monospace); font-size: 11px; }
  .stop-controls { display: flex; align-items: center; gap: 6px; }
  .stop-controls input {
    width: 14rem; height: 24px; border-radius: 8px; border: 1px solid var(--border-subtle);
    background: var(--bg-app); color: var(--text-primary); font-size: 11.5px; padding: 0 9px; outline: none;
  }
  .stop-controls input:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 1px; }
  .stop-error { flex-basis: 100%; font-size: 11.5px; color: var(--accent-red); }
  .btn:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }

  .stats {
    display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; padding: 14px 2px 10px;
    animation: statsIn 320ms var(--ease-snappy) both;
  }
  @keyframes statsIn { from { opacity: 0; transform: translateY(4px); } }
  @media (max-width: 1100px) { .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); } }

  .reason {
    position: relative; padding: 10px 14px 10px 18px; border-radius: 8px;
    background: color-mix(in srgb, var(--accent-amber) 6%, transparent);
  }
  .reason::before {
    content: ""; position: absolute; left: 6px; top: 9px; bottom: 9px; width: 2.5px; border-radius: 2px; background: var(--accent-amber);
  }
  .reason-label { font-size: 10.5px; letter-spacing: 0.06em; text-transform: uppercase; color: var(--accent-amber); }
  .reason p { margin-top: 2px; font-size: 12px; color: var(--text-secondary); }

  .rows { display: flex; flex-direction: column; gap: 2px; max-height: 360px; overflow-y: auto; }

  .containers { display: flex; flex-direction: column; gap: 2px; }
  .container {
    display: flex; align-items: flex-start; justify-content: space-between; gap: 12px;
    padding: 8px 10px; border-radius: 8px; transition: background 120ms ease;
  }
  .container:hover { background: var(--bg-element-hover); }
  .container-main { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .container-name { font-size: 12px; color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .container-sub { display: flex; align-items: center; flex-wrap: wrap; gap: 4px 10px; font-size: 10.5px; color: var(--text-tertiary); }
  .container-sub .bad { color: var(--accent-red); }
  .port {
    color: var(--text-secondary); text-decoration: none; border-radius: 4px;
    transition: color 120ms ease;
  }
  .port:hover { color: var(--text-primary); text-decoration: underline; text-underline-offset: 2px; }
  .port:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }
  .container-reason { margin-top: 2px; font-size: 11.5px; color: var(--text-secondary); word-break: break-word; }
  .container-pills { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }
  @media (prefers-reduced-motion: reduce) { .container { transition: none; } }
  .count { font: 10.5px var(--font-mono, ui-monospace, monospace); font-variant-numeric: tabular-nums; color: var(--text-tertiary); }
  .count.warn { color: var(--accent-amber); }
  .empty { font-size: 11.5px; color: var(--text-tertiary); }

  .split { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
  .split.single { grid-template-columns: minmax(0, 1fr); }
  @media (max-width: 1100px) { .split { grid-template-columns: minmax(0, 1fr); } }

  @media (prefers-reduced-motion: reduce) { .stats, .stop-confirm { animation: none; } }
</style>
