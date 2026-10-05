<script lang="ts">
  import type { Snippet } from "svelte";
  import type { HTMLButtonAttributes } from "svelte/elements";

  let {
    variant = "default",
    children,
    ...attributes
  }: HTMLButtonAttributes & {
    variant?: "default" | "primary" | "danger" | "ghost";
    children: Snippet;
  } = $props();
</script>

<button
  type="button"
  {...attributes}
  class="button {attributes.class ?? ''}"
  data-variant={variant}
>
  {@render children()}
</button>

<style>
  .button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    min-height: 28px;
    padding: 4px 11px;
    border: 1px solid var(--line);
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--ink-secondary);
    font-size: 11.5px;
    font-weight: 500;
    white-space: nowrap;
    transition:
      background var(--duration-fast),
      color var(--duration-fast),
      border-color var(--duration-fast);
  }
  .button:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--ink);
  }
  .button:active:not(:disabled) {
    background: var(--surface-raised);
  }
  .button:disabled {
    opacity: 0.45;
  }
  .button[data-variant="ghost"] {
    border-color: transparent;
  }
  .button[data-variant="primary"] {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 45%, transparent);
    background: color-mix(in srgb, var(--accent) 10%, transparent);
  }
  .button[data-variant="danger"] {
    color: var(--danger);
    border-color: color-mix(in srgb, var(--danger) 45%, transparent);
    background: color-mix(in srgb, var(--danger) 10%, transparent);
  }
  @media (max-width: 640px) {
    .button {
      min-height: 44px;
    }
  }
</style>
