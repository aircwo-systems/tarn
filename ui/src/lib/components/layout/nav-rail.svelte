<script lang="ts">
  import {
    SquaresFourIcon,
    GlobeHemisphereWestIcon,
    LightningIcon,
    ChatCircleIcon,
    KeyIcon,
    HardDriveIcon,
    ScrollIcon,
    DetectiveIcon,
    SidebarSimpleIcon,
    ArrowsClockwiseIcon,
    GearIcon,
    XIcon,
    PlusIcon,
    TrashIcon,
    ShieldWarningIcon,
    BellIcon,
    BridgeIcon,
  } from "phosphor-svelte";
  import TarnLogo from "$lib/components/common/tarn-logo.svelte";
  import NavRailItem from "./nav-rail-item.svelte";
  import ThemeToggle from "./theme-toggle.svelte";
  import StatusIndicator from "$lib/components/common/status-indicator.svelte";
  import ConnectionPanel from "$lib/components/topology/connection-panel.svelte";
  import LedDot from "$lib/components/common/led-dot.svelte";
  import { Separator } from "$lib/components/ui/separator";
  import {
    getDashboard,
    getUISettings,
    getInfraSettings,
    setInfraEnabledKinds,
    setInfraFrontendTargets,
    refresh,
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

  let {
    activeTab = "overview",
    onTabChange,
  }: {
    activeTab?: string;
    onTabChange?: (tab: string) => void;
  } = $props();

  const dashboard = getDashboard();
  const uiSettings = getUISettings();
  const infraSettings = getInfraSettings();

  const INFRA_KINDS: Array<{
    id: InfraProbeKind;
    label: string;
    detail: string;
  }> = [
    { id: "docker", label: "Docker", detail: "daemon" },
    { id: "postgresql", label: "PostgreSQL", detail: ":5432" },
    { id: "redis", label: "Redis", detail: ":6379" },
    { id: "mysql", label: "MySQL", detail: ":3306" },
    { id: "mongodb", label: "MongoDB", detail: ":27017" },
  ];

  let collapsed = $state(false);
  let settingsOpen = $state(false);
  let pollingIntervalDraft = $state(uiSettings.pollingIntervalSeconds);
  let themeModeDraft = $state<ThemeMode>(uiSettings.themeMode);
  let persistenceDraft = $state(uiSettings.persistenceEnabled);
  let schemaSourceDirDraft = $state(uiSettings.schemaSourceDir);
  let logRetentionMinutesDraft = $state(uiSettings.logRetentionMinutes);
  let infraEnabledKindsDraft = $state<InfraProbeKind[]>([]);
  let infraFrontendTargetsDraft = $state<FrontendTarget[]>([]);
  let newTargetName = $state("");
  let newTargetPort = $state("");

  if (typeof window !== "undefined") {
    collapsed = localStorage.getItem("tarn-nav-collapsed") === "true";
  }

  function toggleCollapsed() {
    collapsed = !collapsed;
    localStorage.setItem("tarn-nav-collapsed", String(collapsed));
  }

  const tabs = [
    { id: "overview", label: "Overview", icon: SquaresFourIcon },
    { id: "gateways", label: "Gateways", icon: GlobeHemisphereWestIcon },
    { id: "chaos", label: "Chaos", icon: ShieldWarningIcon },
    { id: "functions", label: "Functions", icon: LightningIcon },
    { id: "queues", label: "Queues", icon: ChatCircleIcon },
    { id: "sns", label: "SNS", icon: BellIcon },
    { id: "secrets", label: "Secrets", icon: KeyIcon },
    { id: "triggers", label: "Triggers", icon: ArrowsClockwiseIcon },
    { id: "eventbridge", label: "EventBridge", icon: BridgeIcon },
    { id: "storage", label: "Storage", icon: HardDriveIcon },
    { id: "logs", label: "Logs", icon: ScrollIcon },
    { id: "xray", label: "Traces", icon: DetectiveIcon },
  ];

  const connectionStatus = $derived(
    dashboard.error
      ? ("error" as const)
      : dashboard.loading
        ? ("loading" as const)
        : dashboard.data
          ? ("ok" as const)
          : ("idle" as const),
  );

  const statusText = $derived(
    dashboard.error
      ? dashboard.error
      : dashboard.loading
        ? "Connecting..."
        : dashboard.data?.status === "running"
          ? `Connected · ${dashboard.lastRefresh}`
          : "Status unknown",
  );

  let refreshing = $state(false);
  async function handleRefresh() {
    refreshing = true;
    await refresh();
    refreshing = false;
  }

  function openSettings() {
    pollingIntervalDraft = uiSettings.pollingIntervalSeconds;
    themeModeDraft = uiSettings.themeMode;
    persistenceDraft = uiSettings.persistenceEnabled;
    schemaSourceDirDraft = uiSettings.schemaSourceDir;
    logRetentionMinutesDraft = uiSettings.logRetentionMinutes;
    infraEnabledKindsDraft = [...infraSettings.enabledKinds];
    infraFrontendTargetsDraft = infraSettings.frontendTargets.map((t) => ({
      ...t,
    }));
    newTargetName = "";
    newTargetPort = "";
    settingsOpen = true;
  }

  function closeSettings() {
    settingsOpen = false;
  }

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
    infraFrontendTargetsDraft = [
      ...infraFrontendTargetsDraft,
      { id: crypto.randomUUID(), name, host: "localhost", port },
    ];
    newTargetName = "";
    newTargetPort = "";
  }

  function removeFrontendTarget(id: string) {
    infraFrontendTargetsDraft = infraFrontendTargetsDraft.filter(
      (t) => t.id !== id,
    );
  }

  function handleNewTargetKeydown(event: KeyboardEvent) {
    if (event.key === "Enter") addFrontendTarget();
  }

  function handleWindowKeydown(event: KeyboardEvent) {
    if (event.key === "Escape" && settingsOpen) {
      settingsOpen = false;
    }
  }
