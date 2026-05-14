<script lang="ts">
  import { Tooltip } from "$lib/components/ui/simple-tooltip";

  export interface SparkBar {
    h: number;
    current: boolean;
    label: string;
  }

  let {
    bars,
    color,
    currentOpacity = 0.2,
    filledOpacity = 0.5,
    emptyOpacity = 0.08,
    scaleWithHeight = false,
  }: {
    bars: SparkBar[];
    color: string;
    currentOpacity?: number;
    filledOpacity?: number;
    emptyOpacity?: number;
    scaleWithHeight?: boolean;
  } = $props();

  function opacity(bar: SparkBar): number {
    if (bar.current) return currentOpacity;
    if (bar.h > 0) return scaleWithHeight ? 0.5 + bar.h * 0.004 : filledOpacity;
    return emptyOpacity;
  }
</script>

<div class="flex h-[18px] gap-px">
  {#each bars as bar, i (i)}
    <Tooltip text={bar.label} class="relative flex-1 min-w-[2px]">
      <div
        class="absolute bottom-0 w-full rounded-t-[1px] transition-all duration-500"
        style="height:{Math.max(bar.h, 4)}%;background:{color};opacity:{opacity(bar)}"
      ></div>
    </Tooltip>
  {/each}
</div>
