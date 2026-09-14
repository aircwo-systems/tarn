<script lang="ts">
  import RcKv, { type KvItem } from "$lib/components/rack/rc-kv.svelte";
  import type { EventBridgeTargetSummary } from "$lib/types";

  let { target }: { target: EventBridgeTargetSummary } = $props();

  const mode = $derived(
    target.inputTemplate || target.inputPathsMap
      ? "Input transformer"
      : target.input
        ? "Constant JSON"
        : target.inputPath
          ? "Part of event"
          : "Matched event",
  );

  const pretty = $derived.by(() => {
    if (!target.input) return "";
    try {
      return JSON.stringify(JSON.parse(target.input), null, 2);
    } catch {
      return target.input;
    }
  });

  const items = $derived<KvItem[]>([
    { label: "Payload", value: mode },
    ...(target.inputPath ? [{ label: "InputPath", value: target.inputPath, mono: true }] : []),
    ...(target.taskDefinition ? [{ label: "Task def", value: target.taskDefinition, mono: true }] : []),
    ...(target.roleArn ? [{ label: "Role", value: target.roleArn, mono: true, dim: true }] : []),
    { label: "ARN", value: target.arn, mono: true, dim: true },
  ]);

  const paths = $derived(Object.entries(target.inputPathsMap ?? {}));
</script>

<div class="settings">
  <RcKv {items} labelWidth="5.5rem" />

  {#if pretty}
    <figure>
      <figcaption>Input sent to target</figcaption>
      <pre>{pretty}</pre>
    </figure>
  {/if}

  {#if paths.length > 0}
    <figure>
      <figcaption>Input paths</figcaption>
      <dl class="paths">
        {#each paths as [key, path] (key)}
          <dt>{key}</dt><dd>{path}</dd>
        {/each}
      </dl>
    </figure>
  {/if}

  {#if target.inputTemplate}
    <figure>
      <figcaption>Template</figcaption>
      <pre>{target.inputTemplate}</pre>
    </figure>
  {/if}

  {#if mode === "Matched event"}
    <p class="hint">No override — the target receives the full event.</p>
  {/if}
</div>

<style>
  .settings { display: flex; flex-direction: column; gap: 12px; }
  figcaption { font-size: 10.5px; letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-tertiary); margin-bottom: 6px; }
  pre {
    max-height: 240px; overflow: auto; padding: 10px 12px; border-radius: 8px; background: var(--bg-app);
    font: 11.5px/1.6 var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); white-space: pre-wrap; word-break: break-word;
  }
  .paths { display: grid; grid-template-columns: max-content 1fr; gap: 4px 14px; font: 11.5px var(--font-mono, ui-monospace, monospace); }
  .paths dt { color: var(--text-tertiary); }
  .paths dd { color: var(--text-primary); }
  .hint { font-size: 11px; color: var(--text-tertiary); }
</style>
