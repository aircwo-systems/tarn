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
    GearIcon,
  } from "phosphor-svelte";
  import { onMount } from "svelte";

  import TarnLogo from "$lib/components/common/tarn-logo.svelte";
  import TopologyCanvas from "$lib/components/topology/topology-canvas.svelte";
  import ThemeToggle from "$lib/components/layout/theme-toggle.svelte";
  import SettingsDialog from "$lib/components/layout/settings-dialog.svelte";
  import APIGatewaysSection from "$lib/components/sections/api-gateways-section.svelte";
  import FunctionsSection from "$lib/components/sections/functions-section.svelte";
  import QueuesSection from "$lib/components/sections/queues-section.svelte";
  import SNSSection from "$lib/components/sections/sns-section.svelte";
  import SecretsSection from "$lib/components/sections/secrets-section.svelte";
  import TriggersSection from "$lib/components/sections/triggers-section.svelte";
  import EventBridgeSection from "$lib/components/sections/eventbridge-section.svelte";
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
    setDashboardTagFilter,
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

  import type { RequestTrace } from "$lib/types";
  import { Tooltip } from "$lib/components/ui/simple-tooltip";

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const uiSettings = getUISettings();
  const infraSettings = getInfraSettings();

  // ── Routing ─────────────────────────────────────────────────────
  const validTabs = ["overview","gateways","chaos","functions","queues","sns","secrets","triggers","eventbridge","storage","logs","xray"];
  let activeTab = $state("overview");
  let logsInitialGroup = $state("");
  let logsInitialTimestamp = $state("");

  function readHash() {
    const raw = window.location.hash.replace("#", "");
    const [tab, qs] = raw.split("?");
    if (validTabs.includes(tab)) activeTab = tab;
    if (tab === "logs" && qs) {
      const params = new URLSearchParams(qs);
      logsInitialGroup = params.get("group") ?? "";
      logsInitialTimestamp = params.get("ts") ?? "";
    }
  }

  function setTab(tab: string) {
    activeTab = tab;
    window.location.hash = tab;
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
  let tagDraft = $state("");
  let tagTokens = $state<string[]>(parseFilterTokens(filters.tagFilter));
  let canvasExpanded = $state(false);
  let sidebarCollapsed = $state(false);

  $effect(() => {
    const nextTokens = parseFilterTokens(filters.tagFilter);
    if (
      nextTokens.length !== tagTokens.length ||
      nextTokens.some((token, index) => token !== tagTokens[index])
    ) {
      tagTokens = nextTokens;
    }
    tagDraft = "";
  });
  // Mirror sidebar collapse state to canvas expansion
  $effect(() => {
    sidebarCollapsed = canvasExpanded;
  });
  function applyFilter() {
    const nextTokens = mergeFilterTokens(tagTokens, tagDraft);
    tagTokens = nextTokens;
    tagDraft = "";
    setDashboardTagFilter(nextTokens.join(" "));
  }
  function clearFilter() {
    tagDraft = "";
    tagTokens = [];
    setDashboardTagFilter("");
  }

  function removeFilterToken(token: string) {
    const nextTokens = tagTokens.filter((entry) => entry !== token);
    tagTokens = nextTokens;
    setDashboardTagFilter(nextTokens.join(" "));
  }

  function handleFilterKeydown(event: KeyboardEvent) {
    if (event.key === "Enter") {
      event.preventDefault();
      applyFilter();
      return;
    }

    if (event.key === "Backspace" && !tagDraft && tagTokens.length > 0) {
      event.preventDefault();
      removeFilterToken(tagTokens[tagTokens.length - 1]);
    }
  }

  type DirectPrototypeFilter =
    | {
        kind:
          | "gateway"
          | "eventbridge"
          | "topic"
          | "queue"
          | "function"
          | "secret"
          | "bucket"
          | "extension"
          | "infra";
        infraKind?: string;
      }
    | null;

  const directPrototypeFilter = $derived(resolveDirectPrototypeFilter(filters.tagFilter));
  const tagOnlyFilterQuery = $derived(extractTagOnlyFilterQuery(filters.tagFilter));

  // ── Resource counts ───────────────────────────────────────────
  const countGateways = $derived(
    (dashboard.data?.gateways ?? []).filter((g) =>
      matchesPrototypeResourceFilter("gateway", g.tags),
    ).length,
  );
  const countFunctions = $derived(
    (dashboard.data?.functions ?? []).filter((f) =>
      matchesPrototypeResourceFilter("function", f.tags),
    ).length,
  );
  const countQueues = $derived(
    (dashboard.data?.queues ?? []).filter((q) =>
      matchesPrototypeResourceFilter("queue", q.tags),
    ).length,
  );
  const countTopics = $derived(
    (dashboard.data?.topics ?? []).filter((t) =>
      matchesPrototypeResourceFilter("topic", t.tags),
    ).length,
  );
  const countSecrets = $derived(
    (dashboard.data?.secrets ?? []).filter((s) =>
      matchesPrototypeResourceFilter("secret", s.tags),
    ).length,
  );
  const countBuckets = $derived(
    (dashboard.data?.buckets ?? []).filter(() =>
      matchesPrototypeResourceFilter("bucket"),
    ).length,
  );
  const countEventBridge = $derived(
    (dashboard.data?.eventBridgeRules ?? []).filter(() =>
      matchesPrototypeResourceFilter("eventbridge"),
    ).length,
  );
  const countTriggers = $derived(
    directPrototypeFilter ? 0 : (dashboard.data?.eventSourceMappings ?? []).length,
  );

  const recentTraces = $derived(dashboard.data?.recentTraces ?? []);
  const visibleInfra = $derived(
    getVisibleInfra(dashboard.data?.infrastructure ?? []).filter((probe) =>
      matchesPrototypeInfraFilter(probe.kind),
    ),
  );
  const infraLegend = $derived(
    (() => {
      const counts = new Map<string, number>();
      for (const probe of visibleInfra) {
        const key = normalizeTopologyInfraKind(probe.kind);
        counts.set(key, (counts.get(key) ?? 0) + 1);
      }

      return [
        { kind: "postgresql", label: "PostgreSQL" },
        { kind: "mysql", label: "MySQL" },
        { kind: "redis", label: "Redis" },
        { kind: "http", label: "HTTP" },
        { kind: "mongodb", label: "MongoDB" },
        { kind: "docker", label: "Docker" },
        { kind: "default", label: "Other" },
      ]
        .map((entry) => ({
          ...entry,
          count: counts.get(entry.kind) ?? 0,
          color: infraKindCssVar(entry.kind),
        }))
        .filter((entry) => entry.count > 0);
    })(),
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

  function parseFilterTokens(value: string): string[] {
    return value
      .trim()
      .split(/\s+/)
      .map((token) => token.trim())
      .filter(Boolean);
  }

  function mergeFilterTokens(currentTokens: string[], draft: string): string[] {
    const next = [...currentTokens];
    for (const token of parseFilterTokens(draft)) {
      if (!next.includes(token)) next.push(token);
    }
    return next;
  }

  function resolveDirectPrototypeFilter(query: string): DirectPrototypeFilter {
    for (const token of parseFilterTokens(query)) {
      const normalized = token.toLowerCase();
      switch (normalized) {
        case "gateway":
        case "gateways":
        case "api":
        case "apigateway":
        case "api-gateway":
          return { kind: "gateway" };
        case "eventbridge":
        case "event-bridge":
        case "schedule":
        case "schedules":
        case "rule":
        case "rules":
          return { kind: "eventbridge" };
        case "topic":
        case "topics":
        case "sns":
          return { kind: "topic" };
        case "queue":
        case "queues":
        case "sqs":
          return { kind: "queue" };
        case "lambda":
        case "lambdas":
        case "function":
        case "functions":
          return { kind: "function" };
        case "secret":
        case "secrets":
          return { kind: "secret" };
        case "bucket":
        case "buckets":
        case "s3":
        case "storage":
          return { kind: "bucket" };
        case "extension":
        case "extensions":
        case "cache":
          return { kind: "extension" };
        case "external":
        case "externals":
        case "infra":
        case "infrastructure":
          return { kind: "infra" };
        case "postgres":
        case "postgresql":
          return { kind: "infra", infraKind: "postgresql" };
        case "mysql":
          return { kind: "infra", infraKind: "mysql" };
        case "redis":
          return { kind: "infra", infraKind: "redis" };
        case "mongo":
        case "mongodb":
          return { kind: "infra", infraKind: "mongodb" };
        case "docker":
          return { kind: "infra", infraKind: "docker" };
        case "http":
        case "https":
          return { kind: "infra", infraKind: "http" };
      }
    }
    return null;
  }

  function extractTagOnlyFilterQuery(query: string): string {
    return parseFilterTokens(query)
      .filter((token) => !resolveDirectPrototypeFilter(token))
      .join(" ");
  }

  function matchesPrototypeResourceFilter(
    kind:
      | "gateway"
      | "eventbridge"
      | "topic"
      | "queue"
      | "function"
      | "secret"
      | "bucket",
    tags?: Record<string, string>,
  ): boolean {
    if (directPrototypeFilter && directPrototypeFilter.kind !== kind) {
      return false;
    }
    if (!tagOnlyFilterQuery) return true;
    return matchesTagFilter(tags, tagOnlyFilterQuery);
  }

  function matchesPrototypeInfraFilter(kind: string): boolean {
    if (!directPrototypeFilter) return true;
    if (directPrototypeFilter.kind !== "infra") return false;
    if (!directPrototypeFilter.infraKind) return true;
    return normalizeTopologyInfraKind(kind) === directPrototypeFilter.infraKind;
  }

  const activeServiceCount = $derived(visibleInfra.length);

  // ── Pulse metrics ─────────────────────────────────────────────
  const avgLatency = $derived(
    recentTraces.length > 0
      ? Math.round(
          recentTraces.reduce((s, t) => s + t.durationMs, 0) /
            recentTraces.length,
        )
      : 0,
  );

  const p95Latency = $derived(
    (() => {
      if (!recentTraces.length) return 0;
      const sorted = [...recentTraces]
        .map((t) => t.durationMs)
        .sort((a, b) => a - b);
      return sorted[
        Math.min(Math.floor(sorted.length * 0.95), sorted.length - 1)
      ];
    })(),
  );

  const errorCount = $derived(
    recentTraces.filter((t) => t.status >= 500).length,
  );

  // Count traces with startedAt within the last 60 s — true req/min
  const throughput = $derived(
    (() => {
      const cutoff = Date.now() - 60_000;
      return recentTraces.filter((t) => {
        const ms = new Date(t.startedAt).getTime();
        return !isNaN(ms) && ms >= cutoff;
      }).length;
    })(),
  );

  // ── Sparklines: 12 × 15-min buckets = 3 h rolling window ─────
  const BUCKET_N = 12;
  const BUCKET_MS = 15 * 60 * 1000;

  interface SparkBar {
    h: number;
    current: boolean;
    label: string;
  }

  function fmtBucketLabel(startMs: number, endMs: number): string {
    const fmt = (ms: number) => {
      const d = new Date(ms);
      return `${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`;
    };
    return `${fmt(startMs)}–${fmt(endMs)}`;
  }

  // Bucket all recent traces into N 15-min windows, newest = last slot
  const traceBuckets = $derived(
    (() => {
      const now = Date.now();
      const windowStart = now - BUCKET_N * BUCKET_MS;
      const buckets: {
        traces: RequestTrace[];
        startMs: number;
        endMs: number;
      }[] = Array.from({ length: BUCKET_N }, (_, i) => ({
        traces: [],
        startMs: windowStart + i * BUCKET_MS,
        endMs: windowStart + (i + 1) * BUCKET_MS,
      }));
      for (const t of recentTraces) {
        const ms = new Date(t.startedAt).getTime();
        if (isNaN(ms) || ms < windowStart) continue;
        const idx = Math.min(
          Math.floor((ms - windowStart) / BUCKET_MS),
          BUCKET_N - 1,
        );
        buckets[idx].traces.push(t);
      }
      return buckets;
    })(),
  );

  function normBars(values: number[], labels: string[]): SparkBar[] {
    const max = Math.max(...values, 1);
    return values.map((v, i) => ({
      h: Math.round((v / max) * 100),
      current: i === BUCKET_N - 1,
      label: labels[i],
    }));
  }

  const sparkThru = $derived(
    normBars(
      traceBuckets.map((b) => b.traces.length),
      traceBuckets.map(
        (b) => `${b.traces.length} req · ${fmtBucketLabel(b.startMs, b.endMs)}`,
      ),
    ),
  );

  const sparkLatency = $derived(
    normBars(
      traceBuckets.map((b) => {
        if (!b.traces.length) return 0;
        return Math.round(
          b.traces.reduce((s, t) => s + t.durationMs, 0) / b.traces.length,
        );
      }),
      traceBuckets.map((b) => {
        if (!b.traces.length)
          return `no requests · ${fmtBucketLabel(b.startMs, b.endMs)}`;
        const avg = Math.round(
          b.traces.reduce((s, t) => s + t.durationMs, 0) / b.traces.length,
        );
        return `avg ${avg}ms · ${fmtBucketLabel(b.startMs, b.endMs)}`;
      }),
    ),
  );

  const sparkP95 = $derived(
    normBars(
      traceBuckets.map((b) => {
        if (!b.traces.length) return 0;
        const sorted = b.traces.map((t) => t.durationMs).sort((a, z) => a - z);
        return sorted[
          Math.min(Math.floor(sorted.length * 0.95), sorted.length - 1)
        ];
      }),
      traceBuckets.map((b) => {
        if (!b.traces.length)
          return `no requests · ${fmtBucketLabel(b.startMs, b.endMs)}`;
        const sorted = b.traces.map((t) => t.durationMs).sort((a, z) => a - z);
        const p95 =
          sorted[Math.min(Math.floor(sorted.length * 0.95), sorted.length - 1)];
        return `p95 ${p95}ms · ${fmtBucketLabel(b.startMs, b.endMs)}`;
      }),
    ),
  );

  const sparkErrors = $derived(
    normBars(
      traceBuckets.map((b) => b.traces.filter((t) => t.status >= 500).length),
      traceBuckets.map((b) => {
        const n = b.traces.filter((t) => t.status >= 500).length;
        return n > 0
          ? `${n} error${n !== 1 ? "s" : ""} · ${fmtBucketLabel(b.startMs, b.endMs)}`
          : `no errors · ${fmtBucketLabel(b.startMs, b.endMs)}`;
      }),
    ),
  );

  // ── Activity feed helpers ─────────────────────────────────────
  function traceLabel(t: RequestTrace): string {
    const eb = t.spans.find((s) => s.kind === "eventbridge");
    if (eb) return `EVENTBRIDGE ${eb.name}`;
    return (
      `${t.method ?? ""} ${t.path ?? ""}`.trim() || `trace:${t.id.slice(0, 8)}`
    );
  }

  function dotClass(status: number): string {
    if (status >= 500) return "bg-[var(--color-red)]";
    if (status >= 400) return "bg-[var(--color-amber)]";
    return "bg-[var(--color-accent)]";
  }

  function statusClass(status: number): string {
    if (status >= 500) return "text-[var(--color-red)]";
    if (status >= 400) return "text-[var(--color-amber)]";
    return "text-[var(--color-text-muted)]";
  }

  function spanColor(kind: string): string {
    const map: Record<string, string> = {
      gateway: "var(--color-red)",
      lambda: "var(--color-blue)",
      queue: "var(--color-amber)",
      dlq: "var(--color-red)",
      topic: "var(--color-accent)",
      eventbridge: "var(--color-accent)",
    };
    return map[kind] ?? "var(--color-text-muted)";
  }

  function chainSegs(
    t: RequestTrace,
  ): { width: number; color: string; title: string }[] {
    if (!t.spans.length) return [];
    const total = Math.max(
      t.durationMs,
      t.spans.reduce((s, sp) => s + sp.durationMs, 0),
      1,
    );
    return t.spans.map((sp) => ({
      width: Math.max(6, Math.round((sp.durationMs / total) * 80)),
      color: spanColor(sp.kind),
      title: sp.name,
    }));
  }

  function timeAgo(iso: string): string {
    const d = Date.now() - new Date(iso).getTime();
    if (isNaN(d) || d < 0) return "";
    if (d < 60000) return `${Math.round(d / 1000)}s ago`;
    if (d < 3600000) return `${Math.round(d / 60000)}m ago`;
    return `${Math.round(d / 3600000)}h ago`;
  }

  function fmtMs(ms: number): string {
    return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`;
  }

  // ── Navigation ────────────────────────────────────────────────
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
        {
          id: "gateways",
          label: "Gateways",
          icon: GlobeHemisphereWestIcon,
          count: countGateways,
        },
        {
          id: "functions",
          label: "Functions",
          icon: LightningIcon,
          count: countFunctions,
        },
        { id: "queues", label: "Queues", icon: ChatCircleIcon, count: countQueues },
        { id: "sns", label: "SNS", icon: BellIcon, count: countTopics },
        { id: "secrets", label: "Secrets", icon: KeyIcon, count: countSecrets },
        {
          id: "triggers",
          label: "Triggers",
          icon: ArrowsClockwiseIcon,
          count: countTriggers,
        },
        {
          id: "eventbridge",
          label: "EventBridge",
          icon: BridgeIcon,
          count: countEventBridge,
        },
        {
          id: "storage",
          label: "Storage",
          icon: HardDriveIcon,
          count: countBuckets,
        },
      ],
    },
    {
      id: "observability",
      label: "Observability",
      items: [
        { id: "logs", label: "Logs", icon: ScrollIcon, count: null },
        { id: "xray", label: "Traces", icon: DetectiveIcon, count: null },
      ],
    },
    {
      id: "tools",
      label: "Tools",
      items: [
        { id: "chaos", label: "Chaos", icon: ShieldWarningIcon, count: null },
      ],
    },
  ]);
</script>

<div class="flex h-svh overflow-hidden bg-background font-mono text-foreground">
  <!-- ══════════════════════════════════════════════ SIDEBAR ══ -->
  <aside
    class="flex h-full shrink-0 flex-col overflow-hidden border-r border-border transition-[width] duration-200
    {sidebarCollapsed ? 'w-11' : 'w-[196px]'}"
  >
    <!-- Brand -->
    <div
      class="flex h-[52px] shrink-0 items-center gap-2 overflow-hidden px-2.5"
    >
      <TarnLogo class="h-7 w-7 shrink-0" color="var(--color-primary)" />
      {#if !sidebarCollapsed}
        <div class="min-w-0">
          <p
            class="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground/60"
          >
            Tarn
          </p>
          <p class="truncate text-[13px] font-semibold text-foreground">
            Rack Console
          </p>
        </div>
      {/if}
    </div>

    <div class="h-px bg-border"></div>

    <!-- Nav -->
    <nav class="flex flex-1 flex-col overflow-y-auto px-1.5 py-2">
      {#each navSections as section, sectionIndex (section.id)}
        <div class:mt-3={sectionIndex > 0} class="flex flex-col gap-px">
          {#if !sidebarCollapsed}
            <div class="px-2 pb-1 pt-1 text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground/40">
              {section.label}
            </div>
          {:else if sectionIndex > 0}
            <div class="mx-1.5 my-1 h-px bg-border"></div>
          {/if}

          {#each section.items as item (item.id)}
            {@const Icon = item.icon}
            {@const active = item.id === activeTab}
            <button
              type="button"
              title={sidebarCollapsed ? item.label : undefined}
              onclick={() => setTab(item.id)}
              class="flex w-full items-center rounded-[5px] px-2 py-1.5 text-[12px] transition-colors
                {sidebarCollapsed ? 'justify-center' : 'gap-2 text-left'}
                {active
                ? 'bg-primary/[0.06] text-primary'
                : 'text-muted-foreground hover:bg-white/[0.04] hover:text-foreground'}"
              aria-current={active ? "page" : undefined}
            >
              <Icon
                size={15}
                class="shrink-0 {active ? 'opacity-100' : 'opacity-55'}"
                weight={active ? "fill" : "regular"}
              />
              {#if !sidebarCollapsed}
                <span class="flex-1 truncate">{item.label}</span>
                {#if item.count !== null && item.count > 0}
                  <span
                    class="text-right text-[10px] font-medium tabular-nums {active
                      ? 'text-primary'
                      : 'text-muted-foreground/50'}"
                  >
                    {item.count}
                  </span>
                {/if}
              {/if}
            </button>
          {/each}
        </div>
      {/each}
    </nav>

    <!-- Footer -->
    <div class="border-t border-border px-1.5 py-1.5">
      {#if !sidebarCollapsed}
        <div class="flex items-center gap-1">
          <div class="flex flex-1 items-center gap-1.5 px-1 text-[10px] text-muted-foreground/50">
            <span
              class="inline-block h-1.5 w-1.5 shrink-0 rounded-full
              {connectionStatus === 'ok'
                ? 'bg-primary shadow-[0_0_5px_var(--color-primary)]'
                : connectionStatus === 'loading'
                  ? 'bg-amber-400'
                  : 'bg-destructive'}"
            ></span>
            <span class="truncate">
              {connectionStatus === "ok"
                ? `${dashboard.data?.config.region ?? "connected"}`
                : connectionStatus === "loading"
                  ? "connecting…"
                  : "error"}
            </span>
          </div>
          <ThemeToggle />
          <button
            type="button"
            onclick={openSettings}
            class="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            aria-label="Settings"
          >
            <GearIcon size={15} />
          </button>
        </div>
      {:else}
        <div class="flex flex-col items-center gap-1">
          <ThemeToggle />
          <button
            type="button"
            onclick={openSettings}
            class="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            aria-label="Settings"
          >
            <GearIcon size={15} />
          </button>
        </div>
      {/if}
    </div>
  </aside>

  <!-- ═══════════════════════════════════════════════════ MAIN ══ -->
  {#if activeTab === "overview"}
  <main class="flex min-w-0 flex-1 flex-col overflow-hidden">
    <div class="flex flex-1 flex-col overflow-hidden px-6 py-5">
      <!-- Status bar -->
      <div
        class="flex flex-wrap items-center gap-4 pb-2"
      >
        <button
          type="button"
          onclick={() => (sidebarCollapsed = !sidebarCollapsed)}
          aria-label={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
          class="flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground/50 transition-colors hover:bg-muted/60 hover:text-foreground"
        >
          <SidebarSimpleIcon
            size={14}
            weight={sidebarCollapsed ? "regular" : "fill"}
          />
        </button>

        <!-- Tag filter (always visible) -->
        <div
          class="flex min-w-1/2 shrink-0 items-center gap-1.5 rounded-[5px] border border-border bg-background/80 px-2 py-1"
          style="min-height:28px"
        >
          <MagnifyingGlassIcon
            size={12}
            class="shrink-0 text-muted-foreground/50"
          />
          <div class="flex min-w-0 flex-1 flex-wrap items-center gap-1">
            {#each tagTokens as token (token)}
              <button
                type="button"
                class="inline-flex h-5 items-center gap-1 rounded bg-muted px-1.5 text-[10px] text-foreground/85"
                onclick={() => removeFilterToken(token)}
                aria-label={`Remove filter ${token}`}
                title={`Remove filter ${token}`}
              >
                <span>{token}</span>
                <span class="text-muted-foreground/60">×</span>
              </button>
            {/each}
            <input
              type="text"
              placeholder={tagTokens.length > 0 ? "Add filter..." : "Filter topology by tag or type..."}
              bind:value={tagDraft}
              onkeydown={handleFilterKeydown}
              class="min-w-[88px] flex-1 bg-transparent text-[11px] text-foreground outline-none placeholder:text-muted-foreground/40"
            />
          </div>
          {#if tagDraft.trim()}
            <button
              onclick={applyFilter}
              class="text-[10px] text-primary hover:text-primary/80"
              >add</button
            >
          {/if}
          {#if tagDraft || tagTokens.length > 0}
            <button
              onclick={clearFilter}
              class="text-[10px] text-muted-foreground/40 hover:text-muted-foreground"
              >✕</button
            >
          {/if}
        </div>

        {#if canvasExpanded}
          <!-- Flat density strips: opacity = population, no height variation -->
          <div class="flex flex-1 flex-col gap-[3px]">
            {#each [{ bars: sparkThru, color: "var(--color-accent)" }, { bars: sparkLatency, color: "var(--color-text-muted)" }, { bars: sparkP95, color: "var(--color-text-muted)" }, { bars: sparkErrors, color: "var(--color-red)" }] as metric}
              <div class="flex w-1/2 gap-px">
                {#each metric.bars as bar (bar.label)}
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

        <div
          class="ml-auto flex shrink-0 items-center gap-1 text-[11px] text-muted-foreground/50"
        >
          <span class="inline-block h-[5px] w-[5px] rounded-full bg-primary/70"
          ></span>
          Poll {uiSettings.pollingIntervalSeconds}s
        </div>
      </div>

      <!-- Pulse strip: hidden when topology is expanded -->
      {#if !canvasExpanded}
        <div class="flex items-stretch border-b border-border py-4">
          <!-- Throughput -->
          <div
            class="flex flex-1 flex-col gap-0.5 border-r border-border px-5 first:pl-0 last:border-r-0 last:pr-0"
          >
            <div
              class="flex items-baseline gap-1 font-light leading-none"
              style="font-size:20px;letter-spacing:-0.03em;color:var(--color-accent)"
            >
              {throughput || "—"}<span
                class="text-[11px] font-medium tracking-normal text-muted-foreground/50"
                >req/min</span
              >
            </div>
            <div class="text-[11px] tracking-[0.02em] text-muted-foreground/50">
              Throughput
            </div>
            <div class="mt-1 flex h-[18px] gap-px">
              {#each sparkThru as bar, i (i)}
                <Tooltip text={bar.label} class="relative flex-1 min-w-[2px]">
                  <div
                    class="absolute bottom-0 w-full rounded-t-[1px] transition-all duration-500"
                    style="height:{Math.max(
                      bar.h,
                      4,
                    )}%;background:var(--color-accent);opacity:{bar.current
                      ? 0.22
                      : bar.h > 0
                        ? 0.5 + bar.h * 0.004
                        : 0.08}"
                  ></div>
                </Tooltip>
              {/each}
            </div>
          </div>

          <!-- Avg latency -->
          <div
            class="flex flex-1 flex-col gap-0.5 border-r border-border px-5 first:pl-0 last:border-r-0 last:pr-0"
          >
            <div
              class="flex items-baseline gap-1 font-light leading-none"
              style="font-size:20px;letter-spacing:-0.03em"
            >
              {avgLatency}<span
                class="text-[11px] font-medium tracking-normal text-muted-foreground/50"
                >ms</span
              >
            </div>
            <div class="text-[11px] tracking-[0.02em] text-muted-foreground/50">
              Avg Latency
            </div>
            <div class="mt-1 flex h-[18px] gap-px">
              {#each sparkLatency as bar, i (i)}
                <Tooltip text={bar.label} class="relative flex-1 min-w-[2px]">
                  <div
                    class="absolute bottom-0 w-full rounded-t-[1px] transition-all duration-500"
                    style="height:{Math.max(
                      bar.h,
                      4,
                    )}%;background:var(--color-text-muted);opacity:{bar.current
                      ? 0.15
                      : bar.h > 0
                        ? 0.35
                        : 0.08}"
                  ></div>
                </Tooltip>
              {/each}
            </div>
          </div>

          <!-- p95 latency -->
          <div
            class="flex flex-1 flex-col gap-0.5 border-r border-border px-5 first:pl-0 last:border-r-0 last:pr-0"
          >
            <div
              class="flex items-baseline gap-1 font-light leading-none"
              style="font-size:20px;letter-spacing:-0.03em"
            >
              {p95Latency}<span
                class="text-[11px] font-medium tracking-normal text-muted-foreground/50"
                >ms</span
              >
            </div>
            <div class="text-[11px] tracking-[0.02em] text-muted-foreground/50">
              p95 Latency
            </div>
            <div class="mt-1 flex h-[18px] gap-px">
              {#each sparkP95 as bar, i (i)}
                <Tooltip text={bar.label} class="relative flex-1 min-w-[2px]">
                  <div
                    class="absolute bottom-0 w-full rounded-t-[1px] transition-all duration-500"
                    style="height:{Math.max(
                      bar.h,
                      4,
                    )}%;background:var(--color-text-muted);opacity:{bar.current
                      ? 0.15
                      : bar.h > 0
                        ? 0.3
                        : 0.08}"
                  ></div>
                </Tooltip>
              {/each}
            </div>
          </div>

          <!-- Errors -->
          <div
            class="flex flex-1 flex-col gap-0.5 border-r border-border px-5 first:pl-0 last:border-r-0 last:pr-0"
          >
            <div
              class="flex items-baseline gap-1 font-light leading-none"
              style="font-size:20px;letter-spacing:-0.03em;color:{errorCount > 0
                ? 'var(--color-red)'
                : 'inherit'}"
            >
              {errorCount}<span
                class="text-[11px] font-medium tracking-normal text-muted-foreground/50"
                >errors</span
              >
            </div>
            <div class="text-[11px] tracking-[0.02em] text-muted-foreground/50">
              Last 5 min
            </div>
            <div class="mt-1 flex h-[18px] gap-px">
              {#each sparkErrors as bar, i (i)}
                <Tooltip text={bar.label} class="relative flex-1 min-w-[2px]">
                  <div
                    class="absolute bottom-0 w-full rounded-t-[1px] transition-all duration-500"
                    style="height:{Math.max(
                      bar.h,
                      4,
                    )}%;background:var(--color-red);opacity:{bar.current
                      ? 0.2
                      : bar.h > 0
                        ? 0.55
                        : 0.08}"
                  ></div>
                </Tooltip>
              {/each}
            </div>
          </div>

          <!-- Active services -->
          <div
            class="flex flex-1 flex-col gap-0.5 px-5 first:pl-0 last:border-r-0 last:pr-0"
          >
            <div
              class="flex items-baseline gap-1 font-light leading-none"
              style="font-size:20px;letter-spacing:-0.03em;color:var(--color-accent)"
            >
              {activeServiceCount}<span
                class="text-[11px] font-medium tracking-normal text-muted-foreground/50"
                >services</span
              >
            </div>
            <div class="text-[11px] tracking-[0.02em] text-muted-foreground/50">
              Active
            </div>
            {#if infraLegend.length > 0}
              <div class="mt-2 flex flex-wrap gap-x-3 gap-y-1">
                {#each infraLegend as item (item.kind)}
                  <div class="flex items-center gap-1 text-[10px] text-muted-foreground/60">
                    <span
                      class="inline-block h-2.5 w-2.5 rounded-[2px]"
                      style={`background:${item.color};`}
                    ></span>
                    <span class="tabular-nums text-foreground/90">{item.count}</span>
                    <span>{item.label}</span>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        </div>
      {/if}

      <!-- Hero grid: topology + feed -->
      <div
        class="mt-4 grid min-h-0 flex-1 gap-4 {canvasExpanded
          ? 'grid-cols-1'
          : 'grid-cols-[1fr_300px]'}"
      >
        <!-- Topology -->
        <div class="flex min-h-0 flex-col">
          <div class="flex-1 overflow-hidden rounded-[5px]">
            <TopologyCanvas
              onNavigate={setTab}
              onExpandedChange={(expanded) => (canvasExpanded = expanded)}
            />
          </div>
        </div>

        <!-- Activity feed: hidden when topology is expanded -->
        {#if !canvasExpanded}
          <div class="flex min-h-0 flex-col">
            <div
              class="mb-2 flex items-center justify-between text-[10px] font-semibold uppercase tracking-[0.06em] text-muted-foreground/50"
            >
              <span>Activity</span>
              {#if dashboard.data}
                <span
                  class="font-normal normal-case tracking-normal text-accent/70"
                  >live</span
                >
              {/if}
            </div>

            <div class="flex-1 overflow-y-auto">
              {#if recentTraces.length === 0}
                <div
                  class="flex h-24 items-center justify-center text-[11px] text-muted-foreground/30"
                >
                  No recent requests
                </div>
              {:else}
                {#each recentTraces.slice(0, 25) as trace (trace.id)}
                  {@const segs = chainSegs(trace)}
                  <div
                    class="grid cursor-pointer items-center gap-2 border-b border-border/50 py-2 transition-colors hover:bg-white/[0.02]"
                    style="grid-template-columns:6px 30px 1fr auto"
                  >
                    <!-- Status dot -->
                    <span
                      class="inline-block h-[6px] w-[6px] rounded-[1px] {dotClass(
                        trace.status,
                      )}"
                    ></span>

                    <!-- Status code -->
                    <span
                      class="text-right text-[11px] font-medium {statusClass(
                        trace.status,
                      )}">{trace.status}</span
                    >

                    <!-- Path + chain -->
                    <div class="min-w-0">
                      <div
                        class="truncate text-[11px] text-muted-foreground/80"
                      >
                        {traceLabel(trace)}
                      </div>
                      {#if segs.length}
                        <div
                          class="mt-[3px] flex items-center gap-[2px]"
                          style="height:3px"
                        >
                          {#each segs as seg, i (i)}
                            <div
                              class="rounded-[1px]"
                              style="width:{seg.width}px;height:3px;background:{seg.color};opacity:0.5"
                              title={seg.title}
                            ></div>
                          {/each}
                        </div>
                      {/if}
                      {#if trace.status >= 500}
                        <div
                          class="mt-[2px] text-[10px]"
                          style="color:var(--color-red)"
                        >
                          {trace.spans.find((s) => s.status === "error")
                            ?.name ?? "error"}
                        </div>
                      {/if}
                    </div>

                    <!-- Duration + ago -->
                    <div class="shrink-0 text-right">
                      <div class="text-[11px] text-muted-foreground/70">
                        {fmtMs(trace.durationMs)}
                      </div>
                      <div class="text-[10px] text-muted-foreground/40">
                        {timeAgo(trace.startedAt)}
                      </div>
                    </div>
                  </div>
                {/each}
              {/if}
            </div>
          </div>
        {/if}
      </div>
    </div>
  </main>
  {:else}
  <main class="min-w-0 flex-1 overflow-y-auto px-6 py-5">
    {#if activeTab === "gateways"}
      <APIGatewaysSection
        {sidebarCollapsed}
        onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
      />
    {:else if activeTab === "functions"}
      <FunctionsSection
        {sidebarCollapsed}
        onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
      />
    {:else if activeTab === "queues"}
      <QueuesSection
        {sidebarCollapsed}
        onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
      />
    {:else if activeTab === "sns"}
      <SNSSection
        {sidebarCollapsed}
        onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
      />
    {:else if activeTab === "secrets"}
      <SecretsSection
        {sidebarCollapsed}
        onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
      />
    {:else if activeTab === "triggers"}
      <TriggersSection
        {sidebarCollapsed}
        onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
      />
    {:else if activeTab === "eventbridge"}
      <EventBridgeSection
        {sidebarCollapsed}
        onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
      />
    {:else if activeTab === "storage"}
      <StorageSection
        {sidebarCollapsed}
        onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
      />
    {:else if activeTab === "logs"}
      <LogsSection
        initialGroup={logsInitialGroup}
        initialTimestamp={logsInitialTimestamp}
        {sidebarCollapsed}
        onToggleSidebar={() => (sidebarCollapsed = !sidebarCollapsed)}
      />
    {:else if activeTab === "xray"}
      <XraySection
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
