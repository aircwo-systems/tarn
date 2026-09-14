<script lang="ts">
  import type { Snippet } from "svelte";

  let {
    href,
    onclick,
    variant = "default",
    small = false,
    disabled = false,
    type = "button",
    title,
    children,
  }: {
    href?: string;
    onclick?: (e: MouseEvent) => void;
    variant?: "default" | "ghost" | "primary" | "danger";
    small?: boolean;
    disabled?: boolean;
    type?: "button" | "submit";
    title?: string;
    children?: Snippet;
  } = $props();
</script>

{#if href}
  <a class="rc-btn" class:small data-variant={variant} {href} {title}>{@render children?.()}</a>
{:else}
  <button {type} class="rc-btn" class:small data-variant={variant} {disabled} {title} {onclick}>
    {@render children?.()}
  </button>
{/if}

<style>
  .rc-btn {
    --tone: var(--text-secondary);
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 11px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11.5px; color: var(--tone); text-decoration: none; white-space: nowrap;
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease, opacity 120ms ease;
  }
  .rc-btn:hover:not(:disabled) { color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element-hover); }
  .rc-btn:active:not(:disabled) { transform: scale(0.96); }
  .rc-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .rc-btn:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }
  .small { height: 24px; padding: 0 9px; font-size: 11px; }
  [data-variant="ghost"] { border-color: transparent; }
  [data-variant="primary"] { --tone: var(--accent-green); }
  [data-variant="danger"] { --tone: var(--accent-red); }
  [data-variant="primary"], [data-variant="danger"] {
    border-color: color-mix(in srgb, var(--tone) 45%, transparent);
    background: color-mix(in srgb, var(--tone) 10%, transparent);
  }
  [data-variant="primary"]:hover:not(:disabled), [data-variant="danger"]:hover:not(:disabled) {
    color: var(--tone); border-color: var(--tone); background: color-mix(in srgb, var(--tone) 16%, transparent);
  }
  .rc-btn :global(svg) { flex-shrink: 0; }
</style>
