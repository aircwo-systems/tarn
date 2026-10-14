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
    ShieldWarningIcon,
    BridgeIcon,
    ScrollIcon,
    DetectiveIcon,
    StackIcon,
    DatabaseIcon,
    FlowArrowIcon,
    CubeIcon,
    PlugsConnectedIcon,
    FingerprintIcon,
  } from "phosphor-svelte";
  import { onMount, type Component } from "svelte";

  import AppSidebar, { type NavSection } from "$lib/components/layout/app-sidebar.svelte";
  import CommandPalette from "$lib/components/layout/command-palette.svelte";
  import { getLogPulse, startLogPulse } from "$lib/log-pulse.svelte";
  import HomeView from "$lib/components/home/home-view.svelte";
  import {
    normalizeTopologyInfraKind,
  } from "$lib/components/topology/topology-canvas-theme";

  import {
    getDashboard,
    getDashboardFilters,
    getUISettings,
    getAccountSettings,
    getVisibleInfra,
  } from "$lib/state.svelte";

  import {
    matchesResourceFilter,
    matchesResourceType,
    matchesInfrastructureFilter,
  } from "$lib/filter-utils";
  import {
    TabNavigationHistory,
    logStateFromLocation,
    tabFromHash,
    type DashboardTab,
    type LogSortOrder,
  } from "$lib/navigation-history";

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const uiSettings = getUISettings();
  const accountSettings = getAccountSettings();

  // ── Routing ─────────────────────────────────────────────────────
  const tabHistory = new TabNavigationHistory();
  let activeTab = $state<DashboardTab>("home");
  let logsInitialGroup = $state("");
  let logsInitialTimestamp = $state("");
  let logsInitialLevels = $state<string[]>([]);
  let logsInitialPattern = $state("");
  let logsInitialStream = $state("");
  let logsInitialOrder = $state<LogSortOrder>("desc");
  let xrayInitialTraceId = $state("");
  let commandPaletteOpen = $state(false);
  let homeSection = $state("");

  function readHash() {
    if (window.location.hash.split("?", 1)[0] === "#overview") {
      window.history.replaceState(null, "", "#home?section=topology");
    }
    const raw = window.location.hash.replace("#", "");
    const [tab, qs] = raw.split("?");
    homeSection = new URLSearchParams(qs ?? "").get("section") ?? "";
    const nextTab = tabFromHash(window.location.hash);
    if (nextTab) activeTab = nextTab;
    tabHistory.remember(window.location.hash);
    logsInitialGroup = "";
    logsInitialTimestamp = "";
    logsInitialLevels = [];
    logsInitialPattern = "";
    logsInitialStream = "";
    logsInitialOrder = "desc";
    xrayInitialTraceId = "";
    if (tab === "logs") {
      const logState = logStateFromLocation(window.location.hash);
      logsInitialGroup = logState.group;
      logsInitialTimestamp = logState.timestamp;
      logsInitialLevels = logState.levels;
      logsInitialPattern = logState.pattern;
      logsInitialStream = logState.stream;
      logsInitialOrder = logState.order;
    }
    if (tab === "xray" && qs) {
      const params = new URLSearchParams(qs);
      xrayInitialTraceId = params.get("trace") ?? "";
    }
  }

  function setTab(tab: string) {
    const nextTab = tabFromHash(tab);
    if (!nextTab) return;
    tabHistory.remember(window.location.hash);
    activeTab = nextTab;
    // A caller can pass "settings?section=infra" to deep link inside the tab.
    const qs = tab.replace(/^#/, "").split("?").slice(1).join("?");
    window.location.hash = qs ? `#${nextTab}?${qs}` : tabHistory.destination(nextTab);
  }

  function openTrace(traceId: string) {
    activeTab = "xray";
    xrayInitialTraceId = traceId;
    window.location.hash = `xray?trace=${encodeURIComponent(traceId)}`;
  }

  // ── Lazy sections ───────────────────────────────────────────────
  // Each tab ships as its own chunk so first load only pays for home.
  type SectionModule = { default: Component<any> };
  const sectionLoaders: Partial<Record<DashboardTab, () => Promise<SectionModule>>> = {
    gateways: () => import("$lib/components/sections/api-gateways-section.svelte"),
    functions: () => import("$lib/components/sections/functions-section.svelte"),
    ecs: () => import("$lib/components/sections/ecs-section.svelte"),
    logs: () => import("$lib/components/sections/logs-section.svelte"),
    queues: () => import("$lib/components/sections/queues-section.svelte"),
    dynamodb: () => import("$lib/components/sections/dynamodb-section.svelte"),
    sns: () => import("$lib/components/sections/sns-section.svelte"),
    secrets: () => import("$lib/components/sections/secrets-section.svelte"),
    cognito: () => import("$lib/components/sections/cognito-section.svelte"),
    triggers: () => import("$lib/components/sections/triggers-section.svelte"),
    eventbridge: () => import("$lib/components/sections/eventbridge-section.svelte"),
    stepfunctions: () => import("$lib/components/sections/stepfunctions-section.svelte"),
    storage: () => import("$lib/components/sections/storage-section.svelte"),
    services: () => import("$lib/components/sections/services-section.svelte"),
    xray: () => import("$lib/components/sections/xray-section.svelte"),
    stack: () => import("$lib/components/sections/stack-section.svelte"),
    settings: () => import("$lib/components/sections/settings-section.svelte"),
    chaos: () => import("$lib/components/sections/chaos-section.svelte"),
  };
  let loadedSections = $state.raw<Partial<Record<DashboardTab, Component<any>>>>({});
  let sectionLoadError = $state("");
  const ActiveSection = $derived(loadedSections[activeTab]);

  $effect(() => {
    const tab = activeTab;
    const load = sectionLoaders[tab];
    if (!load || loadedSections[tab]) return;
    sectionLoadError = "";
    load().then(
      (mod) => (loadedSections = { ...loadedSections, [tab]: mod.default }),
      (err) => (sectionLoadError = err instanceof Error ? err.message : String(err)),
    );
  });

  const logPulse = $derived(getLogPulse());

  onMount(() => {
    const stopLogPulse = startLogPulse();
    readHash();
    const handleHashChange = (event: HashChangeEvent) => {
      tabHistory.remember(new URL(event.oldURL).hash);
      readHash();
    };
    const handleGlobalKeydown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        commandPaletteOpen = !commandPaletteOpen;
      }
    };
    window.addEventListener("hashchange", handleHashChange);
    window.addEventListener("keydown", handleGlobalKeydown);
    return () => {
      window.removeEventListener("hashchange", handleHashChange);
      window.removeEventListener("keydown", handleGlobalKeydown);
      stopLogPulse();
    };
  });

  let sidebarCollapsed = $state(false);


  // ── Filter-derived counts ──────────────────────────────────────

  const countGateways    = $derived((dashboard.data?.gateways ?? []).filter(g => matchesResourceFilter("gateway", filters.tagFilter, g.tags)).length);
  const visibleFunctions = $derived((dashboard.data?.functions ?? []).filter(f => matchesResourceFilter("function", filters.tagFilter, f.tags)));
  const countFunctions   = $derived(visibleFunctions.length);
  const countECS         = $derived(!matchesResourceType("ecs", filters.tagFilter) ? 0 : ((dashboard.data?.ecs?.clusters?.length ?? 0) + (dashboard.data?.ecs?.services?.length ?? 0) + (dashboard.data?.ecs?.tasks?.length ?? 0)));
  const visibleQueues = $derived((dashboard.data?.queues ?? []).filter(q => matchesResourceFilter("queue", filters.tagFilter, q.tags)));
  const countQueues      = $derived(visibleQueues.length);
  const countTopics      = $derived((dashboard.data?.topics ?? []).filter(t => matchesResourceFilter("topic", filters.tagFilter, t.tags)).length);
  const countDynamoTables = $derived((dashboard.data?.dynamodbTables ?? []).filter(() => matchesResourceFilter("dynamodb", filters.tagFilter)).length);
  const countSecrets     = $derived((dashboard.data?.secrets ?? []).filter(s => matchesResourceFilter("secret", filters.tagFilter, s.tags)).length);
  const visiblePools     = $derived((dashboard.data?.cognitoPools ?? []).filter(p => matchesResourceFilter("userpool", filters.tagFilter, p.tags)));
  const countPools       = $derived(visiblePools.length);
  const countBuckets     = $derived((dashboard.data?.buckets ?? []).filter(() => matchesResourceFilter("bucket", filters.tagFilter)).length);
  const countEventBridge = $derived((dashboard.data?.eventBridgeRules ?? []).filter(() => matchesResourceFilter("eventbridge", filters.tagFilter)).length);
  const countStateMachines = $derived(!matchesResourceType("stepfunctions", filters.tagFilter) ? 0 : (dashboard.data?.stateMachines ?? []).length);
  const countTriggers    = $derived(!matchesResourceType("trigger", filters.tagFilter) ? 0 : (dashboard.data?.eventSourceMappings ?? []).length);

  const recentTraces = $derived(dashboard.data?.recentTraces ?? []);
  const visibleInfra = $derived(
    getVisibleInfra(dashboard.data?.infrastructure ?? []).filter(p => matchesInfrastructureFilter(normalizeTopologyInfraKind(p.kind), filters.tagFilter)),
  );
  const activeServiceCount = $derived(visibleInfra.length);
  const sidebarServices = $derived(visibleInfra.map((probe) => {
    const id = `${probe.kind}-${probe.host}-${probe.port}`;
    return {
      id,
      name: probe.name,
      kind: probe.kind,
      status: probe.status,
      latencyMs: probe.latencyMs,
      linkedFunctions: new Set((dashboard.data?.connections ?? [])
        .filter((connection) => connection.targetId === id)
        .map((connection) => connection.sourceFunction)).size,
    };
  }));

  // ── Navigation config ──────────────────────────────────────────
  const navSections = $derived<NavSection[]>([
    {
      id: "home",
      label: "Home",
      items: [
        {
          id: "home", label: "Home", icon: SquaresFourIcon, count: null,
          widget: {
            kind: "summary",
            rows: [
              { label: "AWS resources", value: countGateways + countFunctions + countECS + countQueues + countTopics + countDynamoTables + countSecrets + countPools + countBuckets + countEventBridge + countStateMachines + countTriggers },
              { label: "Last sync", value: dashboard.lastRefresh || "Waiting" },
              { label: "Services", value: `${visibleInfra.filter((probe) => probe.status === "connected").length} / ${activeServiceCount} reachable`, minWidth: 340 },
            ],
            note: dashboard.error ? "Refresh failed · showing last known data" : undefined,
          },
        },
      ],
    },
    {
      id: "infra",
      label: "Infra",
      items: [
        { id: "gateways",     label: "Gateways",       icon: GlobeHemisphereWestIcon, count: countGateways      },
        {
          id: "functions", label: "Functions", icon: LightningIcon, count: countFunctions,
          widget: {
            kind: "summary",
            rows: [
              {
                label: "Active", value: `${visibleFunctions.filter((fn) => fn.state.toLowerCase() === "active").length} / ${countFunctions}`,
                blocks: visibleFunctions.map((fn) => {
                  const state = fn.state.toLowerCase();
                  return {
                    id: fn.name,
                    title: `${fn.name} · ${state} · ${fn.invocations === undefined ? "invocations unavailable" : `${fn.invocations} invocations`}`,
                    color: state === "failed" ? "var(--accent-red)" : state === "active" ? "var(--accent-green)" : "var(--accent-amber)",
                    dimmed: state !== "active" && state !== "failed",
                  };
                }),
              },
              { label: "Invocations", value: visibleFunctions.some((fn) => fn.invocations !== undefined) ? visibleFunctions.reduce((sum, fn) => sum + (fn.invocations ?? 0), 0) : "--" },
            ],
            note: dashboard.error ? "Refresh failed · showing last known data" : undefined,
          },
        },
        { id: "ecs",          label: "ECS",            icon: CubeIcon,                 count: countECS           },
        {
          id: "queues", label: "Queues", icon: ChatCircleIcon, count: countQueues,
          widget: {
            kind: "summary",
            rows: [
              {
                label: "Waiting", value: `≈ ${visibleQueues.reduce((sum, queue) => sum + queue.approxVisible, 0)}`,
                blocks: visibleQueues.map((queue) => ({
                  id: queue.name,
                  title: `${queue.name} · ≈ ${queue.approxVisible} waiting · ≈ ${queue.approxInFlight} in flight${queue.approxDelayed > 0 ? ` · ≈ ${queue.approxDelayed} delayed` : ""}${queue.approxStale > 0 ? ` · ≈ ${queue.approxStale} stale` : ""}${queue.disruptEnabled ? " · disruptor armed" : ""}`,
                  color: queue.disruptEnabled || queue.approxStale > 0 ? "var(--accent-red)" : "var(--accent-amber)",
                  dimmed: !queue.disruptEnabled && !(queue.approxStale > 0 || queue.approxVisible > 0 || queue.approxInFlight > 0 || queue.approxDelayed > 0),
                })),
              },
              { label: "In flight", value: `≈ ${visibleQueues.reduce((sum, queue) => sum + queue.approxInFlight, 0)}` },
            ],
            note: dashboard.error ? "Refresh failed · showing last known data" : undefined,
          },
        },
        { id: "dynamodb",     label: "DynamoDB",       icon: DatabaseIcon,             count: countDynamoTables  },
        { id: "sns",          label: "SNS",            icon: BellIcon,                 count: countTopics        },
        { id: "secrets",      label: "Secrets",        icon: KeyIcon,                  count: countSecrets       },
        { id: "cognito",      label: "Cognito",        icon: FingerprintIcon,  count: countPools         },
        { id: "triggers",     label: "Triggers",       icon: ArrowsClockwiseIcon,      count: countTriggers      },
        { id: "eventbridge",  label: "EventBridge",    icon: BridgeIcon,               count: countEventBridge   },
        { id: "stepfunctions", label: "Step Functions", icon: FlowArrowIcon,           count: countStateMachines },
        { id: "storage",      label: "Storage",        icon: HardDriveIcon,            count: countBuckets       },
        {
          id: "services", label: "Services", icon: PlugsConnectedIcon, count: activeServiceCount,
          widget: { kind: "services", services: sidebarServices, stale: !!dashboard.error },
        },
      ],
    },
    {
      id: "observability",
      label: "Observability",
      items: [
        {
          id: "logs", label: "Logs", icon: ScrollIcon, count: null, pulse: logPulse,
          widget: { kind: "logs", pulse: logPulse, groups: dashboard.data?.counts.logGroups ?? 0 },
        },
        {
          id: "xray", label: "Traces", icon: DetectiveIcon, count: null,
          widget: {
            kind: "summary",
            rows: [
              { label: "Requests", value: recentTraces.length },
              { label: "5xx responses", value: recentTraces.filter((trace) => trace.status >= 500).length, tone: recentTraces.some((trace) => trace.status >= 500) ? "error" : undefined },
            ],
            note: "Recent request sample",
          },
        },
        { id: "stack", label: "Stack", icon: StackIcon, count: null },
      ],
    },
    {
      id: "tools",
      label: "Tools",
      items: [{ id: "chaos", label: "Chaos", icon: ShieldWarningIcon, count: null }],
    },
  ]);
