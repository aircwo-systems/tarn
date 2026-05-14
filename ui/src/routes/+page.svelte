<script lang="ts">
  import {
    SquaresFourIcon,
    GlobeHemisphereWestIcon,
    LightningIcon,
    ChatCircleIcon,
    BellIcon,
    KeyIcon,
    HardDriveIcon,
    ArrowsClockwiseIcon,
    MagnifyingGlassIcon,
    ShieldWarningIcon,
    BridgeIcon,
    ScrollIcon,
    DetectiveIcon,
    SidebarSimpleIcon,
    DatabaseIcon,
  } from "phosphor-svelte";
  import { onMount } from "svelte";

  import AppSidebar from "$lib/components/layout/app-sidebar.svelte";
  import TagFilter from "$lib/components/layout/tag-filter.svelte";
  import OverviewPulse from "$lib/components/layout/overview-pulse.svelte";
  import ActivityFeed from "$lib/components/layout/activity-feed.svelte";
  import type { SparkBar as SparkBarData } from "$lib/components/common/spark-bar.svelte";
  import TopologyCanvas from "$lib/components/topology/topology-canvas.svelte";
  import SettingsDialog from "$lib/components/layout/settings-dialog.svelte";
  import APIGatewaysSection from "$lib/components/sections/api-gateways-section.svelte";
  import FunctionsSection from "$lib/components/sections/functions-section.svelte";
  import QueuesSection from "$lib/components/sections/queues-section.svelte";
  import SNSSection from "$lib/components/sections/sns-section.svelte";
  import SecretsSection from "$lib/components/sections/secrets-section.svelte";
  import TriggersSection from "$lib/components/sections/triggers-section.svelte";
  import EventBridgeSection from "$lib/components/sections/eventbridge-section.svelte";
  import DynamoDBSection from "$lib/components/sections/dynamodb-section.svelte";
  import StorageSection from "$lib/components/sections/storage-section.svelte";
  import LogsSection from "$lib/components/sections/logs-section.svelte";
  import XraySection from "$lib/components/sections/xray-section.svelte";
  import ChaosSection from "$lib/components/sections/chaos-section.svelte";
  import {
    infraKindCssVar,
    normalizeTopologyInfraKind,
  } from "$lib/components/topology/topology-canvas-theme";

  import {
    getDashboard,
    getDashboardFilters,
    getUISettings,
    getInfraSettings,
    getVisibleInfra,
    matchesTagFilter,
    setInfraEnabledKinds,
    setInfraFrontendTargets,
    setLogRetentionMinutes,
    setPersistenceEnabled,
    setPollingIntervalSeconds,
    setSchemaSourceDir,
    setThemeMode,
    sanitizeSchemaSourceDir,
    type ThemeMode,
    type InfraProbeKind,
    type FrontendTarget,
  } from "$lib/state.svelte";

  import {
    resolveDirectPrototypeFilter,
    extractTagOnlyFilterQuery,
  } from "$lib/filter-utils";

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const uiSettings = getUISettings();
  const infraSettings = getInfraSettings();

  // ── Routing ─────────────────────────────────────────────────────
  const validTabs = ["overview","gateways","chaos","functions","queues","dynamodb","sns","secrets","triggers","eventbridge","storage","logs","xray"];
  let activeTab = $state("overview");
  let logsInitialGroup = $state("");
  let logsInitialTimestamp = $state("");
  let xrayInitialTraceId = $state("");

  function readHash() {
    const raw = window.location.hash.replace("#", "");
    const [tab, qs] = raw.split("?");
    if (validTabs.includes(tab)) activeTab = tab;
    logsInitialGroup = "";
    logsInitialTimestamp = "";
    xrayInitialTraceId = "";
    if (tab === "logs" && qs) {
      const params = new URLSearchParams(qs);
      logsInitialGroup = params.get("group") ?? "";
      logsInitialTimestamp = params.get("ts") ?? "";
    }
    if (tab === "xray" && qs) {
      const params = new URLSearchParams(qs);
      xrayInitialTraceId = params.get("trace") ?? "";
    }
  }

  function setTab(tab: string) {
    activeTab = tab;
    window.location.hash = tab;
  }

  function openTrace(traceId: string) {
    activeTab = "xray";
    xrayInitialTraceId = traceId;
    window.location.hash = `xray?trace=${encodeURIComponent(traceId)}`;
  }

  onMount(() => {
    readHash();
    window.addEventListener("hashchange", readHash);
    return () => window.removeEventListener("hashchange", readHash);
  });

  // ── Settings ────────────────────────────────────────────────────
  const INFRA_KINDS: Array<{ id: InfraProbeKind; label: string; detail: string }> = [
    { id: "docker",     label: "Docker",     detail: "daemon" },
    { id: "postgresql", label: "PostgreSQL", detail: ":5432"  },
    { id: "redis",      label: "Redis",      detail: ":6379"  },
    { id: "mysql",      label: "MySQL",      detail: ":3306"  },
    { id: "mongodb",    label: "MongoDB",    detail: ":27017" },
  ];

  let settingsOpen              = $state(false);
  let pollingIntervalDraft      = $state(uiSettings.pollingIntervalSeconds);
  let themeModeDraft            = $state<ThemeMode>(uiSettings.themeMode);
  let persistenceDraft          = $state(uiSettings.persistenceEnabled);
  let schemaSourceDirDraft      = $state(uiSettings.schemaSourceDir);
  let logRetentionMinutesDraft  = $state(uiSettings.logRetentionMinutes);
  let infraEnabledKindsDraft    = $state<InfraProbeKind[]>([]);
  let infraFrontendTargetsDraft = $state<FrontendTarget[]>([]);
  let newTargetName             = $state("");
  let newTargetPort             = $state("");

  function openSettings() {
    pollingIntervalDraft      = uiSettings.pollingIntervalSeconds;
    themeModeDraft            = uiSettings.themeMode;
    persistenceDraft          = uiSettings.persistenceEnabled;
    schemaSourceDirDraft      = uiSettings.schemaSourceDir;
    logRetentionMinutesDraft  = uiSettings.logRetentionMinutes;
    infraEnabledKindsDraft    = [...infraSettings.enabledKinds];
    infraFrontendTargetsDraft = infraSettings.frontendTargets.map(t => ({ ...t }));
    newTargetName = ""; newTargetPort = "";
    settingsOpen = true;
  }
  function closeSettings() { settingsOpen = false; }
  function applySettings() {
    setPollingIntervalSeconds(pollingIntervalDraft);
    setThemeMode(themeModeDraft);
    setPersistenceEnabled(persistenceDraft);
    setSchemaSourceDir(schemaSourceDirDraft);
    setLogRetentionMinutes(logRetentionMinutesDraft);
    setInfraEnabledKinds(infraEnabledKindsDraft);
    setInfraFrontendTargets(infraFrontendTargetsDraft);
    settingsOpen = false;
  }
  function addFrontendTarget() {
    const name = newTargetName.trim();
    const port = parseInt(newTargetPort, 10);
    if (!name || isNaN(port) || port < 1 || port > 65535) return;
    infraFrontendTargetsDraft = [...infraFrontendTargetsDraft, { id: crypto.randomUUID(), name, host: "localhost", port }];
    newTargetName = ""; newTargetPort = "";
  }
  function removeFrontendTarget(id: string) {
    infraFrontendTargetsDraft = infraFrontendTargetsDraft.filter(t => t.id !== id);
  }

  let canvasExpanded = $state(false);
  let sidebarCollapsed = $state(false);

  $effect(() => { sidebarCollapsed = canvasExpanded; });

  // ── Filter-derived counts ──────────────────────────────────────
  const directPrototypeFilter = $derived(resolveDirectPrototypeFilter(filters.tagFilter));
  const tagOnlyFilterQuery = $derived(extractTagOnlyFilterQuery(filters.tagFilter));

  function matchesPrototypeResourceFilter(
    kind: "gateway" | "eventbridge" | "topic" | "queue" | "dynamodb" | "function" | "secret" | "bucket",
    tags?: Record<string, string>,
  ): boolean {
    if (directPrototypeFilter && directPrototypeFilter.kind !== kind) return false;
    if (!tagOnlyFilterQuery) return true;
    return matchesTagFilter(tags, tagOnlyFilterQuery);
  }

  function matchesPrototypeInfraFilter(kind: string): boolean {
    if (!directPrototypeFilter) return true;
    if (directPrototypeFilter.kind !== "infra") return false;
    if (!directPrototypeFilter.infraKind) return true;
    return normalizeTopologyInfraKind(kind) === directPrototypeFilter.infraKind;
  }

  const countGateways    = $derived((dashboard.data?.gateways ?? []).filter(g => matchesPrototypeResourceFilter("gateway", g.tags)).length);
  const countFunctions   = $derived((dashboard.data?.functions ?? []).filter(f => matchesPrototypeResourceFilter("function", f.tags)).length);
  const countQueues      = $derived((dashboard.data?.queues ?? []).filter(q => matchesPrototypeResourceFilter("queue", q.tags)).length);
  const countTopics      = $derived((dashboard.data?.topics ?? []).filter(t => matchesPrototypeResourceFilter("topic", t.tags)).length);
  const countDynamoTables = $derived((dashboard.data?.dynamodbTables ?? []).filter(() => matchesPrototypeResourceFilter("dynamodb")).length);
  const countSecrets     = $derived((dashboard.data?.secrets ?? []).filter(s => matchesPrototypeResourceFilter("secret", s.tags)).length);
  const countBuckets     = $derived((dashboard.data?.buckets ?? []).filter(() => matchesPrototypeResourceFilter("bucket")).length);
  const countEventBridge = $derived((dashboard.data?.eventBridgeRules ?? []).filter(() => matchesPrototypeResourceFilter("eventbridge")).length);
  const countTriggers    = $derived(directPrototypeFilter ? 0 : (dashboard.data?.eventSourceMappings ?? []).length);

  const recentTraces = $derived(dashboard.data?.recentTraces ?? []);
  const visibleInfra = $derived(
    getVisibleInfra(dashboard.data?.infrastructure ?? []).filter(p => matchesPrototypeInfraFilter(p.kind)),
  );
  const activeServiceCount = $derived(visibleInfra.length);

  const infraLegend = $derived(
    (() => {
      const counts = new Map<string, number>();
      for (const probe of visibleInfra) {
        const key = normalizeTopologyInfraKind(probe.kind);
        counts.set(key, (counts.get(key) ?? 0) + 1);
      }
      return [
        { kind: "postgresql", label: "PostgreSQL" },
        { kind: "mysql",      label: "MySQL"      },
        { kind: "redis",      label: "Redis"      },
        { kind: "http",       label: "HTTP"       },
        { kind: "mongodb",    label: "MongoDB"    },
        { kind: "docker",     label: "Docker"     },
        { kind: "default",    label: "Other"      },
      ]
        .map(e => ({ ...e, count: counts.get(e.kind) ?? 0, color: infraKindCssVar(e.kind) }))
        .filter(e => e.count > 0);
    })(),
  );

  const connectionStatus = $derived(
    dashboard.error ? "error" : dashboard.loading ? "loading" : dashboard.data ? "ok" : "idle",
  ) as "ok" | "loading" | "error" | "idle";

  // ── Flat sparkline strips (shown when canvas is expanded) ──────
  const BUCKET_N = 12;
  const BUCKET_MS = 15 * 60 * 1000;

  const traceBuckets = $derived(
    (() => {
      const now = Date.now();
      const windowStart = now - BUCKET_N * BUCKET_MS;
      const buckets = Array.from({ length: BUCKET_N }, (_, i) => ({
        traces: [] as typeof recentTraces,
        startMs: windowStart + i * BUCKET_MS,
        endMs: windowStart + (i + 1) * BUCKET_MS,
      }));
      for (const t of recentTraces) {
        const ms = new Date(t.startedAt).getTime();
        if (isNaN(ms) || ms < windowStart) continue;
        const idx = Math.min(Math.floor((ms - windowStart) / BUCKET_MS), BUCKET_N - 1);
        buckets[idx].traces.push(t);
      }
      return buckets;
    })(),
  );

  function normBars(values: number[]): SparkBarData[] {
    const max = Math.max(...values, 1);
    return values.map((v, i) => ({ h: Math.round((v / max) * 100), current: i === BUCKET_N - 1, label: "" }));
  }

  const flatBars = $derived([
    { bars: normBars(traceBuckets.map(b => b.traces.length)), color: "var(--color-accent)" },
    { bars: normBars(traceBuckets.map(b => b.traces.length ? Math.round(b.traces.reduce((s,t)=>s+t.durationMs,0)/b.traces.length) : 0)), color: "var(--color-text-muted)" },
    { bars: normBars(traceBuckets.map(b => { if (!b.traces.length) return 0; const s=[...b.traces].map(t=>t.durationMs).sort((a,z)=>a-z); return s[Math.min(Math.floor(s.length*0.95),s.length-1)]; })), color: "var(--color-text-muted)" },
    { bars: normBars(traceBuckets.map(b => b.traces.filter(t=>t.status>=500).length)), color: "var(--color-red)" },
  ]);

  // ── Navigation config ──────────────────────────────────────────
  const navSections = $derived([
    {
      id: "overview",
      label: "Overview",
      items: [{ id: "overview", label: "Overview", icon: SquaresFourIcon, count: null }],
    },
    {
      id: "infra",
      label: "Infra",
      items: [
        { id: "gateways",     label: "Gateways",    icon: GlobeHemisphereWestIcon, count: countGateways    },
        { id: "functions",    label: "Functions",   icon: LightningIcon,            count: countFunctions   },
        { id: "queues",       label: "Queues",      icon: ChatCircleIcon,           count: countQueues      },
        { id: "dynamodb",     label: "DynamoDB",    icon: DatabaseIcon,             count: countDynamoTables },
        { id: "sns",          label: "SNS",         icon: BellIcon,                 count: countTopics      },
        { id: "secrets",      label: "Secrets",     icon: KeyIcon,                  count: countSecrets     },
        { id: "triggers",     label: "Triggers",    icon: ArrowsClockwiseIcon,      count: countTriggers    },
        { id: "eventbridge",  label: "EventBridge", icon: BridgeIcon,               count: countEventBridge },
        { id: "storage",      label: "Storage",     icon: HardDriveIcon,            count: countBuckets     },
      ],
    },
    {
      id: "observability",
      label: "Observability",
      items: [
        { id: "logs", label: "Logs",   icon: ScrollIcon,    count: null },
        { id: "xray", label: "Traces", icon: DetectiveIcon, count: null },
      ],
    },
    {
      id: "tools",
      label: "Tools",
      items: [{ id: "chaos", label: "Chaos", icon: ShieldWarningIcon, count: null }],
    },
  ]);
