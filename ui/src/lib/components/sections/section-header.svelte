<script lang="ts">
  import type { Snippet } from "svelte";
  import { SidebarSimpleIcon } from "phosphor-svelte";

  let {
    title,
    description = "",
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
    lead,
    stats,
    actions,
  }: {
    title: string;
    description?: string;
    /** @deprecated no longer rendered */
    icon?: any;
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
    lead?: Snippet;
    stats?: Snippet;
    actions?: Snippet;
  } = $props();
</script>

<div class="shrink-0 flex flex-wrap items-center justify-between gap-x-4 gap-y-3 border-b border-border/80 pb-4">
  <div class="flex min-w-0 flex-wrap items-center gap-3">
    <button
      type="button"
      onclick={onToggleSidebar}
      class="flex h-7 w-7 shrink-0 items-center justify-center rounded-md border border-border/60 bg-background/50 text-muted-foreground/60 transition-colors hover:border-border hover:bg-muted/50 hover:text-foreground"
      aria-label={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
      title={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
    >
      <SidebarSimpleIcon
        size={14}
        weight={sidebarCollapsed ? "regular" : "fill"}
      />
    </button>

    {@render lead?.()}

    <div class="inline-flex min-w-0 items-center gap-2.5">
      <div class="min-w-0">
        <h1 class="truncate text-[15px] font-semibold tracking-[-0.01em] text-foreground">{title}</h1>
        {#if description}
          <p class="truncate text-[12px] text-muted-foreground">
            {description}
          </p>
        {/if}
      </div>
    </div>

    {#if stats}
      <span class="hidden h-4 w-px shrink-0 bg-border/60 sm:block"></span>
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px]">
        {@render stats()}
      </div>
    {/if}
  </div>

  {#if actions}
    <div class="flex flex-wrap items-center gap-2 text-[12px]">
      {@render actions()}
    </div>
  {/if}
</div>
