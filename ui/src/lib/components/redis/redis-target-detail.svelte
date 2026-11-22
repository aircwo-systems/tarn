<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import {
    ArrowClockwiseIcon,
    ArrowSquareOutIcon,
    CheckIcon,
    CopyIcon,
    DatabaseIcon,
    FlowArrowIcon,
    KeyIcon,
    LockKeyIcon,
    RowsIcon,
    TerminalIcon,
    WarningCircleIcon,
  } from "phosphor-svelte";
  import RcButton from "#lib/components/rack/rc-button.svelte";
  import RcKv from "#lib/components/rack/rc-kv.svelte";
  import RcPanel from "#lib/components/rack/rc-panel.svelte";
  import RcStat from "#lib/components/rack/rc-stat.svelte";
  import RcTonePill from "#lib/components/rack/rc-tone-pill.svelte";
  import RedisKeysBrowser from "./redis-keys-browser.svelte";
  import RedisCliPanel from "./redis-cli-panel.svelte";
  import RedisIntegrations from "./redis-integrations.svelte";
  import { fetchRedisTargetDetail } from "#lib/api";
  import {
    REDIS_VIEWER_CHANNEL,
    REDIS_VIEWER_TAB_ID,
    formatUptime,
    loadRedisSessionAuth,
    redisProbeId,
    redisStatusTone,
    saveRedisSessionAuth,
  } from "#lib/redis";
  import { formatDate, timeAgo } from "#lib/utils";
  import type {
    InfraProbe,
    RedisAuthOptions,
    RedisTargetDetailResponse,
  } from "#lib/types";

  export type RedisSubTab = "overview" | "keys" | "cli" | "integrations";

  let {
    probe,
    selectedDb = 0,
    activeSubTab = "overview",
    popout = false,
    onSelectDb = (_db: number) => {},
    onSelectSubTab = (_tab: RedisSubTab) => {},
    onPopout = () => {},
    onNavigate = (_target: string) => {},
  }: {
    probe: InfraProbe;
    selectedDb?: number;
    activeSubTab?: RedisSubTab;
    popout?: boolean;
    onSelectDb?: (db: number) => void;
    onSelectSubTab?: (tab: RedisSubTab) => void;
    onPopout?: () => void;
    onNavigate?: (target: string) => void;
  } = $props();

  const targetId = $derived(redisProbeId(probe));
  const numberFormatter = new Intl.NumberFormat("en-GB");

  let detail = $state<RedisTargetDetailResponse | null>(null);
  let detailLoading = $state(false);
  let detailError = $state("");
  let mutationTick = $state(0);

  // Per-session optional auth/TLS override (never persisted to disk/localStorage)
  let showAuth = $state(false);
  let username = $state("");
  let password = $state("");
  let useTls = $state(false);

  const auth = $derived<RedisAuthOptions>({
    db: selectedDb,
    username: username.trim() || undefined,
    password: password || undefined,
    tls: useTls || probe.kind.toLowerCase() === "rediss",
  });

  let copied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  let controller: AbortController | null = null;
  let channel: BroadcastChannel | null = null;

  async function loadDetail(dbOverride?: number) {
    controller?.abort();
    const ac = new AbortController();
    controller = ac;
    const id = targetId;
    const reqAuth: RedisAuthOptions = { ...auth, db: dbOverride ?? selectedDb };
    detailLoading = true;
    detailError = "";
    try {
      const next = await fetchRedisTargetDetail(id, reqAuth, ac.signal);
      if (ac.signal.aborted || targetId !== id) return;
      detail = next;
    } catch (e) {
      if (ac.signal.aborted || targetId !== id) return;
      detailError = e instanceof Error ? e.message : "Failed to load Redis target";
    } finally {
      if (controller === ac) {
        detailLoading = false;
        controller = null;
      }
    }
  }

  onMount(() => {
    const saved = loadRedisSessionAuth(targetId);
    username = saved.username ?? "";
    password = saved.password ?? "";
    useTls = saved.tls ?? probe.kind.toLowerCase() === "rediss";
    void loadDetail();
    try {
      channel = new BroadcastChannel(REDIS_VIEWER_CHANNEL);
      channel.onmessage = (ev) => {
        if (document.hidden || ev.data?.sender === REDIS_VIEWER_TAB_ID) return;
        if (ev.data?.targetId === targetId) {
          mutationTick += 1;
          void loadDetail();
        }
      };
    } catch {
      channel = null;
    }
  });

  onDestroy(() => {
    controller?.abort();
    if (copyTimer) clearTimeout(copyTimer);
    channel?.close();
  });

  function applyAuth() {
    saveRedisSessionAuth(targetId, { username: username.trim(), password, tls: useTls });
    showAuth = false;
    mutationTick += 1;
    void loadDetail();
  }

  function handleDbChange(nextDb: number) {
    if (nextDb === selectedDb) return;
    onSelectDb(nextDb);
    void loadDetail(nextDb);
  }

  function handleMutated() {
    void loadDetail();
  }

  async function copyURL() {
    const scheme = auth.tls ? "rediss" : "redis";
    const url = `${scheme}://${probe.host}:${probe.port}/${selectedDb}`;
    try {
      await navigator.clipboard.writeText(url);
      copied = true;
      if (copyTimer) clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (copied = false), 1500);
    } catch {}
  }

  const resolvedStatus = $derived(detail?.target.status ?? probe.status);
  const isConnected = $derived(resolvedStatus === "connected" && !detail?.infoError);
  const info = $derived(detail?.info ?? null);
  const hitRate = $derived.by(() => {
    if (!info) return "--";
    const total = info.keyspaceHits + info.keyspaceMisses;
    if (total === 0) return "--";
    return `${((info.keyspaceHits / total) * 100).toFixed(1)}%`;
  });

  const integrationsCount = $derived(
    detail
      ? (detail.integrations.lambdas?.length ?? 0) +
          (detail.integrations.ecs?.length ?? 0) +
          (detail.integrations.secrets?.length ?? 0) +
          (detail.integrations.traces?.length ?? 0)
      : 0,
  );

  const serverItems = $derived(
    info
      ? [
          { label: "Engine", value: `${info.engine === "valkey" ? "Valkey" : "Redis"} ${info.version}`, mono: true },
          { label: "Mode / Role", value: [info.mode, info.role].filter(Boolean).join(" · ") || "--", mono: true },
          { label: "Uptime", value: formatUptime(info.uptimeSeconds), mono: true },
          { label: "Peak memory", value: info.usedMemoryPeakHuman || "--", mono: true },
          { label: "Max memory", value: info.maxMemoryBytes > 0 ? info.maxMemoryHuman : "Unlimited", mono: true },
          { label: "Ops / sec", value: numberFormatter.format(info.opsPerSec), mono: true },
          { label: "Commands", value: numberFormatter.format(info.totalCommandsHandled), mono: true },
        ]
      : [
          { label: "Status", value: resolvedStatus },
          { label: "Version", value: probe.version || "--", mono: true },
          { label: "Last probed", value: formatDate(probe.probedAt) },
        ],
  );

  const connectionItems = $derived([
    { label: "URL", value: `${auth.tls ? "rediss" : "redis"}://${probe.host}:${probe.port}/${selectedDb}`, mono: true },
    { label: "Host", value: probe.host, mono: true },
    { label: "Port", value: String(probe.port), mono: true },
    { label: "Database", value: `db${selectedDb}`, mono: true },
    { label: "Scope", value: "External · shared across all Tarn accounts", dim: true },
    { label: "Last probed", value: `${timeAgo(probe.probedAt)} (${formatDate(probe.probedAt)})` },
  ]);

  const dbKeyCounts = $derived.by(() => {
    const map = new Map<number, number>();
    for (const ks of info?.databases ?? []) map.set(ks.db, ks.keys);
    return map;
  });
