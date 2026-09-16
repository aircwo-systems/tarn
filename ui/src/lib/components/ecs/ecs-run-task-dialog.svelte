<script lang="ts">
  import { onMount } from "svelte";
  import { PlayIcon, XIcon } from "phosphor-svelte";
  import { describeECSTaskDefinition, runECSTask } from "$lib/api";
  import type {
    ECSClusterSummary,
    ECSRunTaskResult,
    ECSTaskDefinitionDetail,
    ECSTaskDefinitionSummary,
  } from "$lib/types";

  let {
    clusters,
    taskDefinitions,
    initialClusterArn = null,
    initialTaskDefinitionArn = null,
    onclose,
    onlaunched,
  }: {
    clusters: ECSClusterSummary[];
    taskDefinitions: ECSTaskDefinitionSummary[];
    initialClusterArn?: string | null;
    initialTaskDefinitionArn?: string | null;
    onclose: () => void;
    onlaunched: (taskArns: string[]) => void;
  } = $props();

  let clusterArn = $state("");
  let taskDefArn = $state("");
  let count = $state(1);
  let launchType = $state("FARGATE");
  let containerName = $state("");
  let envText = $state("");
  let commandText = $state("");

  // Snapshot the open-time presets once: the dialog remounts fresh on every
  // open, and afterwards these are user-editable draft state.
  onMount(() => {
    clusterArn = initialClusterArn ?? clusters[0]?.arn ?? "";
    taskDefArn =
      initialTaskDefinitionArn ??
      taskDefinitions[taskDefinitions.length - 1]?.taskDefinitionArn ??
      "";
  });

  let detail = $state<ECSTaskDefinitionDetail | null>(null);
  let detailLoading = $state(false);
  let detailToken = 0;

  let busy = $state(false);
  let status = $state("");
  let statusTone: "ok" | "err" = $state("ok");
  let result = $state<ECSRunTaskResult | null>(null);

  // Like AWS, RunTask returns a task whose launch failed (a missing secret, a
  // bad image) as a STOPPED task with stopCode TaskFailedToStart, not as an
  // entry in failures — so both count as failures here.
  const failedToStart = (r: ECSRunTaskResult) => r.tasks.filter((t) => t.stopCode === "TaskFailedToStart");
  const startedTasks = (r: ECSRunTaskResult) => r.tasks.filter((t) => t.stopCode !== "TaskFailedToStart");

  const selectedDef = $derived(
    taskDefinitions.find((d) => d.taskDefinitionArn === taskDefArn) ?? null,
  );

  $effect(() => {
    const family = selectedDef?.family ?? "";
    if (!family) {
      detail = null;
      return;
    }
    const token = ++detailToken;
    detailLoading = true;
    describeECSTaskDefinition(family)
      .then((d) => {
        if (token !== detailToken) return;
        detail = d;
        const essential = d.containers.find((c) => c.essential) ?? d.containers[0];
        containerName = essential?.name ?? "";
      })
      .catch(() => {
        if (token !== detailToken) return;
        detail = null;
        containerName = "";
      })
      .finally(() => {
        if (token === detailToken) detailLoading = false;
      });
  });

  function parseEnv(text: string): { name: string; value: string }[] {
    const out: { name: string; value: string }[] = [];
    for (const raw of text.split("\n")) {
      const line = raw.trim();
      if (!line || line.startsWith("#")) continue;
      const idx = line.indexOf("=");
      if (idx <= 0) throw new Error(`Bad environment line (want KEY=value): ${line}`);
      const name = line.slice(0, idx).trim();
      if (!name) throw new Error(`Bad environment line (want KEY=value): ${line}`);
      out.push({ name, value: line.slice(idx + 1) });
    }
    return out;
  }

  async function run() {
    if (busy || !taskDefArn || !clusterArn) return;
    busy = true;
    status = "";
    result = null;
    try {
      const environment = parseEnv(envText);
      const command = commandText.trim() ? commandText.trim().split(/\s+/) : undefined;
      const payload = await runECSTask({
        cluster: clusterArn,
        taskDefinition: taskDefArn,
        count,
        launchType,
        overrides:
          environment.length > 0 || command
            ? { containerOverrides: [{ name: containerName || detail?.containers[0]?.name || "", command, environment }] }
            : undefined,
      });
      result = payload;
      const arns = startedTasks(payload).map((t) => t.taskArn);
      const failureCount = failedToStart(payload).length + (payload.failures?.length ?? 0);
      if (arns.length > 0) {
        statusTone = "ok";
        status =
          failureCount > 0
            ? `Launched ${arns.length} task${arns.length === 1 ? "" : "s"} with ${failureCount} failure${failureCount === 1 ? "" : "s"}.`
            : `Launched ${arns.length} task${arns.length === 1 ? "" : "s"}.`;
        onlaunched(arns);
      } else {
        statusTone = "err";
        status = "No tasks launched — see failures below.";
      }
    } catch (error) {
      statusTone = "err";
      status = error instanceof Error ? error.message : "Failed to run task";
    } finally {
      busy = false;
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") onclose();
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="overlay" onclick={onclose} aria-hidden="true"></div>
<div
  role="dialog"
  aria-modal="true"
  aria-label="Run ECS task"
  class="dialog"
  tabindex="-1"
  onkeydown={onKeydown}
>
  <div class="dialog-head">
    <h2>Run ECS task</h2>
    <button type="button" onclick={onclose} class="icon-btn" aria-label="Close run task dialog">
      <XIcon size={14} />
    </button>
  </div>

  <div class="dialog-body">
    {#if clusters.length === 0 || taskDefinitions.length === 0}
      <p class="empty">Need at least one cluster and one active task definition to launch a task.</p>
    {:else}
      <label class="field">
        <span class="field-label">Cluster</span>
        <select bind:value={clusterArn} disabled={busy}>
          {#each clusters as c (c.arn)}
            <option value={c.arn}>{c.name}</option>
          {/each}
        </select>
      </label>

      <label class="field">
        <span class="field-label">Task definition</span>
        <select bind:value={taskDefArn} disabled={busy}>
          {#each taskDefinitions as d (d.taskDefinitionArn)}
            <option value={d.taskDefinitionArn}>
              {d.family}:{d.revision}{d.status !== "ACTIVE" ? ` (${d.status})` : ""}
            </option>
          {/each}
        </select>
      </label>

      <div class="row">
        <label class="field">
          <span class="field-label">Count</span>
          <input type="number" min="1" max="10" step="1" bind:value={count} disabled={busy} />
        </label>
        <label class="field">
          <span class="field-label">Launch type</span>
          <select bind:value={launchType} disabled={busy}>
            <option value="FARGATE">FARGATE</option>
            <option value="EC2">EC2</option>
          </select>
        </label>
      </div>

      <label class="field">
        <span class="field-label">Container for overrides{detailLoading ? " (loading…)" : ""}</span>
        <select bind:value={containerName} disabled={busy || detailLoading || !detail}>
          {#if detail}
            {#each detail.containers as c (c.name)}
              <option value={c.name}>{c.name} — {c.image}</option>
            {/each}
          {:else}
            <option value="">Select a task definition first</option>
          {/if}
        </select>
      </label>

      <label class="field">
        <span class="field-label">Environment overrides <span class="dim">one KEY=value per line, optional</span></span>
        <textarea
          rows="3"
          spellcheck={false}
          placeholder={"RUN_TAG=demo\nLOG_LEVEL=debug"}
          bind:value={envText}
          disabled={busy}
        ></textarea>
      </label>

      <label class="field">
        <span class="field-label">Command override <span class="dim">optional, space-separated</span></span>
        <input
          type="text"
          spellcheck={false}
          placeholder="sh -c 'echo hello'"
          bind:value={commandText}
          disabled={busy}
        />
      </label>
    {/if}

    {#if status}
      <p class="status" class:err={statusTone === "err"}>{status}</p>
    {/if}

    {#if result && (failedToStart(result).length > 0 || (result.failures?.length ?? 0) > 0)}
      <div class="failures">
        {#each failedToStart(result) as t (t.taskArn)}
          <p class="failure" title={t.taskArn}><span>Task failed to start</span>{t.stoppedReason || t.taskArn}</p>
        {/each}
        {#each result.failures ?? [] as f (f.arn ?? f.reason)}
          <p class="failure"><span>{f.reason ?? "Failed"}</span>{f.detail ?? f.arn ?? ""}</p>
        {/each}
      </div>
    {/if}

    {#if result && startedTasks(result).length > 0}
      <div class="launched">
        {#each startedTasks(result) as t (t.taskArn)}
          <p class="task-arn" title={t.taskArn}>{t.taskArn} · {t.lastStatus.toLowerCase()}</p>
        {/each}
      </div>
    {/if}
  </div>

  <div class="dialog-foot">
    <button type="button" class="btn ghost" onclick={onclose}>Close</button>
    <button
      type="button"
      class="btn primary"
      onclick={run}
      disabled={busy || !clusterArn || !taskDefArn}
    >
      <PlayIcon size={12} weight="fill" />{busy ? "Launching…" : "Run task"}
    </button>
  </div>
</div>

<style>
  .overlay {
    position: fixed; inset: 0; z-index: 70; background: rgb(0 0 0 / 0.45);
  }
  .dialog {
    position: fixed; z-index: 75; left: 50%; top: 50%; transform: translate(-50%, -50%);
    width: min(34rem, 92vw); max-height: 86vh; display: flex; flex-direction: column;
    border-radius: 12px; border: 1px solid var(--border-default);
    background: var(--bg-app); box-shadow: 0 24px 64px rgb(0 0 0 / 0.35);
  }
  .dialog-head {
    display: flex; align-items: center; justify-content: space-between;
    padding: 12px 16px; border-bottom: 1px solid var(--border-subtle);
  }
  .dialog-head h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .icon-btn {
    display: inline-flex; align-items: center; justify-content: center;
    height: 28px; width: 28px; border-radius: 8px; color: var(--text-tertiary);
    transition: color 120ms ease, background 120ms ease;
  }
  .icon-btn:hover { color: var(--text-primary); background: var(--bg-element-hover); }
  .dialog-body { display: flex; flex-direction: column; gap: 12px; padding: 16px; overflow-y: auto; }
  .dialog-foot {
    display: flex; align-items: center; justify-content: flex-end; gap: 8px;
    padding: 12px 16px; border-top: 1px solid var(--border-subtle);
  }
  .field { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .row { display: flex; gap: 12px; }
  .row .field { flex: 1; }
  .field-label { font-size: 11.5px; color: var(--text-secondary); }
  .field-label .dim { color: var(--text-tertiary); }
  select, input[type="number"], input[type="text"], textarea {
    border-radius: 8px; border: 1px solid var(--border-subtle);
    background: var(--bg-app); color: var(--text-primary); font-size: 12px;
    padding: 7px 9px; outline: none; width: 100%;
  }
  textarea { font-family: var(--font-mono, ui-monospace, monospace); font-size: 11.5px; resize: vertical; }
  select:focus-visible, input:focus-visible, textarea:focus-visible {
    outline: 1px solid var(--border-focus); outline-offset: 1px;
  }
  .empty { font-size: 12px; color: var(--text-tertiary); }
  .status { font-size: 11.5px; color: var(--text-secondary); }
  .status.err { color: var(--accent-red); }
  .failures { display: flex; flex-direction: column; gap: 6px; }
  .failure {
    display: flex; flex-direction: column; gap: 2px; font-size: 11.5px; color: var(--accent-red);
    border: 1px solid color-mix(in srgb, var(--accent-red) 35%, transparent);
    border-radius: 8px; padding: 8px 10px; word-break: break-word;
  }
  .failure span { font-weight: 600; }
  .launched { display: flex; flex-direction: column; gap: 4px; }
  .task-arn {
    font-family: var(--font-mono, ui-monospace, monospace); font-size: 11px; color: var(--text-secondary);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .btn {
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 11px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11.5px; color: var(--text-secondary);
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .btn:hover:not(:disabled) { color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element-hover); }
  .btn:active:not(:disabled) { transform: scale(0.96); }
  .btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn.primary:not(:disabled) { color: var(--accent-green); border-color: color-mix(in srgb, var(--accent-green) 40%, transparent); }
  .btn.ghost { border-color: transparent; }
  .btn.ghost:hover:not(:disabled) { border-color: var(--border-subtle); }
  .btn:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }
  @media (prefers-reduced-motion: reduce) { .btn { transition: none; } }
</style>
