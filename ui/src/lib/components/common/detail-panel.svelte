<script lang="ts">
  import { XIcon } from "phosphor-svelte";
  import type { Snippet } from "svelte";

  let {
    open = false,
    onclose = () => {},
    title,
    subtitle,
    width = 440,
    children,
  }: {
    open?: boolean;
    onclose?: () => void;
    title: string;
    subtitle?: string;
    width?: number;
    children?: Snippet;
  } = $props();
</script>

<div
  class="absolute inset-y-0 right-0 z-10 overflow-hidden border-l border-border bg-card shadow-xl transition-[width,opacity] duration-200 ease-out {open
    ? 'opacity-100'
    : 'pointer-events-none opacity-0'}"
  style="width: {open ? `${width}px` : '0px'}"
>
  {#if open}
    <div class="flex h-full flex-col" style="min-width: {width}px">
      <div class="flex shrink-0 items-center justify-between gap-3 border-b border-border bg-card px-4 py-2.5">
        <div class="min-w-0">
          <p class="truncate font-mono text-sm text-foreground">{title}</p>
          {#if subtitle}
            <p class="mt-1 text-[11px] text-muted-foreground/70">{subtitle}</p>
          {/if}
        </div>
        <button
          type="button"
          onclick={onclose}
          class="flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground/70 transition-colors hover:bg-muted hover:text-foreground"
          aria-label="Close panel"
        >
          <XIcon size={14} />
        </button>
      </div>
      <div class="flex-1 overflow-y-auto p-4 space-y-5">
        {@render children?.()}
      </div>
    </div>
  {/if}
</div>