</script>

<svelte:window onkeydown={handleWindowKeydown} />

<aside
  class="hidden md:flex flex-col border-r border-sidebar-border bg-sidebar h-screen sticky top-0 transition-[width] duration-200 overflow-hidden shrink-0"
  style:width={collapsed ? "56px" : "200px"}
>
  <!-- Brand -->
  <div class="flex items-center gap-2.5 px-3 py-3 shrink-0">
    <TarnLogo class="h-8 w-8 shrink-0" color="#007a5a" />
    {#if !collapsed}
      <div class="min-w-0">
        <p
          class="text-[10px] font-mono uppercase tracking-wider text-sidebar-foreground/50"
        >
          Tarn
        </p>
        <p class="text-sm font-semibold text-sidebar-foreground truncate">
          Rack Console
        </p>
      </div>
    {/if}
  </div>

  <Separator />

  <!-- Nav items -->
  <nav
    class="flex flex-col gap-0.5 px-1.5 py-2 flex-1"
    aria-label="Dashboard sections"
  >
    {#each tabs as tab}
      <NavRailItem
        icon={tab.icon}
        label={tab.label}
        active={activeTab === tab.id}
        {collapsed}
        onclick={() => onTabChange?.(tab.id)}
      />
    {/each}
  </nav>

  <!-- Bottom section -->
  <div
    class="mt-auto flex flex-col gap-2 pb-2 shrink-0"
    class:px-2={!collapsed}
    class:px-1={collapsed}
  >
    {#if !collapsed}
      <Separator />
      <StatusIndicator status={connectionStatus} text={statusText} />

      {#if dashboard.data}
        <Separator />
        <ConnectionPanel
          region={dashboard.data.config.region}
          accountId={dashboard.data.config.accountId}
          endpoint={dashboard.data.config.endpoint}
          infrastructure={dashboard.data.infrastructure ?? []}
          connections={dashboard.data.connections ?? []}
        />
      {/if}

      <button
        type="button"
        onclick={handleRefresh}
        disabled={refreshing || dashboard.loading}
        class="flex items-center justify-center gap-1.5 w-full h-7 rounded-md border border-primary/50 bg-primary/10 text-xs text-primary hover:bg-primary/20 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
      >
        <ArrowsClockwiseIcon
          size={12}
          class={refreshing ? "animate-spin" : ""}
        />
        {refreshing ? "Refreshing..." : "Refresh"}
      </button>
    {/if}

    <Separator />
    {#if collapsed}
      <div class="flex flex-col items-center gap-1">
        <button
          type="button"
          onclick={toggleCollapsed}
          class="flex items-center justify-center h-8 w-8 rounded-md text-sidebar-foreground/70 hover:text-sidebar-foreground hover:bg-sidebar-accent transition-colors"
          aria-label="Expand sidebar"
        >
          <SidebarSimpleIcon size={15} />
        </button>
        <ThemeToggle />
        <button
          type="button"
          onclick={openSettings}
          class="flex items-center justify-center h-8 w-8 rounded-md text-sidebar-foreground/70 hover:text-sidebar-foreground hover:bg-sidebar-accent transition-colors"
          aria-label="Open UI settings"
        >
          <GearIcon size={15} />
        </button>
      </div>
    {:else}
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-1">
          <ThemeToggle />
          <button
            type="button"
            onclick={openSettings}
            class="flex items-center justify-center h-8 w-8 rounded-md text-sidebar-foreground/70 hover:text-sidebar-foreground hover:bg-sidebar-accent transition-colors"
            aria-label="Open UI settings"
          >
            <GearIcon size={15} />
          </button>
        </div>
        <button
          type="button"
          onclick={toggleCollapsed}
          class="flex items-center justify-center h-8 w-8 rounded-md text-sidebar-foreground/70 hover:text-sidebar-foreground hover:bg-sidebar-accent transition-colors"
          aria-label="Collapse sidebar"
        >
          <SidebarSimpleIcon size={15} weight="fill" />
        </button>
      </div>
    {/if}
  </div>
</aside>

<!-- Mobile bottom tab bar -->
<nav
  class="fixed bottom-0 inset-x-0 z-50 flex md:hidden items-center justify-around border-t border-sidebar-border bg-sidebar/95 backdrop-blur-sm h-14 px-2"
  aria-label="Dashboard sections"
>
  {#each tabs as tab}
    {@const TabIcon = tab.icon}
    <button
      type="button"
      class="flex flex-col items-center gap-0.5 py-1 px-3 text-[10px] transition-colors {activeTab ===
      tab.id
        ? 'text-sidebar-primary'
        : 'text-sidebar-foreground/70'}"
      onclick={() => onTabChange?.(tab.id)}
      aria-current={activeTab === tab.id ? "page" : undefined}
    >
      <TabIcon size={18} weight={activeTab === tab.id ? "fill" : "regular"} />
      {tab.label}
    </button>
  {/each}
</nav>

{#if settingsOpen}
  <div
    class="fixed inset-0 z-[70] bg-black/45"
    onclick={closeSettings}
    aria-hidden="true"
  ></div>
  <div
    role="dialog"
    aria-modal="true"
    aria-label="UI Settings"
    class="fixed z-[75] left-1/2 top-1/2 w-[min(32rem,90vw)] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-card shadow-xl"
  >
    <div
      class="flex items-center justify-between border-b border-border px-4 py-3"
    >
      <h2 class="text-sm font-semibold text-foreground">UI Settings</h2>
      <button
        type="button"
        onclick={closeSettings}
        class="flex items-center justify-center h-7 w-7 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
        aria-label="Close settings"
      >
        <XIcon size={14} />
      </button>
    </div>

    <div class="space-y-4 px-4 py-4 max-h-[70vh] overflow-y-auto">
      <p class="text-xs text-muted-foreground/70">
        These preferences are saved in a browser cookie and local storage.
      </p>

      <div class="space-y-1.5">
        <label
          class="text-xs font-medium text-foreground"
          for="polling-interval">Polling Interval (seconds)</label
        >
        <input
          id="polling-interval"
          type="number"
          min="1"
          max="120"
          step="1"
          bind:value={pollingIntervalDraft}
          class="w-full rounded-md border border-border bg-muted px-2.5 py-1.5 text-sm text-foreground outline-none focus:ring-1 focus:ring-primary"
        />
      </div>

      <div class="space-y-1.5">
        <label
          class="text-xs font-medium text-foreground"
          for="log-retention">Log Retention (minutes)</label
        >
        <input
          id="log-retention"
          type="number"
          min="1"
          max="1440"
          step="1"
          bind:value={logRetentionMinutesDraft}
          class="w-full rounded-md border border-border bg-muted px-2.5 py-1.5 text-sm text-foreground outline-none focus:ring-1 focus:ring-primary"
        />
        <p class="text-[11px] leading-relaxed text-muted-foreground/70">
          Automatically remove log events older than this. Default 15 minutes. Max 1440 (24h).
        </p>
      </div>

      <div class="space-y-1.5">
        <label class="text-xs font-medium text-foreground" for="theme-mode"
          >Theme</label
        >
        <select
          id="theme-mode"
          bind:value={themeModeDraft}
          class="w-full rounded-md border border-border bg-muted px-2.5 py-1.5 text-sm text-foreground outline-none focus:ring-1 focus:ring-primary"
        >
          <option value="system">System</option>
          <option value="light">Light</option>
          <option value="dark">Dark</option>
        </select>
      </div>

      <div class="rounded-md border border-border bg-muted/70 p-3">
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0">
            <label
              class="text-xs font-medium text-foreground"
              for="persistence-enabled">Persistence</label
            >
            <p
              class="mt-1 text-[11px] leading-relaxed text-muted-foreground/70"
            >
              Persist configuration over Tarn sessions. Intended to allow
              for config to be saved and reused instead of building instance
              each time.
            </p>
          </div>
          <label
            class="relative inline-flex cursor-pointer items-center self-center"
          >
            <input
              id="persistence-enabled"
              type="checkbox"
              bind:checked={persistenceDraft}
              class="peer sr-only"
            />
            <span
              class="h-6 w-11 rounded-full border border-border bg-muted transition-colors peer-checked:border-primary/50 peer-checked:bg-primary/10"
            ></span>
            <span
              class="pointer-events-none absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white shadow-sm transition-transform peer-checked:translate-x-5 dark:bg-zinc-950"
            ></span>
          </label>
        </div>
        <div class="mt-2 text-[11px] font-mono text-muted-foreground/70">
          {persistenceDraft ? "true" : "false"}
        </div>
      </div>

      <div class="space-y-1.5">
        <label class="text-xs font-medium text-foreground" for="schema-source"
          >Schema Source</label
        >
        <input
          id="schema-source"
          type="text"
          placeholder="/path/to/lambda-repos"
          bind:value={schemaSourceDirDraft}
          onblur={() =>
            (schemaSourceDirDraft =
              sanitizeSchemaSourceDir(schemaSourceDirDraft))}
          class="w-full rounded-md border border-border bg-muted px-2.5 py-1.5 font-mono text-sm text-foreground outline-none focus:ring-1 focus:ring-primary"
        />
        <p class="text-[11px] leading-relaxed text-muted-foreground/70">
          Local directory used by Chaos Probe to discover <code>schemas.ts</code
          >
          and event samples. Saved in local project settings.
        </p>
      </div>

      <!-- Infrastructure Probes -->
      <div class="rounded-md border border-border bg-muted/70 p-3 space-y-2">
        <p
          class="text-xs font-semibold uppercase tracking-wide text-muted-foreground/70"
        >
          Infrastructure Probes
        </p>
        <p class="text-[11px] text-muted-foreground/70 leading-relaxed">
          Show local services probed by the backend. Docker is checked by
          default.
        </p>
        <div class="space-y-1.5 pt-0.5">
          {#each INFRA_KINDS as k}
            <label class="flex items-center gap-2.5 cursor-pointer group">
              <input
                type="checkbox"
                bind:group={infraEnabledKindsDraft}
                value={k.id}
                class="h-3.5 w-3.5 cursor-pointer rounded"
                style="accent-color: var(--color-accent)"
              />
              <span
                class="text-xs text-foreground group-hover:text-foreground flex-1"
                >{k.label}</span
              >
              <span class="text-[10px] font-mono text-muted-foreground/70"
                >{k.detail}</span
              >
            </label>
          {/each}
        </div>
      </div>

      <!-- Frontend Services -->
      <div class="rounded-md border border-border bg-muted/70 p-3 space-y-2">
        <p
          class="text-xs font-semibold uppercase tracking-wide text-muted-foreground/70"
        >
          Frontend Services
        </p>
        <p class="text-[11px] text-muted-foreground/70 leading-relaxed">
          Add locally running apps to probe from the browser (localhost).
        </p>
        {#if infraFrontendTargetsDraft.length > 0}
          <ul class="space-y-1 pt-0.5">
            {#each infraFrontendTargetsDraft as target (target.id)}
              <li class="flex items-center gap-2 group">
                <span class="text-xs text-foreground flex-1 truncate"
                  >{target.name}</span
                >
                <span
                  class="text-[10px] font-mono text-muted-foreground/70 shrink-0"
                  >:{target.port}</span
                >
                <button
                  type="button"
                  onclick={() => removeFrontendTarget(target.id)}
                  class="flex items-center justify-center h-5 w-5 rounded text-muted-foreground/70 hover:text-destructive hover:bg-destructive/10 transition-colors opacity-0 group-hover:opacity-100"
                  aria-label="Remove {target.name}"
                >
                  <TrashIcon size={11} />
                </button>
              </li>
            {/each}
          </ul>
        {/if}
        <div class="flex items-center gap-1.5 pt-0.5">
          <input
            type="text"
            bind:value={newTargetName}
            onkeydown={handleNewTargetKeydown}
            placeholder="Name"
            class="flex-1 min-w-0 rounded border border-border bg-muted px-2 py-1 text-xs text-foreground placeholder:text-muted-foreground/70 outline-none focus:ring-1 focus:ring-primary"
          />
          <input
            type="number"
            bind:value={newTargetPort}
            onkeydown={handleNewTargetKeydown}
            placeholder="Port"
            min="1"
            max="65535"
            class="w-16 rounded border border-border bg-muted px-2 py-1 text-xs text-foreground placeholder:text-muted-foreground/70 outline-none focus:ring-1 focus:ring-primary"
          />
          <button
            type="button"
            onclick={addFrontendTarget}
            class="flex items-center justify-center h-6 w-6 shrink-0 rounded border border-primary/50 bg-primary/10 text-primary hover:bg-primary/20 transition-colors"
            aria-label="Add frontend service"
          >
            <PlusIcon size={12} />
          </button>
        </div>
      </div>

      <div class="rounded-md border border-border bg-muted/70 p-3">
        <p
          class="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground/70"
        >
          Instance Info
        </p>
        <div class="space-y-1.5 text-xs">
          <div class="grid grid-cols-[6.5rem_1fr] gap-2">
            <span class="text-muted-foreground/70">Region</span>
            <span class="font-mono text-foreground break-all"
              >{dashboard.data?.config.region ?? "--"}</span
            >
          </div>
          <div class="grid grid-cols-[6.5rem_1fr] gap-2">
            <span class="text-muted-foreground/70">Account</span>
            <span class="font-mono text-foreground break-all"
              >{dashboard.data?.config.accountId ?? "--"}</span
            >
          </div>
          <div class="grid grid-cols-[6.5rem_1fr] gap-2">
            <span class="text-muted-foreground/70">API URL</span>
            <span class="font-mono text-foreground break-all"
              >{dashboard.data?.config.endpoint ?? "--"}</span
            >
          </div>
        </div>
        <p class="mt-2 text-[11px] text-muted-foreground/70">
          These values are currently read-only.
        </p>
      </div>
    </div>

    <div
      class="flex items-center justify-end gap-2 border-t border-border px-4 py-3"
    >
      <button
        type="button"
        onclick={closeSettings}
        class="rounded-md border border-border px-3 py-1.5 text-xs text-muted-foreground hover:bg-muted"
      >
        Cancel
      </button>
      <button
        type="button"
        onclick={applySettings}
        class="rounded-md border border-primary/50 bg-primary/10 px-3 py-1.5 text-xs text-primary hover:bg-primary/20"
      >
        Save
      </button>
    </div>
  </div>
{/if}