</script>

<div class="flex h-svh overflow-hidden bg-background font-mono text-foreground">
  <!-- ══════════════════════════════════════════════ SIDEBAR ══ -->
  <AppSidebar
    {navSections}
    {activeTab}
    bind:sidebarCollapsed
    {connectionStatus}
    region={dashboard.data?.config.region}
    pollingIntervalSeconds={uiSettings.pollingIntervalSeconds}
    onSetTab={setTab}
    onOpenSettings={openSettings}
  />

  <!-- ═══════════════════════════════════════════════════ MAIN ══ -->
  {#if activeTab === "overview"}
  <main class="flex min-w-0 flex-1 flex-col overflow-hidden">
    <div class="flex flex-1 flex-col overflow-hidden px-6 py-5">
      <!-- Status bar -->
      <div class="flex flex-wrap items-center gap-4 pb-2">
        <button
          type="button"
          onclick={() => (sidebarCollapsed = !sidebarCollapsed)}
          aria-label={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
          class="flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground/50 transition-colors hover:bg-muted/60 hover:text-foreground"
        >
          <SidebarSimpleIcon size={14} weight={sidebarCollapsed ? "regular" : "fill"} />
        </button>

        <TagFilter onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />

        {#if canvasExpanded}
          <div class="flex flex-1 flex-col gap-[3px]">
            {#each flatBars as metric}
              <div class="flex w-1/2 gap-px">
                {#each metric.bars as bar, i (i)}
                  <div
                    class="h-[2px] flex-1"
                    style="background:{metric.color};opacity:{bar.current
                      ? Math.max((bar.h / 100) * 0.5, 0.12)
                      : bar.h > 0
                        ? 0.15 + (bar.h / 100) * 0.75
                        : 0.1}"
                  ></div>
                {/each}
              </div>
            {/each}
          </div>
        {/if}

        <div class="ml-auto flex shrink-0 items-center gap-1 text-[11px] text-muted-foreground/50">
          <span class="inline-block h-[5px] w-[5px] rounded-full bg-primary/70"></span>
          Poll {uiSettings.pollingIntervalSeconds}s
        </div>
      </div>

      {#if !canvasExpanded}
        <OverviewPulse {recentTraces} {activeServiceCount} {infraLegend} />
      {/if}

      <!-- Hero grid: topology + feed -->
      <div class="mt-4 grid min-h-0 flex-1 gap-4 {canvasExpanded ? 'grid-cols-1' : 'grid-cols-[1fr_300px]'}">
        <div class="flex min-h-0 flex-col">
          <div class="flex-1 overflow-hidden rounded-[5px]">
            <TopologyCanvas
              {canvasExpanded}
              onNavigate={setTab}
              onExpandedChange={(expanded) => (canvasExpanded = expanded)}
            />
          </div>
        </div>

        {#if !canvasExpanded}
          <ActivityFeed traces={recentTraces} onOpenTrace={openTrace} />
        {/if}
      </div>
    </div>
  </main>
  {:else}
  <main class="min-w-0 flex-1 overflow-y-auto px-6 py-5">
    {#if activeTab === "gateways"}
      <APIGatewaysSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
    {:else if activeTab === "functions"}
      <FunctionsSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
    {:else if activeTab === "queues"}
      <QueuesSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
    {:else if activeTab === "dynamodb"}
      <DynamoDBSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
    {:else if activeTab === "sns"}
      <SNSSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
    {:else if activeTab === "secrets"}
      <SecretsSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
    {:else if activeTab === "triggers"}
      <TriggersSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
    {:else if activeTab === "eventbridge"}
      <EventBridgeSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
    {:else if activeTab === "storage"}
      <StorageSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
    {:else if activeTab === "logs"}
      <LogsSection
        initialGroup={logsInitialGroup}
        initialTimestamp={logsInitialTimestamp}
        {sidebarCollapsed}
        onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
      />
    {:else if activeTab === "xray"}
      <XraySection
        initialTraceId={xrayInitialTraceId}
        {sidebarCollapsed}
        onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
      />
    {:else if activeTab === "chaos"}
      <ChaosSection
        gateways={dashboard.data?.gateways ?? []}
        {sidebarCollapsed}
        onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
      />
    {/if}
  </main>
  {/if}
</div>

<SettingsDialog
  open={settingsOpen}
  bind:pollingIntervalDraft
  bind:themeModeDraft
  bind:persistenceDraft
  bind:schemaSourceDirDraft
  bind:logRetentionMinutesDraft
  bind:infraEnabledKindsDraft
  bind:infraFrontendTargetsDraft
  bind:newTargetName
  bind:newTargetPort
  infraKinds={INFRA_KINDS}
  instanceInfo={dashboard.data?.config ?? null}
  {sanitizeSchemaSourceDir}
  onClose={closeSettings}
  onSave={applySettings}
  onAddFrontendTarget={addFrontendTarget}
  onRemoveFrontendTarget={removeFrontendTarget}
/>
