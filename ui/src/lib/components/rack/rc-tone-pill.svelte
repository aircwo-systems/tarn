<script lang="ts" module>
  export type Tone = "green" | "amber" | "red" | "neutral";
</script>

<script lang="ts">
  import type { Snippet } from "svelte";

  let { tone = "neutral", children }: { tone?: Tone; children?: Snippet } = $props();
</script>

<span class="rc-tone-pill" data-tone={tone}>{@render children?.()}</span>

<style>
  .rc-tone-pill {
    --tone: var(--text-secondary);
    display: inline-flex; align-items: center; gap: 4px; height: 22px; padding: 0 9px;
    border-radius: 8px; font-size: 11px; white-space: nowrap; text-transform: capitalize;
    color: var(--text-secondary); border: 1px solid var(--border-subtle);
  }
  [data-tone="green"] { --tone: var(--accent-green); }
  [data-tone="amber"] { --tone: var(--accent-amber); }
  [data-tone="red"] { --tone: var(--accent-red); }
  [data-tone]:not([data-tone="neutral"]) {
    color: var(--tone);
    border-color: color-mix(in srgb, var(--tone) 45%, transparent);
    background: color-mix(in srgb, var(--tone) 10%, transparent);
  }
</style>
