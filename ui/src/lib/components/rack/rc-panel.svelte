<script lang="ts">
  import type { Snippet } from "svelte";

  let {
    title,
    description = "",
    id,
    index = 0,
    actions,
    children,
  }: {
    title?: string;
    description?: string;
    id?: string;
    /** position in a stack; staggers the entrance */
    index?: number;
    actions?: Snippet;
    children?: Snippet;
  } = $props();
</script>

<section {id} class="rc-panel" style:--i={index}>
  {#if title || actions}
    <header>
      <div class="head">
        {#if title}<h2>{title}</h2>{/if}
        {#if description}<p>{description}</p>{/if}
      </div>
      {#if actions}<div class="actions">{@render actions()}</div>{/if}
    </header>
  {/if}
  {@render children?.()}
</section>

<style>
  /* Groups have no card chrome: space and the title separate them. */
  .rc-panel {
    scroll-margin-top: 12px;
    padding: 6px 2px 22px;
    animation: rcPanelIn 320ms var(--ease-snappy) both;
    animation-delay: calc(var(--i, 0) * 30ms);
  }
  header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
  .head { min-width: 0; }
  h2 { font-size: 13px; font-weight: 600; color: var(--text-primary); letter-spacing: -0.01em; }
  p { margin-top: 2px; font-size: 11.5px; color: var(--text-secondary); }
  .actions { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }
  @keyframes rcPanelIn { from { opacity: 0; transform: translateY(6px); } }
  @media (prefers-reduced-motion: reduce) { .rc-panel { animation: none; } }
</style>
