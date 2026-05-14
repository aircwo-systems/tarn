<script lang="ts">
  import { PaneGroup } from "paneforge";
  import type { ComponentProps } from "svelte";
  import { cn } from "$lib/lib/utils.js";

  let {
    class: className,
    direction = "horizontal",
    ...restProps
  }: ComponentProps<typeof PaneGroup> & { class?: string } = $props();
</script>

<PaneGroup
  {direction}
  class={cn("flex h-full w-full data-[direction=vertical]:flex-col", className)}
  {...restProps}
/>
