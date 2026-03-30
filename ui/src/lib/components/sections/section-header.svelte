<script lang="ts">
  import type { Snippet } from "svelte";
  import { SidebarSimpleIcon } from "phosphor-svelte";

  let {
    title,
    description = "",
    icon: Icon,
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
    lead,
    stats,
    actions,
  }: {
    title: string;
    description?: string;
    icon: any;
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
    lead?: Snippet;
    stats?: Snippet;
    actions?: Snippet;
  } = $props();
</script>

<div class="shrink-0 flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-border pb-4">
  <button
    type="button"
    onclick={onToggleSidebar}
    class="flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground/50 transition-colors hover:bg-muted/60 hover:text-foreground"
    aria-label={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
    title={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
  >
    <SidebarSimpleIcon
      size={14}
      weight={sidebarCollapsed ? "regular" : "fill"}
    />
  </button>

  {@render lead?.()}

  <div class="inline-flex min-w-0 shrink-0 items-center gap-2.5">
    <span
      class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-border bg-muted/40 text-primary"
    >
      <Icon size={16} weight="fill" />
    </span>
    <div class="min-w-0">
      <h1 class="truncate text-sm font-semibold text-foreground">{title}</h1>
      {#if description}
        <p class="truncate text-[11px] text-muted-foreground/70">
          {description}
        </p>
      {/if}
    </div>
  </div>

  {#if stats}
    <span class="hidden h-4 w-px shrink-0 bg-border sm:block"></span>
    <div class="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px]">
      {@render stats()}
    </div>
  {/if}

  {#if actions}
    <div class="ml-auto flex flex-wrap items-center gap-2 text-[11px]">
      {@render actions()}
    </div>
  {/if}
</div>
