<script lang="ts">
  import { PaneResizer } from "paneforge";
  import { cn } from "$lib/lib/utils.js";

  let {
    class: className,
    withHandle = true,
    ...restProps
  }: { class?: string; withHandle?: boolean; [key: string]: unknown } = $props();
</script>

<PaneResizer
  class={cn(
    "relative flex w-px items-center justify-center bg-border transition-colors",
    "after:absolute after:inset-y-0 after:left-1/2 after:w-1 after:-translate-x-1/2",
    "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring focus-visible:ring-offset-1",
    "data-[state=hover]:bg-border/80 data-[state=drag]:bg-primary/40",
    "[&[data-direction=vertical]]:h-px [&[data-direction=vertical]]:w-full",
    "[&[data-direction=vertical]]:after:left-0 [&[data-direction=vertical]]:after:h-1",
    "[&[data-direction=vertical]]:after:w-full [&[data-direction=vertical]]:after:translate-x-0",
    "[&[data-direction=vertical]]:after:-translate-y-1/2 [&[data-direction=vertical]]:after:top-1/2",
    className,
  )}
  {...restProps}
>
  {#if withHandle}
    <div class="z-10 flex h-8 w-3 items-center justify-center rounded-sm border border-border bg-background">
      <svg
        width="6"
        height="14"
        viewBox="0 0 6 14"
        fill="none"
        class="text-border"
        aria-hidden="true"
      >
        <circle cx="1" cy="2" r="1" fill="currentColor" />
        <circle cx="5" cy="2" r="1" fill="currentColor" />
        <circle cx="1" cy="6" r="1" fill="currentColor" />
        <circle cx="5" cy="6" r="1" fill="currentColor" />
        <circle cx="1" cy="10" r="1" fill="currentColor" />
        <circle cx="5" cy="10" r="1" fill="currentColor" />
      </svg>
    </div>
  {/if}
</PaneResizer>
