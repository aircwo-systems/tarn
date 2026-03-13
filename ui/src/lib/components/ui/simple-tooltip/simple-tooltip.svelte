<script lang="ts">
  import { cn } from "$lib/utils";

  let {
    text = "",
    class: className,
    children,
  }: {
    text?: string;
    class?: string;
    children?: import("svelte").Snippet;
  } = $props();

  let visible = $state(false);
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<span
  class={cn("relative inline-flex", className)}
  onmouseenter={() => (visible = true)}
  onmouseleave={() => (visible = false)}
  onfocus={() => (visible = true)}
  onblur={() => (visible = false)}
>
  {@render children?.()}
  {#if visible && text}
    <span
      role="tooltip"
      class="absolute bottom-full left-1/2 -translate-x-1/2 mb-1.5 px-2 py-1 text-xs rounded-md bg-popover border border-border text-foreground whitespace-nowrap z-50"
    >
      {text}
    </span>
  {/if}
</span>
