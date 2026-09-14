<script lang="ts">
  import type { Snippet } from "svelte";

  let {
    label,
    value,
    sub = "",
    valueColor = "var(--text-primary)",
    children,
  }: {
    label: string;
    value?: string | number;
    sub?: string;
    valueColor?: string;
    children?: Snippet;
  } = $props();
</script>

<div class="metric-card flex flex-col justify-between rounded-md border border-border/70 bg-card/60 p-3.5 transition-colors">
  <div class="text-[11px] font-medium tracking-[0.04em] uppercase text-muted-foreground">
    {label}
  </div>
  <div class="mt-1.5 flex items-baseline gap-2">
    {#if children}
      {@render children()}
    {:else}
      <span
        class="font-mono text-[20px] font-semibold tracking-[-0.02em] tabular-nums"
        style="color: {valueColor};"
      >
        {value ?? "—"}
      </span>
    {/if}
  </div>
  {#if sub}
    <div class="mt-1 text-[11px] text-muted-foreground/80">
      {sub}
    </div>
  {/if}
</div>
