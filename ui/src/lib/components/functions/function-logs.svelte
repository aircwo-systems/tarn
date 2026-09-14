<script lang="ts">
  import { untrack } from "svelte";
  import { fetchLogEvents } from "$lib/api";
  import type { LogEvent } from "$lib/types";

  let {
    functionName,
    /** bump to refetch (e.g. invocation count) */
    revision = 0,
    limit = 10,
  }: {
    functionName: string;
    revision?: number;
    limit?: number;
  } = $props();

  let events = $state<LogEvent[]>([]);
  let loading = $state(true);

  const group = $derived(`/aws/lambda/${functionName}`);

  let lastGroup = "";

  $effect(() => {
    const name = group;
    void revision;
    const ctrl = new AbortController();
    untrack(() => {
      // Switching functions: drop old lines so they don't flash under the new name.
      if (name !== lastGroup) {
        lastGroup = name;
        events = [];
      }
      loading = events.length === 0;
    });
    fetchLogEvents(name, { limit, order: "desc" }, ctrl.signal)
      .then((res) => (events = res.events ?? []))
      .catch((err) => {
        if (!ctrl.signal.aborted) events = [];
        void err;
      })
      .finally(() => {
        if (!ctrl.signal.aborted) loading = false;
      });
    return () => ctrl.abort();
  });

  function level(e: LogEvent): string {
    return (e.level || "info").toLowerCase();
  }

  function clock(ts: string): string {
    const d = new Date(ts);
    return Number.isNaN(d.getTime()) ? "" : d.toLocaleTimeString([], { hour12: false });
  }

  function href(e: LogEvent): string {
    const qs = new URLSearchParams({ groups: group, ts: e.timestamp });
    if (e.streamName) qs.set("stream", e.streamName);
    return `#logs?${qs}`;
  }
</script>

{#if loading}
  <div class="skeleton">
    {#each Array(4) as _, i (i)}<span style:width="{70 - i * 12}%"></span>{/each}
  </div>
{:else if events.length === 0}
  <p class="empty">No log lines in <code>{group}</code> yet.</p>
{:else}
  <ol class="lines">
    {#each events as e, i (e.timestamp + i)}
      <li>
        <a class="line" data-level={level(e)} href={href(e)}>
          <span class="ts">{clock(e.timestamp)}</span>
          <span class="msg">{e.message}</span>
        </a>
      </li>
    {/each}
  </ol>
{/if}

<style>
  .empty { font-size: 11.5px; color: var(--text-tertiary); }
  code { font-family: var(--font-mono, ui-monospace, monospace); font-size: 11px; color: var(--text-secondary); }
  .lines { display: flex; flex-direction: column; gap: 1px; }
  .line {
    --lvl: transparent;
    position: relative; display: flex; gap: 12px; padding: 4px 10px; border-radius: 6px; text-decoration: none;
    font: 11.5px/1.5 var(--font-mono, ui-monospace, monospace);
    transition: background 120ms ease;
  }
  .line::before {
    content: ""; position: absolute; left: 3px; top: 6px; bottom: 6px; width: 2px; border-radius: 2px; background: var(--lvl);
  }
  .line:hover { background: var(--bg-element-hover); }
  .line[data-level="error"], .line[data-level="fatal"] { --lvl: var(--accent-red); }
  .line[data-level="warn"], .line[data-level="warning"] { --lvl: var(--accent-amber); }
  .ts { flex-shrink: 0; color: var(--text-tertiary); font-variant-numeric: tabular-nums; }
  .msg { min-width: 0; color: var(--text-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .line:hover .msg { color: var(--text-primary); }
  [data-level="error"] .msg, [data-level="fatal"] .msg { color: var(--accent-red); }
  .skeleton { display: flex; flex-direction: column; gap: 8px; padding: 4px 10px; }
  .skeleton span { height: 10px; border-radius: 4px; background: var(--bg-element); animation: pulse 1.4s ease-in-out infinite; }
  @keyframes pulse { 50% { opacity: 0.5; } }
  @media (prefers-reduced-motion: reduce) { .skeleton span { animation: none; } }
</style>
