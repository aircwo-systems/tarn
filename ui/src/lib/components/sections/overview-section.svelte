<script lang="ts">
  import {
    WarningIcon,
    MagnifyingGlassIcon,
    XIcon,
    SidebarSimpleIcon,
  } from "phosphor-svelte";
  import LedDot from "$lib/components/common/led-dot.svelte";
  import {
    getDashboard,
    getDashboardFilters,
    getUISettings,
    getVisibleInfra,
    matchesTagFilter,
    setDashboardTagFilter,
  } from "$lib/state.svelte";
  import Separator from "$lib/components/ui/separator/separator.svelte";
  import OverviewPulseStrip from "./overview-pulse-strip.svelte";
  import OverviewActivityFeed from "./overview-activity-feed.svelte";
  import TopologyCanvas from "$lib/components/topology/topology-canvas.svelte";

  let {
    onNavigate = (_tab: string) => {},
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    onNavigate?: (tab: string) => void;
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const uiSettings = getUISettings();

  let tagDraft = $state(filters.tagFilter);

  $effect(() => {
    tagDraft = filters.tagFilter;
  });

  function applyFilter() {
    setDashboardTagFilter(tagDraft);
  }

  function clearFilter() {
    tagDraft = "";
    setDashboardTagFilter("");
  }

  const filteredFunctions = $derived(
    (dashboard.data?.functions ?? []).filter((f) =>
      matchesTagFilter(f.tags, filters.tagFilter),
    ),
  );
  const filteredGateways = $derived(
    (dashboard.data?.gateways ?? []).filter((g) =>
      matchesTagFilter(g.tags, filters.tagFilter),
    ),
  );

  const visibleInfra = $derived(
    getVisibleInfra(dashboard.data?.infrastructure ?? []),
  );
  const infraConnected = $derived(
    visibleInfra.filter((p) => p.status === "connected").length,
  );
  const recentTraces = $derived(dashboard.data?.recentTraces ?? []);
  let selectedRecentTraceId = $state<string | null>(null);
  let isCanvasExpanded = $state(false);
  let sidebarWasCollapsed = $state(false);

  function handleCanvasExpand(expanded: boolean) {
    if (expanded) {
      sidebarWasCollapsed = sidebarCollapsed;
      if (!sidebarCollapsed) onToggleSidebar();
    } else {
      if (sidebarCollapsed !== sidebarWasCollapsed) onToggleSidebar();
    }
    isCanvasExpanded = expanded;
  }

  const activeServiceCount = $derived(
    filteredFunctions.length + filteredGateways.length + infraConnected,
  );

  const connectionStatus = $derived(
    dashboard.error
      ? "error"
      : dashboard.loading
        ? "loading"
        : dashboard.data
          ? "ok"
          : "idle",
  );
</script>

<div class="flex h-full min-h-0 flex-col gap-4 overflow-hidden px-4 py-4 md:px-6 md:py-5 pb-16 md:pb-5">
  <!-- Status bar header -->
  <div class="shrink-0 flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-border pb-4">
    <!-- Sidebar toggle — desktop only -->
    <button
      type="button"
      onclick={onToggleSidebar}
      class="hidden md:flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground/50 transition-colors hover:bg-muted/60 hover:text-foreground"
      aria-label={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
    >
      <SidebarSimpleIcon
        size={14}
        weight={sidebarCollapsed ? "regular" : "fill"}
      />
    </button>

    <!-- Connection indicator -->
    <div class="inline-flex min-w-0 shrink-0 items-center gap-1.5">
      <LedDot
        color={connectionStatus === "ok"
          ? "green"
          : connectionStatus === "loading"
            ? "amber"
            : "red"}
        size="md"
      />
      <span
        class="max-w-[16rem] truncate font-mono text-[11px] text-muted-foreground"
        title={dashboard.data?.config.endpoint}
      >
        {dashboard.data?.config.endpoint ?? "connecting..."}
      </span>
      {#if dashboard.data?.config.region}
        <span class="hidden text-muted-foreground/30 sm:inline">·</span>
        <span class="hidden font-mono text-[11px] text-muted-foreground/60 sm:inline">
          {dashboard.data.config.region}
        </span>
      {/if}
    </div>

    <span class="hidden h-4 w-px shrink-0 bg-border sm:block"></span>

    <!-- Tag filter -->
    <div
      class="flex min-w-48 flex-1 items-center gap-1.5 rounded-[5px] border border-border bg-muted/50 px-2"
    >
      <MagnifyingGlassIcon size={12} class="shrink-0 text-muted-foreground/50" />
      <input
        type="text"
        placeholder="Filter by tag..."
        bind:value={tagDraft}
        onkeydown={(e) => {
          if (e.key === "Enter") applyFilter();
        }}
        class="h-[28px] w-full bg-transparent font-mono text-[11px] text-foreground outline-none placeholder:text-muted-foreground/40"
      />
      {#if tagDraft}
        <button
          type="button"
          onclick={clearFilter}
          class="flex h-5 w-5 items-center justify-center rounded text-muted-foreground/50 transition-colors hover:text-foreground"
          aria-label="Clear tag filter"
        >
          <XIcon size={10} />
        </button>
      {/if}
      {#if tagDraft !== filters.tagFilter}
        <button
          type="button"
          onclick={applyFilter}
          class="rounded px-1.5 py-0.5 font-mono text-[11px] text-primary transition-colors hover:bg-primary/10"
        >
          Apply
        </button>
      {/if}
    </div>

    <!-- Poll indicator -->
    <div class="ml-auto inline-flex shrink-0 items-center gap-1">
      <span class="h-[5px] w-[5px] shrink-0 rounded-full bg-primary"></span>
      <span class="font-mono text-[11px] text-muted-foreground/60">
        Poll {uiSettings.pollingIntervalSeconds}s
      </span>
    </div>
  </div>

  {#if dashboard.data?.warnings?.length}
    <div class="shrink-0 rounded-md border border-destructive/30 bg-destructive/5 px-4 py-3">
      <div class="mb-2 flex items-center gap-2">
        <WarningIcon size={14} class="text-destructive" />
        <h3 class="text-sm font-semibold text-destructive">Warnings</h3>
      </div>
      <ul class="list-disc space-y-1 pl-5 text-xs text-destructive/90">
        {#each dashboard.data.warnings as warning (warning)}
          <li>{warning}</li>
        {/each}
      </ul>
    </div>
  {/if}

  {#if !isCanvasExpanded}
    <div class="shrink-0">
      <OverviewPulseStrip traces={recentTraces} {activeServiceCount} />
    </div>
  {/if}

  <div
    class={`flex-1 min-h-0 grid min-w-0 gap-4 ${isCanvasExpanded ? "lg:grid-cols-1" : "lg:grid-cols-[minmax(0,1fr)_20rem]"}`}
  >
    <div class="min-w-0 flex flex-col min-h-0">
      <div class="flex-1 min-h-0">
        <TopologyCanvas
          {onNavigate}
          onExpandedChange={handleCanvasExpand}
        />
      </div>
    </div>

    {#if !isCanvasExpanded}
      <div class="flex min-w-0 min-h-0 flex-col overflow-hidden">
        <OverviewActivityFeed
          traces={recentTraces}
          selectedTraceId={selectedRecentTraceId}
          onSelect={(id) => (selectedRecentTraceId = id)}
        />
      </div>
    {/if}
  </div>
</div>
