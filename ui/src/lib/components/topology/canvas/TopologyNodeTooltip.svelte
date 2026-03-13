<script lang="ts">
  import { fade } from "svelte/transition";

  let {
    visible = false,
    x = 0,
    y = 0,
    title = "",
    detail = "",
    status = "",
    color = "var(--color-primary)",
  }: {
    visible?: boolean;
    x?: number;
    y?: number;
    title?: string;
    detail?: string;
    status?: string;
    color?: string;
  } = $props();
</script>

{#if visible}
  <div
    transition:fade={{ duration: 100 }}
    class="pointer-events-none fixed z-50 min-w-40 rounded border border-primary/40 bg-popover/92 px-3 py-2 shadow-xl backdrop-blur-md"
    style={`left: ${x + 15}px; top: ${y + 15}px;`}
  >
    <div class="mb-1 flex items-center justify-between gap-4">
      <span class="text-[10px] font-mono tracking-tight text-muted-foreground/80">{detail}</span>
      <span class="h-1.5 w-1.5 rounded-full" style={`background:${color}`}></span>
    </div>

    <p class="truncate text-xs font-semibold text-foreground">{title}</p>

    {#if status}
      <span
        class="mt-1 inline-flex rounded px-1.5 py-0.5 text-[10px] font-mono uppercase"
        style={`background:${color}22; color:${color}`}
      >
        {status}
      </span>
    {/if}
  </div>
{/if}
