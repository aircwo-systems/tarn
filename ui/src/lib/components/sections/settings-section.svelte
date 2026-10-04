<script lang="ts">
  import {
    PlusIcon,
    TrashIcon,
    CheckIcon,
    MonitorIcon,
    SunIcon,
    MoonIcon,
    ArchiveIcon,
    ArrowCounterClockwiseIcon,
    CaretRightIcon,
    UsersIcon,
    PaintBrushIcon,
    TimerIcon,
    HardDrivesIcon,
    InfoIcon,
    SpinnerGapIcon,
    ArrowSquareOutIcon,
  } from "phosphor-svelte";
  import { onMount } from "svelte";
  import { fly, fade } from "svelte/transition";

  import type { UserService } from "$lib/types";
  import { fetchAccounts, changeAccount, type ServerAccount } from "$lib/api";
  import SectionHeader from "$lib/components/sections/section-header.svelte";
  import {
    getUISettings,
    getInfraSettings,
    getAccountSettings,
    setInfraEnabledKinds,
    setUserServices,
    setLogRetentionMinutes,
    setPollingIntervalSeconds,
    setSchemaSourceDir,
    setThemeMode,
    setCollapsedSidebarMode,
    switchAccount,
    addKnownAccount,
    removeKnownAccount,
    sanitizeSchemaSourceDir,
    type ThemeMode,
    type CollapsedSidebarMode,
    type InfraProbeKind,
  } from "$lib/state.svelte";

  let {
    instanceInfo = null,
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    instanceInfo?: { region?: string; accountId?: string; endpoint?: string } | null;
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const uiSettings = getUISettings();
  const infraSettings = getInfraSettings();
  const accountSettings = getAccountSettings();

  type SettingsTab = "accounts" | "appearance" | "engine" | "infra" | "instance";

  const SETTINGS_TABS: Array<{ id: SettingsTab; label: string; icon: any; kicker: string }> = [
    { id: "accounts",   label: "Accounts",          icon: UsersIcon,      kicker: "01" },
    { id: "appearance", label: "Appearance",        icon: PaintBrushIcon, kicker: "02" },
    { id: "engine",     label: "Engine & Polling",  icon: TimerIcon,      kicker: "03" },
    { id: "infra",      label: "Infrastructure",    icon: HardDrivesIcon, kicker: "04" },
    { id: "instance",   label: "System Instance",   icon: InfoIcon,       kicker: "05" },
  ];

  const INFRA_KINDS: Array<{ id: InfraProbeKind; label: string; detail: string; desc: string }> = [
    { id: "docker",     label: "Docker Daemon", detail: "socket / var/run", desc: "Local container engine for Lambda" },
    { id: "postgresql", label: "PostgreSQL",    detail: ":5432",            desc: "Relational database probe" },
    { id: "redis",      label: "Redis",         detail: ":6379",            desc: "In-memory cache & key-value probe" },
    { id: "mysql",      label: "MySQL",         detail: ":3306",            desc: "Relational database probe" },
    { id: "mongodb",    label: "MongoDB",       detail: ":27017",           desc: "Document database probe" },
  ];

  const THEMES: Array<{ id: ThemeMode; label: string; subtitle: string; icon: any }> = [
    { id: "dark",   label: "Pure Pitch Black", subtitle: "Zero slate authentic rack console", icon: MoonIcon },
    { id: "light",  label: "Clean Light",      subtitle: "High-contrast daylight theme",      icon: SunIcon },
    { id: "system", label: "System Sync",      subtitle: "Follows OS dark/light appearance", icon: MonitorIcon },
  ];

  const SIDEBAR_MODES: Array<{ id: CollapsedSidebarMode; label: string; desc: string }> = [
    { id: "icons",  label: "Icon Rail",       desc: "Keep navigation service icons accessible when collapsed" },
    { id: "hidden", label: "Completely Hide", desc: "Hide sidebar fully to maximize screen real estate" },
  ];

  const POLL_PRESETS = [
    { v: 2,  label: "2s",  sub: "Rapid live updates" },
    { v: 5,  label: "5s",  sub: "Balanced (Default)" },
    { v: 10, label: "10s", sub: "Lower CPU overhead" },
    { v: 30, label: "30s", sub: "Relaxed polling" },
  ];

  const RETENTION_PRESETS = [
    { v: 30,   label: "30 min", sub: "Transient session" },
    { v: 60,   label: "1 hour", sub: "Short debugging" },
    { v: 360,  label: "6 hours", sub: "Workday trace history" },
    { v: 1440, label: "24 hours", sub: "Maximum buffer" },
  ];

  let activeTab = $state<SettingsTab>("accounts");
  let savedFlash = $state(false);
  let savedTimer: ReturnType<typeof setTimeout> | undefined;

  function triggerSavedBadge() {
    savedFlash = true;
    clearTimeout(savedTimer);
    savedTimer = setTimeout(() => {
      savedFlash = false;
    }, 1800);
  }

  function handleSetTheme(mode: ThemeMode) {
    setThemeMode(mode);
    triggerSavedBadge();
  }

  function handleSetSidebarMode(mode: CollapsedSidebarMode) {
    setCollapsedSidebarMode(mode);
    triggerSavedBadge();
  }

  function handleSetPolling(seconds: number) {
    setPollingIntervalSeconds(seconds);
    triggerSavedBadge();
  }

  function handleSetRetention(minutes: number) {
    setLogRetentionMinutes(minutes);
    triggerSavedBadge();
  }

  function handleSetSchemaDir(dir: string) {
    setSchemaSourceDir(dir);
    triggerSavedBadge();
  }

  function toggleInfraKind(id: InfraProbeKind) {
    const current = infraSettings.enabledKinds;
    const next = current.includes(id) ? current.filter((k) => k !== id) : [...current, id];
    setInfraEnabledKinds(next);
    triggerSavedBadge();
  }

  // ── Accounts Logic ────────────────────────────────────────────────
  const DEFAULT_ID = "000000000000";
  const STALE_DAYS = 7;
  const DAY_MS = 86_400_000;

  let serverAccounts = $state<ServerAccount[]>([]);
  let accountsLoading = $state(false);
  let accountsError = $state("");
  let accountBusy = $state("");
  let confirmDelete = $state("");
  let showArchived = $state(false);

  let newAccountId = $state("");
  let newAccountLabel = $state("");
  let newAccountError = $state("");

  async function loadServerAccounts() {
    accountsLoading = true;
    try {
      serverAccounts = await fetchAccounts();
      accountsError = "";
    } catch (err) {
      accountsError = err instanceof Error ? err.message : "Failed to load accounts";
    } finally {
      accountsLoading = false;
    }
  }

  function daysSince(ts?: string): number | null {
    if (!ts) return null;
    const t = Date.parse(ts);
    return Number.isNaN(t) ? null : Math.floor((Date.now() - t) / DAY_MS);
  }

  function staleReason(a?: ServerAccount): string {
    if (!a || a.default || a.archived) return "";
    const days = daysSince(a.lastActivityAt);
    if (days !== null && days >= STALE_DAYS) return `No calls for ${days}d`;
    if (a.resourceTotal === 0 && (days === null || days >= 1)) return "No resources";
    return "";
  }

  function activityLabel(a?: ServerAccount): string {
    if (!a) return "Not active yet";
    const parts = [`${a.resourceTotal} ${a.resourceTotal === 1 ? "resource" : "resources"}`];
    const days = daysSince(a.lastActivityAt);
    if (days === null) parts.push("no API calls recorded");
    else if (days === 0) parts.push("active today");
    else parts.push(`last API call ${days}d ago`);
    return parts.join(" · ");
  }

  type AccountRow = { id: string; label: string; known: boolean; server?: ServerAccount; stale: string };

  const accountRows = $derived.by(() => {
    const byId = new Map(serverAccounts.map((a) => [a.id, a]));
    const rows: AccountRow[] = accountSettings.knownAccounts.map((k) => ({
      id: k.id,
      label: k.label,
      known: true,
      server: byId.get(k.id),
      stale: staleReason(byId.get(k.id)),
    }));
    for (const a of serverAccounts) {
      if (!rows.some((r) => r.id === a.id)) {
        rows.push({
          id: a.id,
          label: a.default ? "Default Account" : a.id,
          known: false,
          server: a,
          stale: staleReason(a),
        });
      }
    }
    return rows;
  });

  const activeAccount = $derived(
    accountRows.find((r) => r.id === accountSettings.activeAccountId) ?? {
      id: accountSettings.activeAccountId,
      label: accountSettings.activeAccountId === DEFAULT_ID ? "Default Account" : accountSettings.activeAccountId,
      known: true,
      stale: "",
    }
  );

  const activeRows = $derived(accountRows.filter((r) => !r.server?.archived));
  const otherActiveRows = $derived(activeRows.filter((r) => r.id !== accountSettings.activeAccountId));
  const archivedRows = $derived(accountRows.filter((r) => r.server?.archived));
  const staleCount = $derived(activeRows.filter((r) => !!r.stale).length);

  function handleAddAccount() {
    newAccountError = "";
    const id = newAccountId.trim();
    if (!/^\d{12}$/.test(id)) {
      newAccountError = "Account ID must be exactly 12 numeric digits.";
      return;
    }
    if (!addKnownAccount(id, newAccountLabel.trim() || id)) {
      newAccountError = "This account is already registered.";
      return;
    }
    newAccountId = "";
    newAccountLabel = "";
    triggerSavedBadge();
  }

  async function handleAccountAction(id: string, action: "archive" | "restore" | "delete") {
    accountBusy = id;
    accountsError = "";
    try {
      serverAccounts = await changeAccount(id, action);
      if (action !== "restore" && id === accountSettings.activeAccountId) {
        switchAccount(DEFAULT_ID);
      }
      if (action === "delete") {
        removeKnownAccount(id);
      }
      triggerSavedBadge();
    } catch (err) {
      accountsError = err instanceof Error ? err.message : `Failed to ${action} account`;
    } finally {
      accountBusy = "";
      confirmDelete = "";
    }
  }

  // ── Custom Additional Services Logic ──────────────────────────────
  let newServiceName = $state("");
  let newServiceUrl = $state("");
  let servicesSaving = $state(false);
  let servicesError = $state("");
  let newServiceNameInput: HTMLInputElement | null = $state(null);

  async function handleAddService() {
    servicesError = "";
    const name = newServiceName.trim();
    let url = newServiceUrl.trim();
    if (!url) {
      servicesError = "Service target URL or port is required.";
      return;
    }
    if (/^\d{1,5}$/.test(url)) {
      url = `http://localhost:${url}`;
    }

    servicesSaving = true;
    try {
      const next = [...infraSettings.userServices, { name: name || url, url }];
      await setUserServices(next);
      newServiceName = "";
      newServiceUrl = "";
      triggerSavedBadge();
    } catch (err) {
      servicesError = err instanceof Error ? err.message : "Failed to register service";
    } finally {
      servicesSaving = false;
    }
  }

  async function handleRemoveService(index: number) {
    servicesSaving = true;
    servicesError = "";
    try {
      const next = infraSettings.userServices.filter((_, i) => i !== index);
      await setUserServices(next);
      triggerSavedBadge();
    } catch (err) {
      servicesError = err instanceof Error ? err.message : "Failed to remove service";
    } finally {
      servicesSaving = false;
    }
  }

  // ── Route & Hash Synchronization ──────────────────────────────────
  function selectTab(tab: SettingsTab) {
    activeTab = tab;
    const params = new URLSearchParams();
    params.set("section", tab);
    history.replaceState(null, "", `#settings?${params.toString()}`);
  }

  onMount(() => {
    void loadServerAccounts();

    const qs = window.location.hash.split("?").slice(1).join("?");
    const params = new URLSearchParams(qs);
    const section = params.get("section");

    if (section === "accounts") activeTab = "accounts";
    else if (section === "appearance") activeTab = "appearance";
    else if (section === "refresh" || section === "workspace" || section === "engine") activeTab = "engine";
    else if (section === "infra") {
      activeTab = "infra";
      if (params.get("add") === "service") {
        setTimeout(() => newServiceNameInput?.focus(), 120);
      }
    } else if (section === "instance") activeTab = "instance";
  });
</script>

<div class="settings-view">
  <SectionHeader
    title="Settings"
    description="Configure local accounts, polling frequencies, UI appearance, and backend probes."
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      <div class="header-saved-indicator">
        {#if savedFlash}
          <span class="saved-badge" in:fly={{ y: -4, duration: 150 }} out:fade={{ duration: 150 }}>
            <CheckIcon size={12} weight="bold" />
            <span>Saved</span>
          </span>
        {:else}
          <span class="live-indicator">
            <span class="live-dot" aria-hidden="true"></span>
            <span>Live Auto-Save</span>
          </span>
        {/if}
      </div>
    {/snippet}
  </SectionHeader>

  <!-- Module Selector Bar -->
  <div class="module-bar" role="tablist" aria-label="Settings categories">
    {#each SETTINGS_TABS as tab (tab.id)}
      {@const isSelected = activeTab === tab.id}
      {@const Icon = tab.icon}
      <button
        type="button"
        role="tab"
        aria-selected={isSelected}
        class="module-tab"
        class:selected={isSelected}
        onclick={() => selectTab(tab.id)}
      >
        <span class="module-kicker">{tab.kicker}</span>
        <Icon size={14} weight={isSelected ? "bold" : "regular"} class="module-icon" />
        <span class="module-label">{tab.label}</span>
        {#if tab.id === "accounts" && staleCount > 0}
          <span class="tab-badge amber" title="{staleCount} stale accounts">{staleCount}</span>
        {/if}
      </button>
    {/each}
  </div>

  <div class="settings-content">
    <!-- ══════════════════════ TAB 1: ACCOUNTS ══════════════════════ -->
    {#if activeTab === "accounts"}
      <div class="panel-section" in:fade={{ duration: 120 }}>
        <!-- Active Account Banner -->
        <div class="rack-card active-account-card">
          <div class="card-header">
            <div class="header-left">
              <span class="kicker">ACTIVE WORKSPACE SCOPE</span>
              <h2 class="card-title">{activeAccount.label}</h2>
            </div>
            <span class="active-badge">
              <span class="pulse-dot"></span>
              ACTIVE
            </span>
          </div>

          <div class="account-meta-row">
            <div class="meta-field">
              <span class="meta-label">AWS ACCOUNT ID</span>
              <span class="meta-val mono">{activeAccount.id}</span>
            </div>
            <div class="meta-field">
              <span class="meta-label">RESOURCES ACTIVE</span>
              <span class="meta-val">{activeAccount.server?.resourceTotal ?? 0}</span>
            </div>
            <div class="meta-field">
              <span class="meta-label">ACTIVITY STATUS</span>
              <span class="meta-val">{activityLabel(activeAccount.server)}</span>
            </div>
          </div>
          <p class="card-hint">
            All AWS CLI, SDK, and console operations run in this isolated scope. Switching takes effect immediately across all dashboard views.
          </p>
        </div>

        <!-- Registered Accounts Table -->
        <div class="rack-card">
          <div class="card-header">
            <div class="header-left">
              <span class="kicker">AVAILABLE SCOPES</span>
              <h3 class="card-title">Registered Accounts ({activeRows.length})</h3>
            </div>
            {#if accountsLoading}
              <SpinnerGapIcon size={14} class="spin text-tertiary" />
            {/if}
          </div>

          {#if accountsError}
            <div class="error-banner">{accountsError}</div>
          {/if}

          <div class="accounts-table">
            {#each activeRows as acct (acct.id)}
              {@const isCurrent = acct.id === accountSettings.activeAccountId}
              {@const isDefault = acct.id === DEFAULT_ID}
              <div class="account-row" class:is-active={isCurrent}>
                <div class="account-info">
                  <div class="account-title-line">
                    <span class="account-label">{acct.label}</span>
                    <span class="account-id mono">{acct.id}</span>
                    {#if isCurrent}
                      <span class="mini-pill green">ACTIVE</span>
                    {/if}
                    {#if acct.stale && !isCurrent}
                      <span class="mini-pill amber" title={acct.stale}>{acct.stale}</span>
                    {/if}
                  </div>
                  <span class="account-sub mono">{activityLabel(acct.server)}</span>
                </div>

                <div class="account-actions">
                  {#if !isCurrent}
                    <button
                      type="button"
                      class="btn btn-sm btn-primary"
                      disabled={accountBusy === acct.id}
                      onclick={() => switchAccount(acct.id)}
                    >
                      Switch
                    </button>
                  {/if}

                  {#if !isDefault && acct.server}
                    <button
                      type="button"
                      class="btn btn-sm btn-ghost"
                      disabled={accountBusy === acct.id}
                      onclick={() => handleAccountAction(acct.id, "archive")}
                      title="Archive: suspends containers and background workers while keeping disk state."
                    >
                      <ArchiveIcon size={13} />
                      Archive
                    </button>
                  {/if}

                  {#if !isDefault}
                    {#if confirmDelete === acct.id}
                      <button
                        type="button"
                        class="btn btn-sm btn-danger"
                        disabled={accountBusy === acct.id}
                        onclick={() => handleAccountAction(acct.id, "delete")}
                        onblur={() => (confirmDelete = "")}
                      >
                        Confirm Delete
                      </button>
                    {:else}
                      <button
                        type="button"
                        class="btn btn-sm btn-ghost danger-hover"
                        onclick={() => (confirmDelete = acct.id)}
                        title="Delete account data"
                        aria-label="Delete account"
                      >
                        <TrashIcon size={13} />
                      </button>
                    {/if}
                  {/if}
                </div>
              </div>
            {/each}
          </div>

          <!-- Add Account Form -->
          <div class="add-account-box">
            <span class="kicker">REGISTER NEW ACCOUNT</span>
            <div class="add-form-grid">
              <input
                type="text"
                maxlength="12"
                inputmode="numeric"
                placeholder="12-digit AWS Account ID (e.g. 112233445566)"
                class="form-input mono"
                bind:value={newAccountId}
                onkeydown={(e) => e.key === "Enter" && handleAddAccount()}
              />
              <input
                type="text"
                placeholder="Account Alias (e.g. staging, sandbox)"
                class="form-input"
                bind:value={newAccountLabel}
                onkeydown={(e) => e.key === "Enter" && handleAddAccount()}
              />
              <button
                type="button"
                class="btn btn-primary"
                onclick={handleAddAccount}
                disabled={!newAccountId.trim()}
              >
                <PlusIcon size={14} weight="bold" />
                Add Account
              </button>
            </div>
            {#if newAccountError}
              <span class="field-error">{newAccountError}</span>
            {/if}
          </div>

          <!-- Archived Accounts Drawer -->
          {#if archivedRows.length > 0}
            <div class="archived-section">
              <button
                type="button"
                class="archived-toggle"
                onclick={() => (showArchived = !showArchived)}
              >
                <CaretRightIcon size={12} class={showArchived ? "rotate-90" : ""} />
                <span>Archived Accounts ({archivedRows.length})</span>
              </button>

              {#if showArchived}
                <div class="archived-list" transition:fly={{ y: -4, duration: 140 }}>
                  {#each archivedRows as acct (acct.id)}
                    <div class="account-row archived">
                      <div class="account-info">
                        <div class="account-title-line">
                          <span class="account-label">{acct.label}</span>
                          <span class="account-id mono">{acct.id}</span>
                        </div>
                        <span class="account-sub mono">
                          {acct.server?.resourceTotal ?? 0} resources · archived {daysSince(acct.server?.archivedAt) ?? 0}d ago
                        </span>
                      </div>
                      <div class="account-actions">
                        <button
                          type="button"
                          class="btn btn-sm btn-ghost"
                          disabled={accountBusy === acct.id}
                          onclick={() => handleAccountAction(acct.id, "restore")}
                        >
                          <ArrowCounterClockwiseIcon size={12} />
                          Restore
                        </button>
                        {#if confirmDelete === acct.id}
                          <button
                            type="button"
                            class="btn btn-sm btn-danger"
                            onclick={() => handleAccountAction(acct.id, "delete")}
                          >
                            Confirm Delete
                          </button>
                        {:else}
                          <button
                            type="button"
                            class="btn btn-sm btn-ghost danger-hover"
                            onclick={() => (confirmDelete = acct.id)}
                          >
                            <TrashIcon size={13} />
                          </button>
                        {/if}
                      </div>
                    </div>
                  {/each}
                </div>
              {/if}
            </div>
          {/if}
        </div>
      </div>
    {/if}

    <!-- ══════════════════════ TAB 2: APPEARANCE ══════════════════════ -->
    {#if activeTab === "appearance"}
      <div class="panel-section" in:fade={{ duration: 120 }}>
        <!-- Theme Selection -->
        <div class="rack-card">
          <div class="card-header">
            <div class="header-left">
              <span class="kicker">COLOR PALETTE</span>
              <h3 class="card-title">Console Theme</h3>
            </div>
          </div>
          <p class="card-hint">
            Changes apply instantly across all active views without requiring a page refresh.
          </p>

          <div class="theme-grid">
            {#each THEMES as t (t.id)}
              {@const isSelected = uiSettings.themeMode === t.id}
              {@const Icon = t.icon}
              <button
                type="button"
                class="theme-card"
                class:selected={isSelected}
                onclick={() => handleSetTheme(t.id)}
              >
                <div class="theme-icon-wrap">
                  <Icon size={20} weight={isSelected ? "fill" : "regular"} />
                </div>
                <div class="theme-text">
                  <span class="theme-name">{t.label}</span>
                  <span class="theme-sub">{t.subtitle}</span>
                </div>
                {#if isSelected}
                  <span class="theme-check">
                    <CheckIcon size={14} weight="bold" />
                  </span>
                {/if}
              </button>
            {/each}
          </div>
        </div>

        <!-- Sidebar Collapse Behavior -->
        <div class="rack-card">
          <div class="card-header">
            <div class="header-left">
              <span class="kicker">WORKSPACE LAYOUT</span>
              <h3 class="card-title">Collapsed Sidebar Mode</h3>
            </div>
          </div>
          <p class="card-hint">
            Determines whether the sidebar collapses into a slim icon navigation rail or hides completely for maximum canvas space.
          </p>

          <div class="mode-grid">
            {#each SIDEBAR_MODES as mode (mode.id)}
              {@const isSelected = uiSettings.collapsedSidebarMode === mode.id}
              <button
                type="button"
                class="mode-card"
                class:selected={isSelected}
                onclick={() => handleSetSidebarMode(mode.id)}
              >
                <div class="mode-header">
                  <span class="mode-name">{mode.label}</span>
                  {#if isSelected}
                    <span class="active-dot"></span>
                  {/if}
                </div>
                <span class="mode-desc">{mode.desc}</span>
              </button>
            {/each}
          </div>
        </div>
      </div>
    {/if}

    <!-- ══════════════════════ TAB 3: ENGINE & POLLING ══════════════════════ -->
    {#if activeTab === "engine"}
      <div class="panel-section" in:fade={{ duration: 120 }}>
        <!-- Polling Frequency -->
        <div class="rack-card">
          <div class="card-header">
            <div class="header-left">
              <span class="kicker">RUNTIME TELEMETRY</span>
              <h3 class="card-title">State Polling Frequency</h3>
            </div>
            <span class="mono-badge">{uiSettings.pollingIntervalSeconds}s INTERVAL</span>
          </div>
          <p class="card-hint">
            How often the dashboard queries the Tarn backend for updated queue depths, Lambda execution stats, and gateway topologies.
          </p>

          <div class="presets-grid">
            {#each POLL_PRESETS as p}
              {@const isCurrent = uiSettings.pollingIntervalSeconds === p.v}
              <button
                type="button"
                class="preset-card"
                class:selected={isCurrent}
                onclick={() => handleSetPolling(p.v)}
              >
                <span class="preset-label mono">{p.label}</span>
                <span class="preset-sub">{p.sub}</span>
              </button>
            {/each}
          </div>
        </div>

        <!-- Log & Trace Retention -->
        <div class="rack-card">
          <div class="card-header">
            <div class="header-left">
              <span class="kicker">STORAGE & MEMORY BUFFER</span>
              <h3 class="card-title">Log & Trace Retention</h3>
            </div>
            <span class="mono-badge">{uiSettings.logRetentionMinutes} MIN RETENTION</span>
          </div>
          <p class="card-hint">
            Time window before in-memory CloudWatch log events and X-Ray execution traces are pruned.
          </p>

          <div class="presets-grid">
            {#each RETENTION_PRESETS as r}
              {@const isCurrent = uiSettings.logRetentionMinutes === r.v}
              <button
                type="button"
                class="preset-card"
                class:selected={isCurrent}
                onclick={() => handleSetRetention(r.v)}
              >
                <span class="preset-label mono">{r.label}</span>
                <span class="preset-sub">{r.sub}</span>
              </button>
            {/each}
          </div>
        </div>

        <!-- Schema Source Directory -->
        <div class="rack-card">
          <div class="card-header">
            <div class="header-left">
              <span class="kicker">DEVELOPER WORKSPACE</span>
              <h3 class="card-title">Schema Source Directory</h3>
            </div>
          </div>
          <p class="card-hint">
            Local repository path inspected for TypeScript payload schemas (<code>schemas.ts</code>) and event fixtures.
          </p>
          <div class="single-input-row">
            <input
              type="text"
              class="form-input mono"
              placeholder="/Users/username/projects/my-serverless-app"
              value={uiSettings.schemaSourceDir}
              onblur={(e) => handleSetSchemaDir(e.currentTarget.value)}
              onkeydown={(e) => e.key === "Enter" && handleSetSchemaDir(e.currentTarget.value)}
            />
          </div>
        </div>
      </div>
    {/if}

    <!-- ══════════════════════ TAB 4: INFRASTRUCTURE & PROBES ══════════════════════ -->
    {#if activeTab === "infra"}
      <div class="panel-section" in:fade={{ duration: 120 }}>
        <!-- Built-in Probes -->
        <div class="rack-card">
          <div class="card-header">
            <div class="header-left">
              <span class="kicker">AUTO-DISCOVERY PROBERS</span>
              <h3 class="card-title">Built-in Services</h3>
            </div>
          </div>
          <p class="card-hint">
            Tarn automatically probes these local runtime ports and maps active connections on the Architecture Topology view.
          </p>

          <div class="infra-grid">
            {#each INFRA_KINDS as kind (kind.id)}
              {@const isEnabled = infraSettings.enabledKinds.includes(kind.id)}
              <button
                type="button"
                class="infra-toggle-card"
                class:enabled={isEnabled}
                onclick={() => toggleInfraKind(kind.id)}
              >
                <div class="infra-top-line">
                  <span class="infra-name">{kind.label}</span>
                  <span class="toggle-pill" class:on={isEnabled}>
                    {isEnabled ? "PROBING" : "OFF"}
                  </span>
                </div>
                <span class="infra-desc">{kind.desc}</span>
                <span class="infra-port mono">{kind.detail}</span>
              </button>
            {/each}
          </div>
        </div>

        <!-- Custom User Services -->
        <div class="rack-card">
          <div class="card-header">
            <div class="header-left">
              <span class="kicker">EXTERNAL DEPENDENCIES</span>
              <h3 class="card-title">Custom Endpoints &amp; Microservices</h3>
            </div>
          </div>
          <p class="card-hint">
            Register external APIs, microservices, or local web servers called by your Lambda functions. Probed live by the backend.
          </p>

          {#if infraSettings.userServices.length > 0}
            <div class="custom-services-list">
              {#each infraSettings.userServices as svc, i (svc.url + i)}
                <div class="service-row">
                  <div class="service-info">
                    <span class="service-name">{svc.name || "Unnamed Service"}</span>
                    <span class="service-url mono">{svc.url}</span>
                  </div>
                  <button
                    type="button"
                    class="btn btn-sm btn-ghost danger-hover"
                    disabled={servicesSaving}
                    onclick={() => handleRemoveService(i)}
                    title="Remove endpoint"
                  >
                    <TrashIcon size={13} />
                  </button>
                </div>
              {/each}
            </div>
          {:else}
            <div class="empty-services">
              No additional external services registered yet.
            </div>
          {/if}

          <!-- Add Service Form -->
          <div class="add-service-box">
            <span class="kicker">REGISTER ENDPOINT</span>
            <div class="add-form-grid">
              <input
                bind:this={newServiceNameInput}
                type="text"
                placeholder="Service Name (e.g. Auth Gateway)"
                class="form-input"
                bind:value={newServiceName}
                onkeydown={(e) => e.key === "Enter" && handleAddService()}
              />
              <input
                type="text"
                placeholder="Port or URL (e.g. 8080, localhost:3000, https://api.lan)"
                class="form-input mono"
                bind:value={newServiceUrl}
                onkeydown={(e) => e.key === "Enter" && handleAddService()}
              />
              <button
                type="button"
                class="btn btn-primary"
                disabled={servicesSaving || !newServiceUrl.trim()}
                onclick={handleAddService}
              >
                {#if servicesSaving}
                  <SpinnerGapIcon size={14} class="spin" />
                  Saving…
                {:else}
                  <PlusIcon size={14} weight="bold" />
                  Add Service
                {/if}
              </button>
            </div>
            {#if servicesError}
              <span class="field-error">{servicesError}</span>
            {/if}
          </div>
        </div>
      </div>
    {/if}

    <!-- ══════════════════════ TAB 5: SYSTEM INSTANCE ══════════════════════ -->
    {#if activeTab === "instance"}
      <div class="panel-section" in:fade={{ duration: 120 }}>
        <div class="rack-card">
          <div class="card-header">
            <div class="header-left">
              <span class="kicker">SYSTEM TELEMETRY</span>
              <h3 class="card-title">Tarn Daemon Status</h3>
            </div>
            <span class="mini-pill green">ONLINE</span>
          </div>

          <div class="system-kv-grid">
            <div class="kv-item">
              <span class="kv-key">PRIMARY AWS REGION</span>
              <span class="kv-val mono">{instanceInfo?.region ?? "us-east-1"}</span>
            </div>
            <div class="kv-item">
              <span class="kv-key">ROOT ACCOUNT ID</span>
              <span class="kv-val mono">{instanceInfo?.accountId ?? "000000000000"}</span>
            </div>
            <div class="kv-item">
              <span class="kv-key">LOCAL ENDPOINT URL</span>
              <span class="kv-val mono">{instanceInfo?.endpoint ?? "http://127.0.0.1:4566"}</span>
            </div>
            <div class="kv-item">
              <span class="kv-key">ENCRYPTION VAULT</span>
              <span class="kv-val mono">AES-GCM (Master Key Protected)</span>
            </div>
            <div class="kv-item">
              <span class="kv-key">DASHBOARD DISTRIBUTION</span>
              <span class="kv-val mono">Embedded Static FS (v0.1.0-dev)</span>
            </div>
            <div class="kv-item">
              <span class="kv-key">SUPPORTED SERVICES</span>
              <span class="kv-val mono">API GW, Lambda, SQS, SNS, DynamoDB, S3, Secrets Manager, EventBridge</span>
            </div>
          </div>
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  .settings-view {
    display: flex;
    flex-direction: column;
    gap: 16px;
    width: 100%;
    max-width: 960px;
    margin: 0 auto;
    padding-bottom: 64px;
  }

  .mono {
    font-family: var(--font-mono, ui-monospace, monospace);
  }

  /* ── Header Auto-Save Indicator ── */
  .header-saved-indicator {
    display: inline-flex;
    align-items: center;
    height: 26px;
  }
  .saved-badge {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 8px;
    border-radius: 4px;
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 11px;
    font-weight: 600;
    color: var(--accent-green, #34d399);
    background: color-mix(in srgb, var(--accent-green, #34d399) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-green, #34d399) 35%, transparent);
  }
  .live-indicator {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 11px;
    color: var(--text-tertiary);
  }
  .live-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent-green, #34d399);
  }

  /* ── Module Tab Bar ── */
  .module-bar {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 3px;
    border-radius: 6px;
    background: var(--bg-inset, #070707);
    border: 1px solid var(--line-soft, rgba(255, 255, 255, 0.08));
    overflow-x: auto;
    scrollbar-width: none;
  }
  .module-bar::-webkit-scrollbar {
    display: none;
  }

  .module-tab {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    height: 30px;
    padding: 0 12px;
    border-radius: 4px;
    border: 1px solid transparent;
    background: transparent;
    color: var(--text-secondary);
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 11.5px;
    cursor: pointer;
    white-space: nowrap;
    transition: all 120ms ease;
  }
  .module-tab:hover {
    color: var(--text-primary);
    background: var(--bg-element, #141414);
  }
  .module-tab.selected {
    color: var(--text-primary);
    background: var(--bg-stage, #0a0a0a);
    border-color: var(--border-default, rgba(255, 255, 255, 0.16));
    font-weight: 600;
  }
  .module-tab.selected::before {
    content: "";
    position: absolute;
    bottom: -1px;
    left: 8px;
    right: 8px;
    height: 2px;
    background: var(--accent-green, #34d399);
    border-radius: 2px;
  }
  .module-kicker {
    font-size: 9.5px;
    opacity: 0.55;
    letter-spacing: 0.04em;
  }
  :global(.module-icon) {
    flex-shrink: 0;
  }
  .tab-badge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    height: 16px;
    padding: 0 5px;
    border-radius: 3px;
    font-size: 9.5px;
    font-weight: 700;
  }
  .tab-badge.amber {
    color: var(--accent-amber, #f59e0b);
    background: color-mix(in srgb, var(--accent-amber, #f59e0b) 15%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-amber, #f59e0b) 35%, transparent);
  }

  /* ── Content Panels ── */
  .settings-content {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .panel-section {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  /* ── Rack Cards ── */
  .rack-card {
    background: var(--bg-stage, #0a0a0a);
    border: 1px solid var(--line-soft, rgba(255, 255, 255, 0.08));
    border-radius: 6px;
    padding: 16px 18px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .card-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
  }
  .header-left {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .kicker {
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 9.5px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }
  .card-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0;
  }
  .card-hint {
    margin: 0;
    font-size: 12px;
    color: var(--text-secondary);
    line-height: 1.5;
  }
  .card-hint code {
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 11px;
    padding: 1px 4px;
    background: var(--bg-element, #141414);
    border: 1px solid var(--border-subtle);
    border-radius: 3px;
  }

  /* ── Active Account Card Special ── */
  .active-account-card {
    border-color: color-mix(in srgb, var(--accent-green, #34d399) 30%, transparent);
    background: linear-gradient(180deg, color-mix(in srgb, var(--accent-green, #34d399) 5%, var(--bg-stage, #0a0a0a)) 0%, var(--bg-stage, #0a0a0a) 100%);
  }
  .active-badge {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 22px;
    padding: 0 8px;
    border-radius: 3px;
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.06em;
    color: var(--accent-green, #34d399);
    background: color-mix(in srgb, var(--accent-green, #34d399) 14%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-green, #34d399) 40%, transparent);
  }
  .pulse-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent-green, #34d399);
    box-shadow: 0 0 6px var(--accent-green, #34d399);
  }

  .account-meta-row {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 12px;
    padding: 12px 14px;
    background: var(--bg-app, #000000);
    border: 1px solid var(--border-subtle);
    border-radius: 4px;
  }
  .meta-field {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .meta-label {
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 9px;
    letter-spacing: 0.06em;
    color: var(--text-tertiary);
  }
  .meta-val {
    font-size: 12px;
    font-weight: 500;
    color: var(--text-primary);
  }

  /* ── Accounts List & Rows ── */
  .accounts-table {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .account-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 8px 12px;
    border-radius: 4px;
    border: 1px solid var(--line-soft, rgba(255, 255, 255, 0.06));
    background: var(--bg-app, #000000);
    transition: all 120ms ease;
  }
  .account-row:hover {
    border-color: var(--border-default, rgba(255, 255, 255, 0.16));
    background: var(--bg-element, #141414);
  }
  .account-row.is-active {
    border-color: color-mix(in srgb, var(--accent-green, #34d399) 35%, transparent);
    background: color-mix(in srgb, var(--accent-green, #34d399) 4%, var(--bg-app, #000000));
  }
  .account-info {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .account-title-line {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .account-label {
    font-size: 12.5px;
    font-weight: 500;
    color: var(--text-primary);
  }
  .account-id {
    font-size: 11px;
    color: var(--text-secondary);
  }
  .account-sub {
    font-size: 10.5px;
    color: var(--text-tertiary);
  }
  .account-actions {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }

  .mini-pill {
    display: inline-flex;
    align-items: center;
    height: 16px;
    padding: 0 5px;
    border-radius: 3px;
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }
  .mini-pill.green {
    color: var(--accent-green, #34d399);
    background: color-mix(in srgb, var(--accent-green, #34d399) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-green, #34d399) 30%, transparent);
  }
  .mini-pill.amber {
    color: var(--accent-amber, #f59e0b);
    background: color-mix(in srgb, var(--accent-amber, #f59e0b) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-amber, #f59e0b) 30%, transparent);
  }

  /* ── Add Form Box ── */
  .add-account-box,
  .add-service-box {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    border-radius: 4px;
    background: var(--bg-inset, #070707);
    border: 1px dashed var(--line-soft, rgba(255, 255, 255, 0.12));
    margin-top: 4px;
  }
  .add-form-grid {
    display: grid;
    grid-template-columns: 1fr 1fr auto;
    gap: 8px;
  }
  @media (max-width: 680px) {
    .add-form-grid {
      grid-template-columns: 1fr;
    }
  }

  .form-input {
    height: 28px;
    padding: 0 9px;
    border-radius: 4px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-app, #000000);
    color: var(--text-primary);
    font-size: 11.5px;
    outline: none;
    transition: all 120ms ease;
  }
  .form-input:focus {
    border-color: var(--accent-green, #34d399);
  }
  .form-input::placeholder {
    color: var(--text-tertiary);
  }
  .field-error {
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 11px;
    color: var(--accent-red, #fb7185);
  }
  .error-banner {
    padding: 8px 12px;
    border-radius: 4px;
    font-size: 11.5px;
    color: var(--accent-red, #fb7185);
    background: color-mix(in srgb, var(--accent-red, #fb7185) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-red, #fb7185) 30%, transparent);
  }

  /* ── Archived Section ── */
  .archived-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-top: 6px;
    border-top: 1px solid var(--line-soft, rgba(255, 255, 255, 0.08));
  }
  .archived-toggle {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: transparent;
    border: none;
    color: var(--text-tertiary);
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 11px;
    cursor: pointer;
    padding: 4px 0;
    transition: color 120ms ease;
  }
  .archived-toggle:hover {
    color: var(--text-primary);
  }
  .archived-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .account-row.archived {
    opacity: 0.75;
  }
  .account-row.archived:hover {
    opacity: 1;
  }

  /* ── Buttons ── */
  .btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    height: 28px;
    padding: 0 10px;
    border-radius: 4px;
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 11px;
    font-weight: 500;
    cursor: pointer;
    white-space: nowrap;
    border: 1px solid var(--border-subtle);
    background: var(--bg-element, #141414);
    color: var(--text-secondary);
    transition: all 120ms ease;
  }
  .btn:hover:not(:disabled) {
    color: var(--text-primary);
    border-color: var(--border-default);
    background: var(--bg-element-hover, #202020);
  }
  .btn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .btn-sm {
    height: 24px;
    padding: 0 8px;
    font-size: 10.5px;
  }
  .btn-primary {
    color: #000000;
    background: var(--accent-green, #34d399);
    border-color: var(--accent-green, #34d399);
    font-weight: 600;
  }
  .btn-primary:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent-green, #34d399) 88%, #ffffff);
    color: #000000;
  }
  .btn-ghost {
    background: transparent;
    border-color: transparent;
  }
  .btn-ghost:hover:not(:disabled) {
    background: var(--bg-element, #141414);
    border-color: var(--border-subtle);
  }
  .btn-ghost.danger-hover:hover:not(:disabled) {
    color: var(--accent-red, #fb7185);
    background: color-mix(in srgb, var(--accent-red, #fb7185) 12%, transparent);
    border-color: color-mix(in srgb, var(--accent-red, #fb7185) 30%, transparent);
  }
  .btn-danger {
    color: var(--accent-red, #fb7185);
    background: color-mix(in srgb, var(--accent-red, #fb7185) 12%, transparent);
    border-color: color-mix(in srgb, var(--accent-red, #fb7185) 40%, transparent);
  }
  .btn-danger:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent-red, #fb7185) 20%, transparent);
  }

  /* ── Appearance Theme Grid ── */
  .theme-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 10px;
  }
  .theme-card {
    position: relative;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 14px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-app, #000000);
    text-align: left;
    cursor: pointer;
    transition: all 120ms ease;
  }
  .theme-card:hover {
    border-color: var(--border-default);
    background: var(--bg-element, #141414);
  }
  .theme-card.selected {
    border-color: var(--accent-green, #34d399);
    background: color-mix(in srgb, var(--accent-green, #34d399) 6%, var(--bg-app, #000000));
  }
  .theme-icon-wrap {
    color: var(--text-secondary);
  }
  .theme-card.selected .theme-icon-wrap {
    color: var(--accent-green, #34d399);
  }
  .theme-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    min-width: 0;
  }
  .theme-name {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-primary);
  }
  .theme-sub {
    font-size: 10.5px;
    color: var(--text-tertiary);
  }
  .theme-check {
    color: var(--accent-green, #34d399);
    flex-shrink: 0;
  }

  /* ── Mode Grid ── */
  .mode-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 10px;
  }
  .mode-card {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 12px 14px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-app, #000000);
    text-align: left;
    cursor: pointer;
    transition: all 120ms ease;
  }
  .mode-card:hover {
    border-color: var(--border-default);
    background: var(--bg-element, #141414);
  }
  .mode-card.selected {
    border-color: var(--accent-green, #34d399);
    background: color-mix(in srgb, var(--accent-green, #34d399) 6%, var(--bg-app, #000000));
  }
  .mode-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .mode-name {
    font-size: 12.5px;
    font-weight: 600;
    color: var(--text-primary);
  }
  .mode-desc {
    font-size: 11px;
    color: var(--text-tertiary);
    line-height: 1.4;
  }
  .active-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent-green, #34d399);
  }

  /* ── Presets Grid ── */
  .presets-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
    gap: 8px;
  }
  .preset-card {
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: 10px 12px;
    border-radius: 4px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-app, #000000);
    text-align: left;
    cursor: pointer;
    transition: all 120ms ease;
  }
  .preset-card:hover {
    border-color: var(--border-default);
    background: var(--bg-element, #141414);
  }
  .preset-card.selected {
    border-color: var(--accent-green, #34d399);
    background: color-mix(in srgb, var(--accent-green, #34d399) 8%, var(--bg-app, #000000));
  }
  .preset-label {
    font-size: 13px;
    font-weight: 700;
    color: var(--text-primary);
  }
  .preset-sub {
    font-size: 10.5px;
    color: var(--text-tertiary);
  }
  .mono-badge {
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 10px;
    padding: 2px 6px;
    border-radius: 3px;
    color: var(--text-secondary);
    background: var(--bg-inset, #070707);
    border: 1px solid var(--line-soft);
  }

  .single-input-row {
    display: flex;
  }
  .single-input-row .form-input {
    width: 100%;
  }

  /* ── Infrastructure Grid ── */
  .infra-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 8px;
  }
  .infra-toggle-card {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 10px 12px;
    border-radius: 4px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-app, #000000);
    text-align: left;
    cursor: pointer;
    transition: all 120ms ease;
  }
  .infra-toggle-card:hover {
    border-color: var(--border-default);
    background: var(--bg-element, #141414);
  }
  .infra-toggle-card.enabled {
    border-color: color-mix(in srgb, var(--accent-green, #34d399) 45%, transparent);
    background: color-mix(in srgb, var(--accent-green, #34d399) 6%, var(--bg-app, #000000));
  }
  .infra-top-line {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
  }
  .infra-name {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-primary);
  }
  .toggle-pill {
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 9px;
    font-weight: 700;
    padding: 1px 5px;
    border-radius: 3px;
    color: var(--text-tertiary);
    background: var(--bg-element);
    border: 1px solid var(--border-subtle);
  }
  .toggle-pill.on {
    color: var(--accent-green, #34d399);
    background: color-mix(in srgb, var(--accent-green, #34d399) 14%, transparent);
    border-color: color-mix(in srgb, var(--accent-green, #34d399) 35%, transparent);
  }
  .infra-desc {
    font-size: 10.5px;
    color: var(--text-tertiary);
    line-height: 1.3;
  }
  .infra-port {
    font-size: 10px;
    color: var(--text-secondary);
    margin-top: 2px;
  }

  /* ── Custom Services ── */
  .custom-services-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .service-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 7px 10px;
    border-radius: 4px;
    background: var(--bg-app, #000000);
    border: 1px solid var(--border-subtle);
  }
  .service-info {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .service-name {
    font-size: 12px;
    font-weight: 500;
    color: var(--text-primary);
  }
  .service-url {
    font-size: 11px;
    color: var(--text-tertiary);
  }
  .empty-services {
    padding: 16px;
    text-align: center;
    font-size: 11.5px;
    color: var(--text-tertiary);
    background: var(--bg-app, #000000);
    border: 1px dashed var(--border-subtle);
    border-radius: 4px;
  }

  /* ── System KV ── */
  .system-kv-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
    gap: 10px;
  }
  .kv-item {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 10px 12px;
    background: var(--bg-app, #000000);
    border: 1px solid var(--border-subtle);
    border-radius: 4px;
  }
  .kv-key {
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 9px;
    letter-spacing: 0.06em;
    color: var(--text-tertiary);
  }
  .kv-val {
    font-size: 11.5px;
    color: var(--text-primary);
    word-break: break-all;
  }

  :global(.spin) {
    animation: spin 1s linear infinite;
  }
  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }

  @media (prefers-reduced-motion: reduce) {
    :global(.spin) { animation: none; }
  }
</style>
