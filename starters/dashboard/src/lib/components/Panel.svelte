<script lang="ts">
  import type { Snippet } from "svelte";
  let {
    title,
    description,
    actions,
    children,
    flat = false,
  }: {
    title: string;
    description?: string;
    actions?: Snippet;
    children: Snippet;
    flat?: boolean;
  } = $props();
</script>

<section class="panel" class:flat aria-label={title}>
  <header>
    <div>
      <h2>{title}</h2>
      {#if description}<p>{description}</p>{/if}
    </div>
    {#if actions}<div class="actions">{@render actions()}</div>{/if}
  </header>
  {@render children()}
</section>

<style>
  .panel {
    min-width: 0;
    border: 1px solid var(--line);
    border-radius: var(--radius-panel);
    padding: 16px 18px;
    background: var(--surface);
  }
  .panel.flat {
    border: 0;
    border-radius: 0;
    padding: 0;
    background: transparent;
  }
  header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: var(--space-4);
    padding-bottom: var(--space-4);
  }
  p {
    color: var(--ink-tertiary);
    font-size: 12px;
    margin-top: var(--space-1);
  }
  .actions {
    flex-shrink: 0;
  }
</style>
