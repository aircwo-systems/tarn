<script lang="ts">
  import { PlusIcon, TrashIcon, CheckIcon, MonitorIcon, SunIcon, MoonIcon } from "phosphor-svelte";
  import { onMount, untrack } from "svelte";
  import { fly } from "svelte/transition";

  import type { UserService } from "$lib/types";
  import SectionHeader from "$lib/components/sections/section-header.svelte";
  import {
    getUISettings,
    getInfraSettings,
    getAccountSettings,
    setInfraEnabledKinds,
    setUserServices,
    setLogRetentionMinutes,
    setPersistenceEnabled,
    setPollingIntervalSeconds,
    setSchemaSourceDir,
    setThemeMode,
    switchAccount,
    addKnownAccount,
    removeKnownAccount,
    sanitizeSchemaSourceDir,
    type ThemeMode,
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

  const INFRA_KINDS: Array<{ id: InfraProbeKind; label: string; detail: string }> = [
    { id: "docker",     label: "Docker",     detail: "daemon" },
    { id: "postgresql", label: "PostgreSQL", detail: ":5432"  },
    { id: "redis",      label: "Redis",      detail: ":6379"  },
    { id: "mysql",      label: "MySQL",      detail: ":3306"  },
    { id: "mongodb",    label: "MongoDB",    detail: ":27017" },
  ];
  const THEMES: Array<{ id: ThemeMode; label: string; icon: any }> = [
    { id: "system", label: "System", icon: MonitorIcon },
    { id: "light",  label: "Light",  icon: SunIcon },
    { id: "dark",   label: "Dark",   icon: MoonIcon },
  ];
  const POLL_PRESETS = [2, 5, 10, 30];
  const RETENTION_PRESETS = [
    { v: 30, label: "30m" }, { v: 60, label: "1h" }, { v: 360, label: "6h" }, { v: 1440, label: "24h" },
  ];
  const SECTIONS = [
    { id: "accounts",   label: "Accounts" },
    { id: "refresh",    label: "Refresh & retention" },
    { id: "appearance", label: "Appearance" },
    { id: "workspace",  label: "Workspace" },
    { id: "infra",      label: "Infrastructure" },
    { id: "instance",   label: "Instance" },
  ];

  // ── Drafts: edits stay local until saved ─────────────────────────
  let pollingInterval = $state(uiSettings.pollingIntervalSeconds);
  let themeMode       = $state<ThemeMode>(uiSettings.themeMode);
  let persistence     = $state(uiSettings.persistenceEnabled);
  let schemaSourceDir = $state(uiSettings.schemaSourceDir);
  let logRetention    = $state(uiSettings.logRetentionMinutes);
  let enabledKinds    = $state<InfraProbeKind[]>([...infraSettings.enabledKinds]);
  let services        = $state<UserService[]>(infraSettings.userServices.map((svc) => ({ ...svc })));

  function reset() {
    pollingInterval = uiSettings.pollingIntervalSeconds;
    themeMode       = uiSettings.themeMode;
    persistence     = uiSettings.persistenceEnabled;
    schemaSourceDir = uiSettings.schemaSourceDir;
    logRetention    = uiSettings.logRetentionMinutes;
    enabledKinds    = [...infraSettings.enabledKinds];
    services        = infraSettings.userServices.map((svc) => ({ ...svc }));
    servicesError   = "";
  }

  const draftKey = () => JSON.stringify([
    pollingInterval, themeMode, persistence, sanitizeSchemaSourceDir(schemaSourceDir), logRetention,
    [...enabledKinds].sort(), services,
  ]);
  const storedKey = () => JSON.stringify([
    uiSettings.pollingIntervalSeconds, uiSettings.themeMode, uiSettings.persistenceEnabled,
    uiSettings.schemaSourceDir, uiSettings.logRetentionMinutes,
    [...infraSettings.enabledKinds].sort(), infraSettings.userServices,
  ]);

  const dirty = $derived(draftKey() !== storedKey());

  // Settings hydrate from storage in the layout's onMount, after this mounts.
  // Follow stored changes while the user hasn't diverged from the last baseline.
  let baseline = untrack(storedKey);
  $effect(() => {
    const next = storedKey();
    untrack(() => {
      if (draftKey() === baseline) reset();
      baseline = next;
    });
  });

  let ready = $state(false); // skip the hydration blip on first load
  let savedFlash = $state(false);
  let flashTimer: ReturnType<typeof setTimeout> | undefined;

  async function save() {
    if (saving) return;
    saving = true;
    try {
      // Validated server-side; bail before touching local settings so the draft stays intact.
      // Saving services re-probes them on the server, so skip it when only other settings changed.
      if (JSON.stringify(services) !== JSON.stringify(infraSettings.userServices)) {
        await setUserServices(services);
      }
    } catch (err) {
      servicesError = err instanceof Error ? err.message : "Could not save services";
      return;
    } finally {
      saving = false;
    }
    setPollingIntervalSeconds(pollingInterval);
    setThemeMode(themeMode);
    setPersistenceEnabled(persistence);
    setSchemaSourceDir(schemaSourceDir);
    setLogRetentionMinutes(logRetention);
    setInfraEnabledKinds(enabledKinds);
    reset(); // pick up any clamping/normalisation done by the setters
    savedFlash = true;
    clearTimeout(flashTimer);
    flashTimer = setTimeout(() => (savedFlash = false), 1600);
  }

  function onKeydown(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s" && dirty) {
      e.preventDefault();
      save();
    }
  }

  function toggleKind(id: InfraProbeKind) {
    enabledKinds = enabledKinds.includes(id) ? enabledKinds.filter((k) => k !== id) : [...enabledKinds, id];
  }

  // ── Accounts (apply immediately, as before) ──────────────────────
  let newAccountId = $state("");
  let newAccountLabel = $state("");
  let newAccountError = $state("");

  function handleAddAccount() {
    newAccountError = "";
    const id = newAccountId.trim();
    if (!/^\d{12}$/.test(id)) { newAccountError = "Must be exactly 12 digits"; return; }
    if (!addKnownAccount(id, newAccountLabel)) { newAccountError = "Account already exists"; return; }
    newAccountId = "";
    newAccountLabel = "";
  }

  // ── Additional services ──────────────────────────────────────────
  let newTargetName = $state("");
  let newTargetUrl  = $state("");
  let servicesError = $state("");
  let saving        = $state(false);

  // Bare ports mean a local service; anything else (host, IP, URL) is sent as typed
  // and normalised by the server, which also reports what it could not parse.
  function addTarget() {
    let url = newTargetUrl.trim();
    if (!url) return;
    if (/^\d{1,5}$/.test(url)) url = `http://localhost:${url}`;
    services = [...services, { name: newTargetName.trim(), url }];
    servicesError = "";
    newTargetName = "";
    newTargetUrl = "";
  }

  // ── Section index: scroll-spy with sliding pill ──────────────────
  let activeSection = $state("accounts");
  let scrollRoot: HTMLElement | null = null;
  let root: HTMLElement;

  let jumping: ReturnType<typeof setTimeout> | undefined;

  function jump(id: string) {
    activeSection = id;
    clearTimeout(jumping);
    // Hold the clicked item while smooth scroll settles (short sections may never reach the top).
    jumping = setTimeout(() => (jumping = undefined), 700);
    root.querySelector(`#settings-${id}`)?.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  function spy() {
    if (!scrollRoot || jumping) return;
    const { top, height } = scrollRoot.getBoundingClientRect();
    if (scrollRoot.scrollTop + scrollRoot.clientHeight >= scrollRoot.scrollHeight - 4) {
      activeSection = SECTIONS[SECTIONS.length - 1].id;
      return;
    }
    const line = top + height * 0.3;
    let current = SECTIONS[0].id;
    for (const s of SECTIONS) {
      const el = root.querySelector(`#settings-${s.id}`);
      if (el && el.getBoundingClientRect().top <= line) current = s.id;
    }
    activeSection = current;
  }

  // Deep link from Services: "#settings?section=infra&add=service" jumps here and focuses the form.
  let newServiceNameInput: HTMLInputElement | null = null;

  onMount(() => {
    requestAnimationFrame(() => (ready = true));
    const params = new URLSearchParams(window.location.hash.split("?").slice(1).join("?"));
    const section = params.get("section");
    if (section && SECTIONS.some((s) => s.id === section)) {
      requestAnimationFrame(() => {
        jump(section);
        if (params.get("add") === "service") newServiceNameInput?.focus();
      });
    }
    scrollRoot = root.closest("main");
    scrollRoot?.addEventListener("scroll", spy, { passive: true });
    return () => { scrollRoot?.removeEventListener("scroll", spy); clearTimeout(flashTimer); clearTimeout(jumping); };
  });

  const activeIndex = $derived(Math.max(0, SECTIONS.findIndex((s) => s.id === activeSection)));
  const themeIndex = $derived(THEMES.findIndex((t) => t.id === themeMode));
</script>

<svelte:window onkeydown={onKeydown} />

<div class="settings" bind:this={root}>
  <SectionHeader
    title="Settings"
    description="Saved to this browser (cookie + local storage)"
    {sidebarCollapsed}
    {onToggleSidebar}
  />

  <div class="settings-grid">
    <nav class="index" aria-label="Settings sections">
      <span class="index-pill" style="transform: translateY({activeIndex * 30}px)"></span>
      {#each SECTIONS as s (s.id)}
        <button type="button" class="index-item" class:active={s.id === activeSection} onclick={() => jump(s.id)}>
          {s.label}
        </button>
      {/each}
    </nav>

    <div class="panels">
      <!-- Accounts -->
      <section id="settings-accounts" class="panel">
        <header>
          <h2>Accounts</h2>
          <p>The active account scopes every API request. Switching applies immediately.</p>
        </header>
        <ul class="rows">
          {#each accountSettings.knownAccounts as acct (acct.id)}
            {@const isActive = acct.id === accountSettings.activeAccountId}
            <li class="row" class:is-active={isActive}>
              <span class="row-main">
                <span class="row-title">{acct.label}</span>
                <span class="row-sub mono">{acct.id}</span>
              </span>
              {#if isActive}
                <span class="pill pill-accent"><CheckIcon size={10} weight="bold" />Active</span>
              {:else}
                <button type="button" class="pill pill-btn" onclick={() => switchAccount(acct.id)}>Switch</button>
              {/if}
              {#if acct.id !== "000000000000"}
                <button type="button" class="icon-btn danger" onclick={() => removeKnownAccount(acct.id)} aria-label="Remove account {acct.id}">
                  <TrashIcon size={12} />
                </button>
              {:else}
                <span class="icon-spacer"></span>
              {/if}
            </li>
          {/each}
        </ul>
        <div class="add-row accounts">
          <input class="field mono" maxlength="12" inputmode="numeric" placeholder="012345678901"
            aria-label="New AWS account ID"
            bind:value={newAccountId} onkeydown={(e) => e.key === "Enter" && handleAddAccount()} />
          <input class="field" placeholder="Label (optional)"
            aria-label="Account label (optional)"
            bind:value={newAccountLabel} onkeydown={(e) => e.key === "Enter" && handleAddAccount()} />
          <button type="button" class="add-btn" onclick={handleAddAccount} aria-label="Add account"><PlusIcon size={12} weight="bold" /></button>
        </div>
        {#if newAccountError}
          <p class="error" transition:fly={{ y: -4, duration: 140 }}>{newAccountError}</p>
        {/if}
      </section>

      <!-- Refresh & retention -->
      <section id="settings-refresh" class="panel">
        <header>
          <h2>Refresh &amp; retention</h2>
          <p>How often the dashboard polls, and how long logs and traces are kept.</p>
        </header>
        <div class="setting">
          <div class="setting-label">
            <span>Polling interval</span>
            <small>Live backend state refresh</small>
          </div>
          <div class="setting-control">
            <div class="chips">
              {#each POLL_PRESETS as p}
                <button type="button" class="chip" class:on={pollingInterval === p} onclick={() => (pollingInterval = p)}>{p}s</button>
              {/each}
            </div>
            <label class="unit-field">
              <input type="number" min="1" max="120" step="1" bind:value={pollingInterval} aria-label="Polling interval seconds" />
              <span>sec</span>
            </label>
          </div>
        </div>
        <div class="setting">
          <div class="setting-label">
            <span>Log &amp; trace retention</span>
            <small>Older events are removed. Max 24h.</small>
          </div>
          <div class="setting-control">
            <div class="chips">
              {#each RETENTION_PRESETS as p}
                <button type="button" class="chip" class:on={logRetention === p.v} onclick={() => (logRetention = p.v)}>{p.label}</button>
              {/each}
            </div>
            <label class="unit-field">
              <input type="number" min="1" max="1440" step="1" bind:value={logRetention} aria-label="Retention minutes" />
              <span>min</span>
            </label>
          </div>
        </div>
      </section>

      <!-- Appearance -->
      <section id="settings-appearance" class="panel">
        <header>
          <h2>Appearance</h2>
          <p>Follow the OS or pin a theme.</p>
        </header>
        <div class="setting">
          <div class="setting-label"><span>Theme</span></div>
          <div class="segmented" role="radiogroup" aria-label="Theme">
            <span class="segmented-pill" style="transform: translateX({themeIndex * 100}%)"></span>
            {#each THEMES as t (t.id)}
              {@const Icon = t.icon}
              <button type="button" role="radio" aria-checked={themeMode === t.id} class:on={themeMode === t.id} onclick={() => (themeMode = t.id)}>
                <Icon size={12} weight={themeMode === t.id ? "fill" : "regular"} />{t.label}
              </button>
            {/each}
          </div>
        </div>
      </section>

      <!-- Workspace -->
      <section id="settings-workspace" class="panel">
        <header>
          <h2>Workspace</h2>
          <p>Session persistence and local sources used by Chaos Probe.</p>
        </header>
        <div class="setting">
          <div class="setting-label">
            <span>Persistence</span>
            <small>Keep configuration across Tarn sessions</small>
          </div>
          <button type="button" role="switch" aria-checked={persistence} aria-label="Persistence" class="switch" class:on={persistence} onclick={() => (persistence = !persistence)}>
            <span class="knob"></span>
          </button>
        </div>
        <div class="setting stacked">
          <div class="setting-label">
            <span>Schema source</span>
            <small>Directory scanned for <code>schemas.ts</code> and event samples</small>
          </div>
          <input class="field mono" placeholder="/path/to/lambda-repos" aria-label="Schema source directory" bind:value={schemaSourceDir}
            onblur={() => (schemaSourceDir = sanitizeSchemaSourceDir(schemaSourceDir))} />
        </div>
      </section>

      <!-- Infrastructure -->
      <section id="settings-infra" class="panel">
        <header>
          <h2>Infrastructure</h2>
          <p>Local services probed by the backend and shown in topology and health.</p>
        </header>
        <div class="chips wrap">
          {#each INFRA_KINDS as kind (kind.id)}
            {@const on = enabledKinds.includes(kind.id)}
            <button type="button" class="probe" class:on aria-pressed={on} onclick={() => toggleKind(kind.id)}>
              {kind.label}<span class="mono dim">{kind.detail}</span>
            </button>
          {/each}
        </div>

        <div class="subhead">Additional services</div>
        <p class="hint">APIs and apps your functions call: a port, host:port, LAN IP or full URL (http, https, tcp). Probed by the Tarn server.</p>
        {#if services.length > 0}
          <ul class="rows">
            {#each services as target, i (i)}
              <li class="row" transition:fly={{ y: -4, duration: 160 }}>
                <span class="row-main"><span class="row-title">{target.name || "Unnamed"}</span></span>
                <span class="pill mono" title={target.url}>{target.url}</span>
                <button type="button" class="icon-btn danger" onclick={() => (services = services.filter((_, j) => j !== i))} aria-label="Remove {target.name || target.url}">
                  <TrashIcon size={12} />
                </button>
              </li>
            {/each}
          </ul>
        {/if}
        <div class="add-row services">
          <input class="field" bind:this={newServiceNameInput} placeholder="Service name" aria-label="New service name" bind:value={newTargetName} onkeydown={(e) => e.key === "Enter" && addTarget()} />
          <input class="field mono" placeholder="8080, 192.168.1.20:3000 or https://api.lan/health" aria-label="New target URL or port" bind:value={newTargetUrl} onkeydown={(e) => e.key === "Enter" && addTarget()} />
          <button type="button" class="add-btn" onclick={addTarget} aria-label="Add service"><PlusIcon size={12} weight="bold" /></button>
        </div>
        {#if servicesError}<p class="error">{servicesError}</p>{/if}
      </section>

      <!-- Instance -->
      <section id="settings-instance" class="panel">
        <header>
          <h2>Instance</h2>
          <p>Read-only details of the connected Tarn.</p>
        </header>
        <dl class="kv">
          <dt>Region</dt><dd class="mono">{instanceInfo?.region ?? "--"}</dd>
          <dt>Account</dt><dd class="mono">{instanceInfo?.accountId ?? "--"}</dd>
          <dt>API URL</dt><dd class="mono">{instanceInfo?.endpoint ?? "--"}</dd>
        </dl>
      </section>
    </div>
  </div>

  {#if ready && (dirty || savedFlash)}
    <div class="savebar" transition:fly={{ y: 16, duration: 220 }}>
      {#if dirty}
        <span class="savebar-dot"></span>
        <span class="savebar-text">Unsaved changes</span>
        <button type="button" class="pill pill-btn ghost" onclick={reset}>Discard</button>
        <button type="button" class="pill pill-btn primary" disabled={saving} onclick={save}>{saving ? "Saving…" : "Save"} <kbd>⌘S</kbd></button>
      {:else}
        <CheckIcon size={12} weight="bold" class="text-[var(--accent-green)]" />
        <span class="savebar-text">Saved</span>
      {/if}
    </div>
  {/if}
</div>

<style>
  .settings { position: relative; display: flex; flex-direction: column; min-height: 100%; }
  .mono { font-family: var(--font-mono, ui-monospace, monospace); }
  .dim { color: var(--text-tertiary); }

  .settings-grid {
    display: grid;
    grid-template-columns: 180px minmax(0, 720px);
    gap: 32px;
    padding: 20px 0 96px;
  }
  @media (max-width: 900px) {
    .settings-grid { grid-template-columns: minmax(0, 1fr); }
    .index { display: none !important; }
  }

  /* Section index */
  .index { position: sticky; top: 0; align-self: start; display: flex; flex-direction: column; }
  .index-pill {
    position: absolute; left: 0; right: 0; top: 0; height: 28px;
    border-radius: 5px; background: var(--bg-element); border: 1px solid var(--border-default);
    transition: transform 260ms var(--ease-snappy);
  }
  .index-pill::before {
    content: ""; position: absolute; left: 6px; top: 8px; bottom: 8px; width: 2.5px;
    border-radius: 2px; background: var(--text-primary); opacity: 0.9;
  }
  .index-item {
    position: relative; height: 28px; margin-bottom: 2px; padding: 0 10px; text-align: left;
    font-size: 12px; color: var(--text-tertiary); border-radius: 7px;
    transition: color 120ms ease, padding-left 220ms var(--ease-snappy);
  }
  .index-item:hover { color: var(--text-secondary); }
  .index-item.active { color: var(--text-primary); padding-left: 18px; }

  /* Panels */
  .panels { display: flex; flex-direction: column; gap: 14px; }
  .panel {
    scroll-margin-top: 12px;
    border: 1px solid var(--border-subtle); border-radius: 12px; padding: 16px 18px;
    background: var(--bg-stage);
    animation: panelIn 320ms var(--ease-snappy) both;
  }
  .panel:nth-child(2) { animation-delay: 30ms; }
  .panel:nth-child(3) { animation-delay: 60ms; }
  .panel:nth-child(4) { animation-delay: 90ms; }
  .panel:nth-child(5) { animation-delay: 120ms; }
  .panel:nth-child(6) { animation-delay: 150ms; }
  @keyframes panelIn { from { opacity: 0; transform: translateY(6px); } }
  .panel header { margin-bottom: 12px; }
  .panel h2 { font-size: 13px; font-weight: 600; color: var(--text-primary); letter-spacing: -0.01em; }
  .panel header p { margin-top: 2px; font-size: 11.5px; color: var(--text-secondary); }
  .subhead { margin: 16px 0 8px; font-size: 10.5px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--text-tertiary); }

  /* Setting rows */
  .setting {
    display: flex; align-items: center; justify-content: space-between; gap: 16px;
    padding: 10px 0; border-top: 1px solid var(--border-subtle);
  }
  .panel header + .setting { border-top: 0; padding-top: 0; }
  .setting.stacked { flex-direction: column; align-items: stretch; gap: 8px; }
  .setting-label { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
  .setting-label span { font-size: 12.5px; color: var(--text-primary); }
  .setting-label small { font-size: 11px; color: var(--text-tertiary); }
  .setting-control { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }

  /* Fields */
  .field {
    height: 30px; width: 100%; min-width: 0; padding: 0 10px; border-radius: 8px;
    border: 1px solid var(--border-subtle); background: var(--bg-app); color: var(--text-primary);
    font-size: 12px; outline: none; transition: border-color 120ms ease, background 120ms ease;
  }
  .field::placeholder { color: var(--text-tertiary); }
  .field:hover { border-color: var(--border-default); }
  .field:focus { border-color: var(--border-focus); }
  .unit-field {
    display: inline-flex; align-items: center; height: 28px; border-radius: 8px;
    border: 1px solid var(--border-subtle); background: var(--bg-app); padding: 0 10px 0 4px;
    transition: border-color 120ms ease;
  }
  .unit-field:focus-within { border-color: var(--border-focus); }
  .unit-field input {
    width: 48px; background: transparent; border: 0; outline: none; text-align: right;
    font-size: 12px; color: var(--text-primary); font-variant-numeric: tabular-nums;
  }
  .unit-field input::-webkit-inner-spin-button { display: none; }
  .unit-field span { margin-left: 6px; font-size: 11px; color: var(--text-tertiary); }

  /* Chips / pills */
  .chips { display: inline-flex; gap: 4px; }
  .chips.wrap { flex-wrap: wrap; gap: 6px; }
  .chip, .probe {
    display: inline-flex; align-items: center; gap: 6px; height: 26px; padding: 0 10px;
    border-radius: 8px; border: 1px solid var(--border-subtle); font-size: 11.5px;
    color: var(--text-secondary); background: transparent;
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .chip:hover, .probe:hover { color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element-hover); }
  .chip:active, .probe:active, .pill-btn:active, .add-btn:active { transform: scale(0.96); }
  .chip.on, .probe.on {
    color: var(--text-primary); border-color: color-mix(in srgb, var(--accent-green) 45%, transparent);
    background: color-mix(in srgb, var(--accent-green) 10%, transparent);
  }
  .probe .mono { font-size: 10.5px; }

  .pill {
    display: inline-flex; align-items: center; gap: 4px; height: 22px; padding: 0 9px; border-radius: 8px;
    font-size: 11px; color: var(--text-secondary); border: 1px solid var(--border-subtle); white-space: nowrap;
  }
  .pill-accent {
    color: var(--accent-green); border-color: color-mix(in srgb, var(--accent-green) 35%, transparent);
    background: color-mix(in srgb, var(--accent-green) 10%, transparent);
  }
  .pill-btn { cursor: pointer; transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease; }
  .pill-btn:hover { color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element-hover); }
  .pill-btn.primary {
    height: 26px; padding: 0 12px; color: var(--bg-stage); background: var(--text-primary); border-color: var(--text-primary);
  }
  .pill-btn.primary:hover { opacity: 0.88; background: var(--text-primary); color: var(--bg-stage); }
  .pill-btn.ghost { height: 26px; padding: 0 12px; }
  kbd { font: inherit; font-size: 10px; opacity: 0.55; margin-left: 2px; }


  /* Rows */
  .rows { display: flex; flex-direction: column; gap: 2px; }
  .row {
    display: flex; align-items: center; gap: 10px; padding: 7px 8px 7px 10px; border-radius: 8px;
    border: 1px solid transparent;
    transition: background 120ms ease, border-color 120ms ease, padding-left 220ms var(--ease-snappy);
  }
  .row:hover { background: var(--bg-element-hover); }
  .row { position: relative; }
  .row.is-active { padding-left: 18px; border-color: var(--border-default); background: var(--bg-element); }
  .row.is-active::before {
    content: ""; position: absolute; left: 6px; top: 10px; bottom: 10px; width: 2.5px;
    border-radius: 2px; background: var(--text-primary); opacity: 0.9;
  }
  .row-main { display: flex; flex-direction: column; flex: 1; min-width: 0; }
  .row-title { font-size: 12.5px; color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .row-sub { font-size: 10.5px; color: var(--text-tertiary); }
  .icon-btn, .icon-spacer { width: 24px; height: 24px; flex-shrink: 0; }
  .icon-btn {
    display: inline-flex; align-items: center; justify-content: center; border-radius: 8px;
    color: var(--text-tertiary); opacity: 0; transition: opacity 120ms ease, color 120ms ease, background 120ms ease;
  }
  .row:hover .icon-btn, .icon-btn:focus-visible { opacity: 1; }
  .icon-btn.danger:hover { color: var(--accent-red); background: color-mix(in srgb, var(--accent-red) 12%, transparent); }

  .add-row { display: grid; gap: 6px; margin-top: 8px; }
  .add-row.accounts { grid-template-columns: 9rem minmax(0, 1fr) 30px; }
  .add-row.services { grid-template-columns: 10rem minmax(0, 1fr) 30px; }
  .row .pill { display: block; max-width: 55%; line-height: 22px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .hint { margin: -2px 0 8px; font-size: 11px; color: var(--text-tertiary); }
  .add-btn {
    display: inline-flex; align-items: center; justify-content: center; width: 30px; height: 30px; border-radius: 8px;
    border: 1px solid var(--border-subtle); color: var(--text-secondary);
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .add-btn:hover { color: var(--accent-green); border-color: color-mix(in srgb, var(--accent-green) 45%, transparent); background: color-mix(in srgb, var(--accent-green) 10%, transparent); }
  .error { margin-top: 6px; font-size: 11px; color: var(--accent-red); }

  /* Segmented */
  .segmented {
    position: relative; display: inline-grid; grid-template-columns: repeat(3, 84px); padding: 2px;
    border-radius: 8px; border: 1px solid var(--border-subtle); background: var(--bg-app);
  }
  .segmented-pill {
    position: absolute; top: 2px; bottom: 2px; left: 2px; width: 84px; border-radius: 6px;
    background: var(--bg-stage); border: 1px solid var(--border-default);
    transition: transform 280ms var(--ease-snappy);
  }
  .segmented button {
    position: relative; display: inline-flex; align-items: center; justify-content: center; gap: 5px;
    height: 24px; font-size: 11.5px; color: var(--text-tertiary); transition: color 140ms ease;
  }
  .segmented button:hover { color: var(--text-secondary); }
  .segmented button.on { color: var(--text-primary); }

  /* Switch */
  .switch {
    position: relative; width: 34px; height: 20px; border-radius: 8px; flex-shrink: 0;
    border: 1px solid var(--border-default); background: var(--bg-app);
    transition: background 180ms ease, border-color 180ms ease;
  }
  .switch .knob {
    position: absolute; top: 2px; left: 2px; width: 14px; height: 14px; border-radius: 4px;
    background: var(--text-tertiary); transition: transform 240ms var(--ease-snappy), background 180ms ease;
  }
  .switch.on { background: color-mix(in srgb, var(--accent-green) 22%, transparent); border-color: color-mix(in srgb, var(--accent-green) 55%, transparent); }
  .switch.on .knob { transform: translateX(14px); background: var(--accent-green); }

  /* KV */
  .kv { display: grid; grid-template-columns: 7rem 1fr; row-gap: 6px; font-size: 12px; }
  .kv dt { color: var(--text-tertiary); }
  .kv dd { color: var(--text-primary); word-break: break-all; }

  /* Save bar */
  .savebar {
    position: sticky; bottom: 12px; align-self: center; z-index: 5; margin-top: -60px;
    display: inline-flex; align-items: center; gap: 8px; height: 40px; padding: 0 6px 0 14px;
    border-radius: 8px; border: 1px solid var(--border-default); background: var(--bg-element);
  }
  .savebar-text { font-size: 12px; color: var(--text-secondary); margin-right: 6px; }
  .savebar-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--accent-amber); animation: breathe 1.6s ease-in-out infinite; }
  @keyframes breathe { 50% { opacity: 0.35; } }

  button:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }

  @media (prefers-reduced-motion: reduce) {
    .panel, .savebar-dot { animation: none; }
    .index-pill, .segmented-pill, .switch .knob { transition: none; }
  }
</style>
