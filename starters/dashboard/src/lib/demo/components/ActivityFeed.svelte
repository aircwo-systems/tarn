<script lang="ts">
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import { activityTone, type Activity } from "$lib/demo/models";
  import { formatTime } from "$lib/format";
  let {
    events,
    compact = false,
  }: { events: readonly Activity[]; compact?: boolean } = $props();
</script>

<ol class:compact aria-label="Activity events">
  {#each events as event (event.id)}
    <li>
      <div class="event-top">
        <span class="resource mono">{event.resource}</span><time
          datetime={event.timestamp}
          title={`${event.timestamp} · UTC`}>{formatTime(event.timestamp)}</time
        >
      </div>
      <p>{event.message}</p>
      <StatusBadge label={event.level} tone={activityTone[event.level]} />
    </li>
  {/each}
</ol>

<style>
  ol {
    margin: 0;
    padding: 0;
    list-style: none;
    border-top: 1px solid var(--line);
  }
  li {
    padding: var(--space-4) 0;
    border-bottom: 1px solid var(--line);
  }
  .event-top {
    display: flex;
    justify-content: space-between;
    gap: var(--space-3);
  }
  .resource {
    font-size: 11px;
    color: var(--ink);
  }
  time {
    color: var(--ink-tertiary);
    font: 10px var(--font-data);
    white-space: nowrap;
  }
  p {
    margin: var(--space-2) 0 var(--space-3);
    font-size: 12px;
    color: var(--ink-secondary);
    max-width: 72ch;
  }
  .compact p {
    font-size: 11px;
  }
</style>
