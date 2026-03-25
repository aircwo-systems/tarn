<script lang="ts">
  import {
    WarningIcon,
    GlobeHemisphereWestIcon,
    LightningIcon,
    ChatCircleIcon,
    BellIcon,
    KeyIcon,
    HardDriveIcon,
    ArrowsClockwiseIcon,
    MagnifyingGlassIcon,
    XIcon,
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
  import Badge from "$lib/components/ui/badge/badge.svelte";
  import Button from "$lib/components/ui/button/button.svelte";
  import { Card, CardContent, CardHeader } from "$lib/components/ui/card";
  import Separator from "$lib/components/ui/separator/separator.svelte";
  import ActiveComponents from "./active-components.svelte";
  import TopologyTraceTicker from "$lib/components/topology/canvas/TopologyTraceTicker.svelte";
  import TopologyCanvas from "$lib/components/topology/topology-canvas.svelte";

  let {
    onNavigate = (_tab: string) => {},
  }: { onNavigate?: (tab: string) => void } = $props();

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

  const filteredGateways = $derived(
    (dashboard.data?.gateways ?? []).filter((g) =>
      matchesTagFilter(g.tags, filters.tagFilter),
    ),
  );
  const filteredFunctions = $derived(
    (dashboard.data?.functions ?? []).filter((f) =>
      matchesTagFilter(f.tags, filters.tagFilter),
    ),
  );
  const filteredQueues = $derived(
    (dashboard.data?.queues ?? []).filter((q) =>
      matchesTagFilter(q.tags, filters.tagFilter),
    ),
  );
  const filteredSecrets = $derived(
    (dashboard.data?.secrets ?? []).filter((s) =>
      matchesTagFilter(s.tags, filters.tagFilter),
    ),
  );
  const filteredTopics = $derived(
    (dashboard.data?.topics ?? []).filter((t) =>
      matchesTagFilter(t.tags, filters.tagFilter),
    ),
  );

  const buckets = $derived(dashboard.data?.buckets ?? []);
  const eventMappings = $derived(dashboard.data?.eventSourceMappings ?? []);
  const eventBridgeRules = $derived(dashboard.data?.eventBridgeRules ?? []);
  const visibleInfra = $derived(
    getVisibleInfra(dashboard.data?.infrastructure ?? []),
  );
  const infraConnected = $derived(
    visibleInfra.filter((p) => p.status === "connected").length,
  );
  const recentTraces = $derived(dashboard.data?.recentTraces ?? []);
  let selectedRecentTraceId = $state<string | null>(null);
  let isCanvasExpanded = $state(false);

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

<div class="flex w-full min-w-0 flex-col gap-4 pl-4">
  <Card class="w-full min-w-0">
    <CardContent class="space-y-3 px-4 py-3 md:px-5">
      <div class="flex flex-wrap items-center gap-2">
        <div class="inline-flex min-w-0 shrink-0 items-center gap-2">
          <LedDot
            color={connectionStatus === "ok"
              ? "green"
              : connectionStatus === "loading"
                ? "amber"
                : "red"}
            size="md"
          />
          <span
            class="max-w-[16rem] truncate font-mono text-xs text-muted-foreground"
            title={dashboard.data?.config.endpoint}
          >
            {dashboard.data?.config.endpoint ?? "connecting..."}
          </span>
          {#if dashboard.data?.config.region}
            <span class="hidden text-muted-foreground/40 sm:inline">·</span>
            <span
              class="hidden font-mono text-[10px] text-muted-foreground sm:inline"
              >{dashboard.data.config.region}</span
            >
          {/if}
        </div>

        <Separator orientation="vertical" class="hidden h-4 sm:block" />

        <div
          class="flex min-w-56 flex-1 items-center gap-1 rounded-md border border-border bg-muted/30 px-2"
        >
          <MagnifyingGlassIcon
            size={12}
            class="shrink-0 text-muted-foreground"
          />
          <input
            type="text"
            placeholder="Filter infrastructure by tag..."
            bind:value={tagDraft}
            onkeydown={(e) => {
              if (e.key === "Enter") applyFilter();
            }}
            class="h-7 w-full bg-transparent text-xs text-foreground outline-none placeholder:text-muted-foreground"
          />
          {#if tagDraft}
            <Button
              variant="ghost"
              size="icon"
              class="h-6 w-6"
              type="button"
              onclick={clearFilter}
              aria-label="Clear tag filter"
            >
              <XIcon size={11} />
            </Button>
          {/if}
          {#if tagDraft !== filters.tagFilter}
            <Button
              variant="secondary"
              size="sm"
              type="button"
              onclick={applyFilter}
            >
              Apply
            </Button>
          {/if}
        </div>

        {#if filters.tagFilter}
          <Badge variant="default" class="font-mono text-[10px]">
            {filters.tagFilter}
          </Badge>
        {/if}

        <div class="ml-auto inline-flex shrink-0 items-center gap-1.5">
          <LedDot color="green" />
          <span class="font-mono text-[10px] text-muted-foreground">
            Poll {uiSettings.pollingIntervalSeconds}s
          </span>
        </div>
      </div>

      <div class="flex flex-wrap items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          onclick={() => onNavigate("gateways")}
          class="gap-1.5"
        >
          <GlobeHemisphereWestIcon size={12} />
          <span class="font-mono">{filteredGateways.length}</span>
          <span>Gateways</span>
        </Button>
        <Button
          variant="outline"
          size="sm"
          onclick={() => onNavigate("functions")}
          class="gap-1.5"
        >
          <LightningIcon size={12} />
          <span class="font-mono">{filteredFunctions.length}</span>
          <span>Functions</span>
        </Button>
        <Button
          variant="outline"
          size="sm"
          onclick={() => onNavigate("queues")}
          class="gap-1.5"
        >
          <ChatCircleIcon size={12} />
          <span class="font-mono">{filteredQueues.length}</span>
          <span>Queues</span>
        </Button>
        <Button
          variant="outline"
          size="sm"
          onclick={() => onNavigate("sns")}
          class="gap-1.5"
        >
          <BellIcon size={12} />
          <span class="font-mono">{filteredTopics.length}</span>
          <span>SNS</span>
        </Button>
        <Button
          variant="outline"
          size="sm"
          onclick={() => onNavigate("secrets")}
          class="gap-1.5"
        >
          <KeyIcon size={12} />
          <span class="font-mono">{filteredSecrets.length}</span>
          <span>Secrets</span>
        </Button>
        <Button
          variant="outline"
          size="sm"
          onclick={() => onNavigate("storage")}
          class="gap-1.5"
        >
          <HardDriveIcon size={12} />
          <span class="font-mono">{buckets.length}</span>
          <span>Buckets</span>
        </Button>
        {#if eventMappings.length > 0}
          <Button
            variant="outline"
            size="sm"
            onclick={() => onNavigate("triggers")}
            class="gap-1.5"
          >
            <ArrowsClockwiseIcon size={12} />
            <span class="font-mono">{eventMappings.length}</span>
            <span>Triggers</span>
          </Button>
        {/if}
        <Button
          variant="outline"
          size="sm"
          onclick={() => onNavigate("eventbridge")}
          class="gap-1.5"
        >
          <ArrowsClockwiseIcon size={12} />
          <span class="font-mono">{eventBridgeRules.length}</span>
          <span>EventBridge</span>
        </Button>
        {#if visibleInfra.length > 0}
          <Badge variant={infraConnected > 0 ? "secondary" : "destructive"}>
            {infraConnected}/{visibleInfra.length} infra connected
          </Badge>
        {/if}
      </div>
    </CardContent>
  </Card>

  {#if dashboard.data?.warnings?.length}
    <Card class="border-destructive/30 bg-destructive/5">
      <CardContent class="px-4 py-3">
        <div class="mb-2 flex items-center gap-2">
          <WarningIcon size={14} class="text-destructive" />
          <h3 class="text-sm font-semibold text-destructive">Warnings</h3>
        </div>
        <ul class="list-disc space-y-1 pl-5 text-xs text-destructive/90">
          {#each dashboard.data.warnings as warning (warning)}
            <li>{warning}</li>
          {/each}
        </ul>
      </CardContent>
    </Card>
  {/if}

  <div
    class={`grid min-w-0 gap-4 ${isCanvasExpanded ? "lg:grid-cols-1" : "lg:grid-cols-[minmax(0,1fr)_20rem]"}`}
  >
    <Card class="min-w-0 overflow-hidden">
      <CardHeader class="border-b border-border">
        <div class="flex items-center justify-between gap-2">
          <div class="min-w-0">
            <h3 class="truncate text-sm font-semibold text-foreground">
              Topology Stage
            </h3>
            <p class="text-xs text-muted-foreground">
              Visualize infrastructure and service connections
            </p>
          </div>
          <Badge variant="secondary" class="shrink-0" hidden
            >w-full · responsive</Badge
          >
        </div>
      </CardHeader>
      <CardContent class="p-3 md:p-4">
        <div
          class={`w-full overflow-hidden rounded-md border border-border bg-background ${
            isCanvasExpanded
              ? "h-[calc(100svh-12rem)] min-h-144"
              : "h-[58svh] min-h-112 lg:h-[calc(100svh-22rem)]"
          }`}
        >
          <TopologyCanvas
            {onNavigate}
            onExpandedChange={(expanded) => (isCanvasExpanded = expanded)}
          />
        </div>
      </CardContent>
    </Card>

    {#if !isCanvasExpanded}
      <div class="flex min-w-0 flex-col gap-4">
        <TopologyTraceTicker
          traces={recentTraces}
          selectedTraceId={selectedRecentTraceId}
          onSelect={(id) => (selectedRecentTraceId = id)}
        />
        <!-- To enable once used -->
        <Card hidden>
          <CardHeader class="border-b border-border">
            <h4 class="text-sm font-semibold text-foreground">
              Filters / Detail Rail
            </h4>
          </CardHeader>
          <CardContent class="p-3">
            <div
              class="flex h-40 items-center justify-center rounded-md border border-dashed border-border bg-muted/20 text-xs text-muted-foreground"
            >
              Placeholder
            </div>
          </CardContent>
        </Card>
      </div>
    {/if}
  </div>

  {#if !isCanvasExpanded}
    <Card>
      <CardHeader class="border-b border-border">
        <h4 class="text-sm font-semibold text-foreground">Further Content</h4>
      </CardHeader>
      <CardContent class="p-0">
        <ActiveComponents embedded={true} showHeader={false} />
      </CardContent>
    </Card>
  {/if}
</div>