</script>

<div class="detail">
  <header class="hero">
    <div class="identity">
      <div class="title-row">
        <h1 title={probe.name}>{probe.name}</h1>
        <RcTonePill tone={redisStatusTone(resolvedStatus)}>{resolvedStatus}</RcTonePill>
      </div>
      <p class="subline">
        <span>External Redis</span><i></i>
        <span class="mono">{probe.host}:{probe.port}</span>
        {#if detail?.target.version || probe.version}
          <i></i>
          <span class="mono">v{detail?.target.version || probe.version}</span>
        {/if}
        <i></i>
        <span class="shared-note">shared across account IDs</span>
      </p>
    </div>

    <div class="hero-actions">
      <label class="db-picker" title="Select Redis logical database">
        <span>DB</span>
        <select
          value={selectedDb}
          onchange={(e) => handleDbChange(Number((e.currentTarget as HTMLSelectElement).value))}
        >
          {#each Array(16) as _, idx (idx)}
            {@const count = dbKeyCounts.get(idx)}
            <option value={idx}>db{idx}{count !== undefined ? ` (${count})` : ""}</option>
          {/each}
        </select>
      </label>

      <RcButton small variant="ghost" onclick={() => (showAuth = !showAuth)} title="Session auth & TLS">
        <LockKeyIcon size={12} />
        {password || useTls ? "Auth *" : "Auth"}
      </RcButton>
      <RcButton small variant="ghost" disabled={detailLoading} onclick={() => loadDetail()}>
        <ArrowClockwiseIcon size={12} />
        {detailLoading ? "Syncing..." : "Refresh"}
      </RcButton>
      <RcButton small onclick={copyURL}>
        {#if copied}<CheckIcon size={11} />Copied{:else}<CopyIcon size={11} />URL{/if}
      </RcButton>
      {#if !popout}
        <RcButton small onclick={onPopout} title="Open Redis viewer in a standalone window">
          <ArrowSquareOutIcon size={12} />
          Pop out
        </RcButton>
      {/if}
    </div>
  </header>

  {#if showAuth}
    <form class="auth-bar" onsubmit={(e) => { e.preventDefault(); applyAuth(); }}>
      <label class="auth-field">
        <span>Username (optional)</span>
        <input class="mono" bind:value={username} placeholder="default" autocomplete="off" />
      </label>
      <label class="auth-field">
        <span>Password</span>
        <input class="mono" type="password" bind:value={password} placeholder="Session only" autocomplete="off" />
      </label>
      <label class="auth-check">
        <input type="checkbox" bind:checked={useTls} />
        <span>TLS (rediss)</span>
      </label>
      <div class="auth-actions">
        <RcButton small type="submit">Apply</RcButton>
        <RcButton small variant="ghost" onclick={() => (showAuth = false)}>Close</RcButton>
      </div>
    </form>
  {/if}

  <nav class="subtabs" aria-label="Redis viewer sections">
    <button type="button" class="subtab" class:active={activeSubTab === "overview"} onclick={() => onSelectSubTab("overview")}>
      <DatabaseIcon size={13} />
      <span>Overview</span>
    </button>
    <button type="button" class="subtab" class:active={activeSubTab === "keys"} onclick={() => onSelectSubTab("keys")}>
      <RowsIcon size={13} />
      <span>Keys</span>
      {#if info}
        <span class="tab-count">{numberFormatter.format(info.selectedDbKeys)}</span>
      {/if}
    </button>
    <button type="button" class="subtab" class:active={activeSubTab === "cli"} onclick={() => onSelectSubTab("cli")}>
      <TerminalIcon size={13} />
      <span>CLI</span>
    </button>
    <button type="button" class="subtab" class:active={activeSubTab === "integrations"} onclick={() => onSelectSubTab("integrations")}>
      <FlowArrowIcon size={13} />
      <span>Integrations</span>
      {#if integrationsCount > 0}
        <span class="tab-count">{integrationsCount}</span>
      {/if}
    </button>
  </nav>

  {#if detailError || detail?.infoError}
    <div class="error-banner" role="alert">
      <WarningCircleIcon size={14} />
      <span>{detailError || detail?.infoError}</span>
    </div>
  {/if}

  {#if activeSubTab === "overview"}
    <div class="stats">
      <RcStat
        label="Keys (db{selectedDb})"
        value={info ? numberFormatter.format(info.selectedDbKeys) : "--"}
        sub={info ? `${numberFormatter.format(info.totalKeys)} total across DBs` : "not connected"}
      />
      <RcStat
        label="Memory"
        value={info?.usedMemoryHuman || "--"}
        sub={info?.usedMemoryPeakHuman ? `peak ${info.usedMemoryPeakHuman}` : "used memory"}
      />
      <RcStat
        label="Clients"
        value={info ? numberFormatter.format(info.connectedClients) : "--"}
        sub={info ? `${numberFormatter.format(info.opsPerSec)} ops/sec` : "connected"}
      />
      <RcStat
        label="Hit Rate"
        value={hitRate}
        sub={probe.latencyMs > 0 ? `${Math.round(probe.latencyMs)}ms probe latency` : "cache hits"}
        tone={isConnected ? "green" : undefined}
      />
    </div>

    <div class="split">
      <RcPanel title="Server & Memory" description="Live Redis INFO metrics." index={0}>
        <RcKv items={serverItems} labelWidth="7.5rem" />
      </RcPanel>

      <RcPanel title="Connection & Keyspaces" description="Endpoint details and populated logical databases." index={1}>
        <RcKv items={connectionItems} labelWidth="7.5rem" />
        {#if info?.databases?.length}
          <div class="db-list">
            <span class="eyebrow">Populated Databases</span>
            <div class="db-chips">
              {#each info.databases as db (db.db)}
                <button
                  type="button"
                  class="db-chip"
                  class:selected={db.db === selectedDb}
                  onclick={() => {
                    handleDbChange(db.db);
                    onSelectSubTab("keys");
                  }}
                >
                  <KeyIcon size={11} />
                  <b class="mono">db{db.db}</b>
                  <span>{numberFormatter.format(db.keys)} keys</span>
                  {#if db.expires > 0}<small>{db.expires} TTL</small>{/if}
                </button>
              {/each}
            </div>
          </div>
        {/if}
      </RcPanel>
    </div>
  {:else if activeSubTab === "keys"}
    {#key `${targetId}:${selectedDb}`}
      <RedisKeysBrowser
        {targetId}
        {auth}
        connected={isConnected}
        externalMutationTick={mutationTick}
        onMutated={handleMutated}
      />
    {/key}
  {:else if activeSubTab === "cli"}
    <RedisCliPanel {targetId} {auth} onMutated={handleMutated} />
  {:else if activeSubTab === "integrations" && detail}
    <RedisIntegrations {detail} {onNavigate} />
  {:else if activeSubTab === "integrations"}
    <p class="note">Loading Tarn integrations...</p>
  {/if}
</div>

<style>
  .detail { display: flex; flex-direction: column; gap: 18px; min-width: 0; }
  .hero { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; flex-wrap: wrap; }
  .identity { min-width: 0; }
  .title-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .hero h1 {
    margin: 0; font-size: 18px; font-weight: 600; color: var(--text-primary);
    letter-spacing: -0.015em;
  }
  .subline {
    margin: 4px 0 0; display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
    font-size: 11.5px; color: var(--text-tertiary);
  }
  .subline i { width: 3px; height: 3px; border-radius: 50%; background: var(--border-default); display: inline-block; }
  .shared-note { color: var(--text-tertiary); font-style: italic; }
  .hero-actions { display: flex; align-items: center; gap: 7px; flex-wrap: wrap; }
  .db-picker {
    display: inline-flex; align-items: center; gap: 5px;
    font-size: 11px; color: var(--text-tertiary);
  }
  .db-picker select {
    height: 28px; padding: 0 8px; border-radius: 6px;
    border: 1px solid var(--border-subtle); background: var(--bg-element);
    color: var(--text-primary); font: 11.5px var(--font-mono, ui-monospace, monospace);
  }
  .auth-bar {
    display: flex; align-items: flex-end; gap: 10px; flex-wrap: wrap;
    padding: 12px 14px; border-radius: 9px;
    border: 1px solid var(--border-subtle); background: var(--bg-panel);
  }
  .auth-field { display: flex; flex-direction: column; gap: 4px; flex: 1; min-width: 150px; }
  .auth-field span { font-size: 10.5px; color: var(--text-tertiary); }
  .auth-field input {
    height: 28px; padding: 0 8px; border-radius: 6px;
    border: 1px solid var(--border-subtle); background: var(--bg-element);
    color: var(--text-primary); font-size: 12px;
  }
  .auth-check { display: inline-flex; align-items: center; gap: 6px; height: 28px; font-size: 11.5px; color: var(--text-secondary); }
  .auth-actions { display: flex; align-items: center; gap: 6px; }

  .subtabs {
    display: flex; align-items: center; gap: 4px; flex-wrap: wrap;
    border-bottom: 1px solid var(--border-subtle); padding-bottom: 8px;
  }
  .subtab {
    display: inline-flex; align-items: center; gap: 6px; height: 30px; padding: 0 11px;
    border-radius: 7px; border: 1px solid transparent; background: transparent;
    color: var(--text-secondary); font-size: 12px; font-weight: 500; cursor: pointer;
    transition: background 120ms ease, color 120ms ease, border-color 120ms ease;
  }
  .subtab:hover { background: var(--bg-element); color: var(--text-primary); }
  .subtab.active {
    background: var(--bg-element); border-color: var(--border-subtle); color: var(--text-primary);
  }
  .tab-count {
    padding: 1px 6px; border-radius: 999px; font: 10.5px var(--font-mono, ui-monospace, monospace);
    background: var(--bg-panel); border: 1px solid var(--border-subtle); color: var(--text-tertiary);
  }
  .error-banner {
    display: flex; align-items: center; gap: 8px; padding: 10px 12px; border-radius: 8px;
    border: 1px solid color-mix(in srgb, var(--accent-red) 40%, transparent);
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
    color: var(--accent-red); font-size: 12px;
  }
  .stats { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; }
  @media (max-width: 900px) { .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
  .split { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; align-items: start; }
  @media (max-width: 960px) { .split { grid-template-columns: minmax(0, 1fr); } }
  .db-list { margin-top: 14px; padding-top: 12px; border-top: 1px solid var(--border-subtle); }
  .eyebrow { display: block; font-size: 10px; letter-spacing: 0.08em; text-transform: uppercase; color: var(--text-tertiary); margin-bottom: 8px; }
  .db-chips { display: flex; flex-wrap: wrap; gap: 6px; }
  .db-chip {
    display: inline-flex; align-items: center; gap: 6px; padding: 5px 9px; border-radius: 7px;
    border: 1px solid var(--border-subtle); background: var(--bg-element); color: var(--text-secondary);
    font-size: 11.5px; cursor: pointer;
  }
  .db-chip:hover { border-color: var(--border-default); color: var(--text-primary); }
  .db-chip.selected { border-color: var(--topology-external-redis, var(--accent-amber)); color: var(--text-primary); }
  .db-chip small { font-size: 10px; color: var(--text-tertiary); }
  .note { font-size: 12px; color: var(--text-tertiary); }
  .mono { font-family: var(--font-mono, ui-monospace, monospace); }
</style>