</script>

<div class="flex h-svh overflow-hidden bg-[var(--bg-app)] font-sans text-foreground">
  <a
    href="#main-stage-content"
    class="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:px-3 focus:py-1.5 focus:bg-primary focus:text-primary-foreground focus:rounded-md focus:shadow-lg focus:outline-none focus:ring-2 focus:ring-ring font-medium text-xs"
  >
    Skip to main content
  </a>
  <!-- ══════════════════════════════════════════════ SIDEBAR ══ -->
  <AppSidebar
    {navSections}
    {activeTab}
    bind:sidebarCollapsed
    collapsedSidebarMode={uiSettings.collapsedSidebarMode}
    pollingIntervalSeconds={uiSettings.pollingIntervalSeconds}
    activeAccountId={accountSettings.activeAccountId}
    activeAccountLabel={accountSettings.knownAccounts.find((account) => account.id === accountSettings.activeAccountId)?.label}
    onSetTab={setTab}
    onOpenSettings={() => setTab("settings")}
    onOpenCommandPalette={() => (commandPaletteOpen = true)}
    hideSearch={activeTab === "home"}
  />

  <!-- ═══════════════════════════════════════════════════ MAIN ══ -->
  {#key `${accountSettings.activeAccountId}:${activeTab}`}
    {#if activeTab === "home"}
    <main id="main-stage-content" tabindex="-1" class="tab-content-view main-stage min-w-0 flex-1 overflow-y-auto outline-none" class:sidebar-collapsed={sidebarCollapsed && uiSettings.collapsedSidebarMode === "hidden"}>
      <HomeView
        topologyRequested={homeSection === "topology"}
        {sidebarCollapsed}
        onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
        onNavigate={setTab}
        onOpenTrace={openTrace}
        onOpenPalette={() => (commandPaletteOpen = true)}
      />
    </main>
    {:else}
    <main id="main-stage-content" tabindex="-1" class="tab-content-view main-stage min-w-0 flex-1 px-6 py-5 outline-none {activeTab === 'logs' ? 'flex flex-col overflow-hidden' : 'overflow-y-auto'}" class:sidebar-collapsed={sidebarCollapsed && uiSettings.collapsedSidebarMode === "hidden"}>
      {#if sectionLoadError}
        <p class="text-sm text-[var(--accent-red)]">Failed to load section: {sectionLoadError}</p>
      {:else if !ActiveSection}
        <!-- section chunk loading -->
      {:else if activeTab === "gateways"}
        <ActiveSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
      {:else if activeTab === "functions"}
        <ActiveSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
      {:else if activeTab === "ecs"}
        <ActiveSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
      {:else if activeTab === "logs"}
        <ActiveSection
          initialGroup={logsInitialGroup}
          initialTimestamp={logsInitialTimestamp}
          initialLevels={logsInitialLevels}
          initialPattern={logsInitialPattern}
          initialStream={logsInitialStream}
          initialOrder={logsInitialOrder}
          {sidebarCollapsed}
          onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
        />
      {:else if activeTab === "queues"}
        <ActiveSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
      {:else if activeTab === "dynamodb"}
        <ActiveSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
      {:else if activeTab === "sns"}
        <ActiveSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
      {:else if activeTab === "secrets"}
        <ActiveSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
      {:else if activeTab === "cognito"}
        <ActiveSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
      {:else if activeTab === "triggers"}
        <ActiveSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
      {:else if activeTab === "eventbridge"}
        <ActiveSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
      {:else if activeTab === "stepfunctions"}
        <ActiveSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
      {:else if activeTab === "storage"}
        <ActiveSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} />
      {:else if activeTab === "services"}
        <ActiveSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} onNavigate={setTab} />
      {:else if activeTab === "xray"}
        <ActiveSection
          initialTraceId={xrayInitialTraceId}
          {sidebarCollapsed}
          onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
          onNavigate={setTab}
        />
      {:else if activeTab === "stack"}
        <ActiveSection {sidebarCollapsed} onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)} onNavigate={setTab} />
      {:else if activeTab === "settings"}
        <ActiveSection
          instanceInfo={dashboard.data?.config ?? null}
          onNavigate={setTab}
          {sidebarCollapsed}
          onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
        />
      {:else if activeTab === "chaos"}
        <ActiveSection
          gateways={dashboard.data?.gateways ?? []}
          {sidebarCollapsed}
          onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
        />
      {/if}
    </main>
    {/if}
  {/key}

  <CommandPalette
    bind:open={commandPaletteOpen}
    onNavigate={(tab) => setTab(tab)}
  />
</div>


<style>
  :global(.tab-content-view) {
    animation: tabFadeIn 140ms cubic-bezier(0.16, 1, 0.3, 1);
  }

  @keyframes tabFadeIn {
    from {
      opacity: 0.85;
      transform: translateY(2px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
</style>
