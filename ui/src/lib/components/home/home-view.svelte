<script lang="ts">
  import {
    ArrowRightIcon,
    CaretRightIcon,
    DetectiveIcon,
    GraphIcon,
    LightningIcon,
    MagnifyingGlassIcon,
    PaperPlaneTiltIcon,
    ScrollIcon,
    SidebarSimpleIcon,
    WarningCircleIcon,
    WarningIcon,
  } from "phosphor-svelte";
  import { tick } from "svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import LivingTarn, { type Buoy } from "./living-tarn.svelte";
  import { getBannerPrefs } from "./banner-prefs.svelte";
  import StackHex from "$lib/components/stack/stack-hex.svelte";
  import TagFilter from "$lib/components/layout/tag-filter.svelte";
  import TopologyCanvas from "$lib/components/topology/topology-canvas.svelte";
  import { stackGroupsFromOverview } from "$lib/components/stack/overview-groups";
  import { getDashboard, getDashboardFilters, getVisibleInfra } from "$lib/state.svelte";
  import { getLogPulse } from "$lib/log-pulse.svelte";
  import { matchesInfrastructureFilter, matchesResourceFilter } from "$lib/filter-utils";
  import { normalizeTopologyInfraKind } from "$lib/components/topology/topology-canvas-theme";
  import { timeAgo } from "$lib/utils";
  import type { RequestTrace } from "$lib/types";

  // Home is where Tarn opens: the tarn across the top with a buoy for
  // each function, one way to find anything at the seam, then what needs a
  // look, what is moving, and what is running underneath. Every row goes
  // somewhere. The map closes the page: scroll down and it fills the
  // stage, and Explore lets it be panned in place.

  let {
    topologyRequested = false,
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
    onNavigate,
    onOpenTrace,
    onOpenPalette,
  }: {
    topologyRequested?: boolean;
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
    onNavigate: (tab: string) => void;
    onOpenTrace: (traceId: string) => void;
    onOpenPalette: () => void;
  } = $props();

  const dashboard = getDashboard();
  const banner = getBannerPrefs();
  const filters = getDashboardFilters();
  const logPulse = $derived(getLogPulse());

  // A clock for "lit lately" and "ago"; ticks slowly, the poll does the rest.
  let now = $state(Date.now());
  $effect(() => {
    const t = window.setInterval(() => (now = Date.now()), 15_000);
    return () => window.clearInterval(t);
  });

  const data = $derived(dashboard.data);
  const tag = $derived(filters.tagFilter);

  // The same filters the sidebar counts with, so the numbers agree.
  const functions = $derived((data?.functions ?? []).filter((f) => matchesResourceFilter("function", tag, f.tags)));
  const queues = $derived((data?.queues ?? []).filter((q) => matchesResourceFilter("queue", tag, q.tags)));
  const traces = $derived(data?.recentTraces ?? []);
  const infra = $derived(
    getVisibleInfra(data?.infrastructure ?? []).filter((p) => matchesInfrastructureFilter(normalizeTopologyInfraKind(p.kind), tag)),
  );

  const LIT_MS = 10 * 60_000;

  const buoys = $derived<Buoy[]>(
    functions.map((fn) => {
      const state = fn.state.toLowerCase();
      const last = fn.lastInvokedAt ? new Date(fn.lastInvokedAt).getTime() : NaN;
      const lit = !Number.isNaN(last) && now - last < LIT_MS;
      return {
        id: fn.name,
        label: fn.name,
        detail:
          state === "failed"
            ? "failed"
            : lit
              ? `ran ${timeAgo(fn.lastInvokedAt, now)} · ${fn.runtime}`
              : `resting · ${fn.runtime}`,
        state: state === "failed" ? "trouble" : lit ? "lit" : "resting",
        ranAt: Number.isNaN(last) ? undefined : last,
      };
    }),
  );

  // ── Needs a look ─────────────────────────────────────────────
  interface Attention {
    id: string;
    tone: "red" | "amber";
    title: string;
    detail: string;
    go: () => void;
  }

  // A queue is a DLQ when another queue's redrive names it, or when its
  // name says so (redrive isn't always reported).
  const DLQ_NAME = /(^|[-_.])(dlq|dead-?letter)([-_.]|$)/i;
  const dlqNames = $derived(
    new Set([...queues.map((q) => q.dlqName), ...queues.filter((q) => DLQ_NAME.test(q.name)).map((q) => q.name)].filter(Boolean)),
  );
  const failedTraces = $derived(
    traces.filter((t) => t.status >= 500).sort((a, b) => new Date(b.startedAt).getTime() - new Date(a.startedAt).getTime()),
  );

  const attention = $derived.by<Attention[]>(() => {
    const out: Attention[] = [];
    if (dashboard.error) {
      out.push({ id: "refresh", tone: "red", title: "Tarn isn't answering", detail: `${dashboard.error} · showing last known data`, go: () => onNavigate("settings") });
    }
    for (const fn of functions.filter((f) => f.state.toLowerCase() === "failed")) {
      out.push({ id: `fn-${fn.name}`, tone: "red", title: fn.name, detail: "Function failed to start", go: () => onNavigate("functions") });
    }
    for (const q of queues.filter((q) => dlqNames.has(q.name) && q.approxVisible > 0)) {
      out.push({ id: `dlq-${q.name}`, tone: "red", title: q.name, detail: `≈ ${q.approxVisible} dead-lettered ${q.approxVisible === 1 ? "message" : "messages"}`, go: () => onNavigate("queues") });
    }
    if (failedTraces.length) {
      const latest = failedTraces[0];
      out.push({
        id: "5xx",
        tone: "red",
        title: `${failedTraces.length} ${failedTraces.length === 1 ? "request" : "requests"} answered 5xx`,
        detail: `Latest ${traceLabel(latest)} · ${timeAgo(latest.startedAt, now)}`,
        go: () => onOpenTrace(latest.id),
      });
    }
    // Often just something not started yet, so one quiet row for them all.
    const down = infra.filter((p) => p.status !== "connected");
    if (down.length) {
      out.push({
        id: "infra-down",
        tone: "amber",
        title: down.length === 1 ? down[0].name : `${down.length} services not reachable`,
        detail: down.length === 1 ? `${down[0].status} · ${down[0].host}:${down[0].port}` : down.map((p) => p.name).join(", "),
        go: () => onNavigate("services"),
      });
    }
    for (const q of queues.filter((q) => q.disruptEnabled)) {
      out.push({ id: `disrupt-${q.name}`, tone: "amber", title: q.name, detail: `Disruptor armed${q.disruptFailureRate ? ` · ${Math.round(q.disruptFailureRate * 100)}% of sends fail` : ""}`, go: () => onNavigate("queues") });
    }
    for (const q of queues.filter((q) => q.approxStale > 0 && !dlqNames.has(q.name))) {
      out.push({ id: `stale-${q.name}`, tone: "amber", title: q.name, detail: `≈ ${q.approxStale} stale ${q.approxStale === 1 ? "message" : "messages"}`, go: () => onNavigate("queues") });
    }
    if (logPulse.status === "ready" && logPulse.recentErrors > 0) {
      out.push({ id: "log-errors", tone: "amber", title: `${logPulse.recentErrors} log ${logPulse.recentErrors === 1 ? "error" : "errors"}`, detail: "In the last 5 minutes", go: () => onNavigate("logs") });
    }
    for (const [i, w] of (data?.warnings ?? []).entries()) {
      out.push({ id: `warn-${i}`, tone: "amber", title: "Collector warning", detail: w, go: () => scrollToTopology() });
    }
    return out;
  });

  // ── Recent activity ──────────────────────────────────────────
  function traceLabel(t: RequestTrace): string {
    const eb = t.spans.find((s) => s.kind === "eventbridge");
    if (eb) return `EVENT ${eb.name}`;
    return `${t.method ?? ""} ${t.path ?? ""}`.trim() || `trace ${t.id.slice(0, 8)}`;
  }

  function fmtMs(ms: number): string {
    return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${Math.round(ms)}ms`;
  }

  const SPAN_COLOR: Record<string, string> = {
    gateway: "var(--stack-gateway)",
    lambda: "var(--stack-function)",
    queue: "var(--stack-queue)",
    dlq: "var(--accent-red)",
    topic: "var(--stack-topic)",
    eventbridge: "var(--stack-eventbridge)",
  };

  const moving = $derived(
    [...traces].sort((a, b) => new Date(b.startedAt).getTime() - new Date(a.startedAt).getTime()).slice(0, 7),
  );

  // ── Inside a request ─────────────────────────────────────────
  // One request laid out span by span: the latest, the slowest, or the
  // latest that failed.
  type TraceView = "latest" | "slowest" | "failed";
  let traceView = $state<TraceView>("latest");
  const traceChoices = $derived(
    [
      { id: "latest" as const, label: "Latest", trace: moving[0] },
      { id: "slowest" as const, label: "Slowest", trace: [...traces].sort((a, b) => b.durationMs - a.durationMs)[0] },
      { id: "failed" as const, label: "Failed", trace: failedTraces[0] },
    ].filter((c) => c.trace),
  );
  const shown = $derived((traceChoices.find((c) => c.id === traceView) ?? traceChoices[0])?.trace);

  const WATERFALL_ROWS = 9;
  const waterfall = $derived.by(() => {
    const t = shown;
    if (!t) return { total: 1, rows: [] };
    const t0 = new Date(t.startedAt).getTime();
    // Spans without a start are laid end to end, in the order they came.
    const timed = t.spans.every((sp) => sp.startedAt);
    let cursor = 0;
    const spans = t.spans.slice(0, WATERFALL_ROWS).map((sp) => {
      const start = timed ? Math.max(0, new Date(sp.startedAt!).getTime() - t0) : cursor;
      cursor = start + sp.durationMs;
      return { sp, start };
    });
    const total = Math.max(t.durationMs, ...spans.map((r) => r.start + r.sp.durationMs), 1);
    return {
      total,
      rows: spans.map((r) => ({ ...r, left: (r.start / total) * 100, width: Math.max(0.8, (r.sp.durationMs / total) * 100) })),
    };
  });

  // ── Queues ───────────────────────────────────────────────────
  const busiestQueues = $derived(
    [...queues]
      .map((q) => ({ q, total: q.approxVisible + q.approxInFlight + q.approxDelayed }))
      .sort((a, b) => b.total - a.total || a.q.name.localeCompare(b.q.name))
      .slice(0, 6),
  );
  const queuePeak = $derived(Math.max(1, ...busiestQueues.map((x) => x.total)));

  // ── Stack ────────────────────────────────────────────────────
  const stackGroups = $derived(stackGroupsFromOverview(data));
  const stackResources = $derived(stackGroups.reduce((n, g) => n + g.rows.filter((r) => !r.shared).length, 0));

  // ── Seam ─────────────────────────────────────────────────────
  const headline = $derived(
    !data && !dashboard.error
      ? "Reading the water…"
      : attention.length
        ? `${attention.length} ${attention.length === 1 ? "thing needs" : "things need"} a look`
        : "All calm on the tarn",
  );

  const meta = $derived(
    [
      data?.config.region,
      data?.config.accountId ? `account ${data.config.accountId}` : "",
      data?.config.version ? `v${data.config.version.replace(/^v/, "")}` : "",
      dashboard.lastRefresh ? `synced ${dashboard.lastRefresh}` : "",
    ].filter(Boolean),
  );

  const quick = [
    { label: "Invoke a function", tab: "functions", icon: LightningIcon },
    { label: "Send to a queue", tab: "queues", icon: PaperPlaneTiltIcon },
    { label: "Tail logs", tab: "logs", icon: ScrollIcon },
    { label: "Traces", tab: "xray", icon: DetectiveIcon },
    { label: "Topology", tab: "home?section=topology", icon: GraphIcon },
  ];

  // ── The map ──────────────────────────────────────────────────
  // It is as tall as the stage and the stage snaps to it, so scrolling to
  // the foot of Home lands in it. Exploring hands the wheel to the canvas
  // and holds the page still; Esc gives it back.
  let root = $state<HTMLDivElement>();
  let mapEl = $state<HTMLElement>();
  let stageH = $state(0);
  let exploring = $state(false);
  let map = $state<ReturnType<typeof TopologyCanvas>>();
  const isMac = /mac/i.test(navigator.platform);

  $effect(() => {
    const main = root?.closest("main");
    if (!main) return;
    const ro = new ResizeObserver(() => (stageH = main.clientHeight));
    ro.observe(main);
    main.style.scrollSnapType = "y proximity";
    // The banner is the top edge: no rubber band pulling past it.
    main.style.overscrollBehaviorY = "none";
    return () => {
      ro.disconnect();
      main.style.scrollSnapType = "";
      main.style.overscrollBehaviorY = "";
    };
  });

  $effect(() => {
    const main = root?.closest("main");
    if (!main || !exploring) return;
    main.style.overflowY = "hidden";
    return () => (main.style.overflowY = "");
  });

  function scrollToTopology() {
    const still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    mapEl?.scrollIntoView({ behavior: still ? "auto" : "smooth", block: "start" });
  }

  $effect(() => {
    if (!topologyRequested || !mapEl || !stageH) return;
    // Wait for the map's measured stage height to reach the DOM.
    tick().then(() => mapEl?.scrollIntoView({ behavior: "auto", block: "start" }));
  });

  function explore(on: boolean) {
    exploring = on;
    if (on) scrollToTopology();
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape" && exploring) {
      // Esc with a menu open closes the menu, not the map.
      if (document.querySelector('[role="menu"]')) return;
      explore(false);
      return;
    }
    if (!(e.metaKey || e.ctrlKey)) return;
    const target = e.target;
    if (
      target instanceof HTMLElement &&
      (target.isContentEditable ||
        target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.tagName === "SELECT")
    ) {
      return;
    }
    const key = e.key.toLowerCase();
    // ⌘E opens the map, ⌘D closes it (and not the browser's bookmark).
    if (key === "e" && !exploring) {
      e.preventDefault();
      explore(true);
    } else if (key === "d" && exploring) {
      e.preventDefault();
      explore(false);
    }
  }

  const logPeak = $derived(Math.max(1, ...logPulse.bars));
  const logTotal = $derived(logPulse.bars.reduce((s, n) => s + n, 0));
</script>

<svelte:window onkeydown={onKeydown} />

<div class="home" bind:this={root}>
  <div class="hero" class:bare={banner.style === "off"}>
    {#if banner.style !== "off"}
    <LivingTarn
      class="tarn"
      position={0.7}
      fade={0.3}
      {buoys}
      alert={attention.length > 0}
      onOpen={() => onNavigate("functions")}
    />
    {/if}
    <button
      type="button"
      class="sidebar-toggle"
      onclick={onToggleSidebar}
      aria-label={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
      title={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
    >
      <SidebarSimpleIcon size={14} weight={sidebarCollapsed ? "regular" : "fill"} />
    </button>
  </div>

  <div class="seam" class:bare={banner.style === "off"}>
    <h1>{headline}</h1>
    {#if meta.length}
      <p class="meta">{meta.join(" · ")}</p>
    {/if}
    <button type="button" class="search" onclick={onOpenPalette}>
      <MagnifyingGlassIcon size={14} />
      <span>Search functions, queues, tables, pages…</span>
      <kbd>⌘K</kbd>
    </button>
    <div class="quick">
      {#each quick as q (q.tab)}
        {@const Icon = q.icon}
        <button type="button" class="chip" onclick={() => q.tab === "home?section=topology" ? scrollToTopology() : onNavigate(q.tab)}>
          <Icon size={12} />
          {q.label}
        </button>
      {/each}
    </div>
  </div>

  <div class="grid">
    <div class="col">
      <RcPanel title="Needs a look" description={attention.length ? "Each opens where to fix it" : "Errors, dead letters and unhealthy services land here"} index={0}>
        {#if attention.length}
          <ul class="rows">
            {#each attention.slice(0, 8) as a (a.id)}
              <li>
                <button type="button" class="row" onclick={a.go}>
                  <span class="tone" data-tone={a.tone}>
                    {#if a.tone === "red"}<WarningCircleIcon size={14} weight="fill" />{:else}<WarningIcon size={14} weight="fill" />{/if}
                  </span>
                  <span class="primary mono">{a.title}</span>
                  <span class="secondary">{a.detail}</span>
                  <ArrowRightIcon size={12} class="go" />
                </button>
              </li>
            {/each}
          </ul>
          {#if attention.length > 8}
            <p class="more">and {attention.length - 8} more</p>
          {/if}
        {:else}
          <p class="empty">Nothing needs you. Failed functions, 5xx responses, dead letters and unreachable services show up here.</p>
        {/if}
      </RcPanel>

      <RcPanel title="Recent activity" description="The latest requests through your stack" index={2}>
        {#snippet actions()}
          <button type="button" class="link" onclick={() => onNavigate("xray")}>All traces <CaretRightIcon size={11} /></button>
        {/snippet}
        {#if moving.length}
          <ul class="rows muted">
            {#each moving as t (t.id)}
              {@const total = Math.max(t.durationMs, t.spans.reduce((s, sp) => s + sp.durationMs, 0), 1)}
              <li>
                <button type="button" class="row" onclick={() => onOpenTrace(t.id)}>
                  <span class="status mono" data-tone={t.status >= 500 ? "red" : t.status >= 400 ? "amber" : "green"}>{t.status}</span>
                  <span class="primary mono">{traceLabel(t)}</span>
                  <span class="chain" aria-hidden="true">
                    {#each t.spans.slice(0, 8) as sp, i (i)}
                      <span style:width="{Math.max(4, Math.round((sp.durationMs / total) * 72))}px" style:background={SPAN_COLOR[sp.kind] ?? "var(--text-tertiary)"}></span>
                    {/each}
                  </span>
                  <span class="num mono">{fmtMs(t.durationMs)}</span>
                  <span class="num ago">{timeAgo(t.startedAt, now)}</span>
                </button>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="empty">No requests yet. Call an API Gateway route or invoke a function and it appears here.</p>
        {/if}
      </RcPanel>

      <RcPanel title="Queues" description="Backlog by queue: waiting, in flight and delayed" index={4}>
        {#snippet actions()}
          <button type="button" class="link" onclick={() => onNavigate("queues")}>Open queues <CaretRightIcon size={11} /></button>
        {/snippet}
        {#if busiestQueues.length}
          <ul class="rows muted">
            {#each busiestQueues as { q, total } (q.name)}
              <li>
                <button type="button" class="row" onclick={() => onNavigate("queues")}>
                  <span class="primary mono">{q.name}</span>
                  <span class="backlog" aria-hidden="true">
                    <span class="visible" style:width="{(q.approxVisible / queuePeak) * 100}%"></span>
                    <span class="inflight" style:width="{(q.approxInFlight / queuePeak) * 100}%"></span>
                    <span class="delayed" style:width="{(q.approxDelayed / queuePeak) * 100}%"></span>
                  </span>
                  <span class="num mono" title="≈ {q.approxVisible} waiting · ≈ {q.approxInFlight} in flight · ≈ {q.approxDelayed} delayed">≈ {total}</span>
                </button>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="empty">No queues. Create one with the AWS CLI or SDK against Tarn's endpoint.</p>
        {/if}
      </RcPanel>

      <RcPanel
        title="Inside a request"
        description={shown ? `${traceLabel(shown)} · ${shown.status} · ${timeAgo(shown.startedAt, now)}` : "Where a request's time goes, span by span"}
        index={6}
      >
        {#snippet actions()}
          {#if traceChoices.length > 1}
            <div class="seg" role="radiogroup" aria-label="Which request">
              {#each traceChoices as c (c.id)}
                <button type="button" role="radio" aria-checked={shown === c.trace} class:on={shown === c.trace} onclick={() => (traceView = c.id)}>{c.label}</button>
              {/each}
            </div>
          {/if}
        {/snippet}
        {#if shown && waterfall.rows.length}
          {#key shown.id}
            <button type="button" class="waterfall" onclick={() => onOpenTrace(shown.id)} aria-label="Open trace {traceLabel(shown)}">
              {#each waterfall.rows as r, i (i)}
                <span class="wf-row">
                  <span class="wf-name mono">{r.sp.name}</span>
                  <span class="wf-track">
                    <span
                      class="wf-bar"
                      class:error={r.sp.status === "error"}
                      style:left="{r.left}%"
                      style:width="{r.width}%"
                      style:background={SPAN_COLOR[r.sp.kind] ?? "var(--text-tertiary)"}
                      style:--i={i}
                    ></span>
                  </span>
                  <span class="wf-ms mono">{fmtMs(r.sp.durationMs)}</span>
                </span>
              {/each}
              <span class="wf-axis mono">
                <span>0</span>
                {#if shown.spans.length > WATERFALL_ROWS}<span>+{shown.spans.length - WATERFALL_ROWS} more spans</span>{/if}
                <span>{fmtMs(waterfall.total)}</span>
              </span>
            </button>
          {/key}
        {:else}
          <p class="empty">No requests yet. Once one runs, its spans line up here: where it went and how long each step took.</p>
        {/if}
      </RcPanel>
    </div>

    <div class="col">
      <RcPanel title="Log pulse (past minute)" description={logPulse.status === "ready" ? `${logTotal} ${logTotal === 1 ? "event" : "events"} across ${data?.counts.logGroups ?? 0} ${data?.counts.logGroups === 1 ? "group" : "groups"}` : logPulse.status === "loading" ? "Reading recent activity" : "Log activity unavailable"} index={1}>
        {#snippet actions()}
          <button type="button" class="link" onclick={() => onNavigate("logs")}>Tail <CaretRightIcon size={11} /></button>
        {/snippet}
        <button type="button" class="pulse" onclick={() => onNavigate("logs")} aria-label="Open logs">
          {#each logPulse.bars as count, i (i)}
            <span
              data-severity={logPulse.severity[i]}
              style:height="{logPulse.status === 'ready' && count > 0 ? 12 + (count / logPeak) * 88 : 6}%"
              style:opacity={logPulse.status === "ready" && count > 0 ? 1 : 0.25}
            ></span>
          {/each}
        </button>
        <div class="pulse-legend">
          <span data-tone={logPulse.recentErrors ? "red" : undefined}>{logPulse.recentErrors} errors</span>
          <span data-tone={logPulse.recentWarnings ? "amber" : undefined}>{logPulse.recentWarnings} warnings</span>
          <span>in the past 5 minutes</span>
        </div>
      </RcPanel>

      <RcPanel title="Services" description="Databases, caches and apps Tarn can reach" index={3}>
        {#snippet actions()}
          <button type="button" class="link" onclick={() => onNavigate("services")}>Manage <CaretRightIcon size={11} /></button>
        {/snippet}
        {#if infra.length}
          <ul class="rows muted">
            {#each infra.slice(0, 7) as p (`${p.kind}-${p.host}-${p.port}`)}
              <li>
                <button type="button" class="row" onclick={() => onNavigate("services")}>
                  <span class="primary mono">{p.name}</span>
                  <span class="secondary">{p.kind}</span>
                  <span class="num mono">{p.status === "connected" ? `${p.latencyMs}ms` : ""}</span>
                  <span class="state" data-tone={p.status === "connected" ? "green" : "red"}>{p.status}</span>
                </button>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="empty">No services yet. Add a database, cache or local app in Services to watch its health.</p>
        {/if}
      </RcPanel>

      <RcPanel title="Your stack" description="{stackGroups.length} {stackGroups.length === 1 ? 'stack' : 'stacks'} · {stackResources} {stackResources === 1 ? 'resource' : 'resources'}" index={5}>
        {#snippet actions()}
          <button type="button" class="link" onclick={() => onNavigate("stack")}>Open map <CaretRightIcon size={11} /></button>
        {/snippet}
        {#if stackGroups.length}
          <StackHex groups={stackGroups} compact onOpen={() => onNavigate("stack")} />
        {:else}
          <p class="empty">No resources yet. Deploy something and it shows up here as a stack.</p>
        {/if}
      </RcPanel>
    </div>
  </div>

  <section class="map" class:exploring bind:this={mapEl} style:--stage-h={stageH ? `${stageH}px` : undefined} aria-label="Topology">
    <div class="map-frame">
      <header class="map-head">
        <div>
          <h2>Topology</h2>
          <p>{exploring ? "Drag or scroll to pan · ⌘-scroll to zoom · ⌘D or Esc when done" : "Everything Tarn runs, and how it connects"}</p>
        </div>
        <div class="map-filter"><TagFilter /></div>
        <div class="map-actions">
          <button type="button" class="map-search" onclick={onOpenPalette} aria-label="Search resources and pages" title="Search resources and pages (Cmd/Ctrl+K)"><MagnifyingGlassIcon size={14} /><span>Search</span></button>
          {#if exploring}
            <button type="button" class="link" onclick={() => map?.recentre()} title="Re-centre (⌘C)">Re-centre</button>

          {/if}
          <button type="button" class="explore" class:on={exploring} aria-pressed={exploring} onclick={() => explore(!exploring)} title={exploring ? "Done (⌘D)" : "Explore (⌘E)"}>
            <span>{exploring ? "Done" : "Explore"}</span>
            <span class="hint" aria-hidden="true">{exploring ? (isMac ? "⌘D" : "Ctrl D") : (isMac ? "⌘E" : "Ctrl E")}</span>
          </button>
        </div>
      </header>
      <div class="map-canvas">
        <TopologyCanvas bind:this={map} bare canvasExpanded={exploring} {onNavigate} onExpandedChange={explore} />
      </div>
    </div>
  </section>
</div>

<style>
  .home {
    position: relative;
    min-height: 100%;
  }

  /* ── Hero ── */
  .hero {
    position: relative;
    height: clamp(260px, 52vh, 520px);
  }
  .hero.bare { height: 56px; }
  .hero :global(.tarn) {
    position: absolute;
    inset: 0;
  }
  .sidebar-toggle {
    position: absolute;
    top: 14px;
    left: 14px;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
    background: color-mix(in srgb, var(--bg-stage) 70%, transparent);
    color: var(--text-secondary);
    transition: color 120ms ease, background 120ms ease;
  }
  .sidebar-toggle:hover { color: var(--text-primary); background: var(--bg-stage); }

  /* ── Seam ── */
  .seam {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    width: 100%;
    max-width: 640px;
    margin: -8px auto 0;
    padding: 0 24px;
    text-align: center;
    animation: seamIn 320ms var(--ease-snappy) both;
  }
  .seam.bare { margin-top: 24px; }
  h1 {
    font-size: 22px;
    font-weight: 600;
    letter-spacing: -0.02em;
    color: var(--text-primary);
    text-wrap: balance;
  }
  .meta {
    margin-top: 4px;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--text-secondary);
  }
  .search {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    height: 40px;
    margin-top: 16px;
    padding: 0 12px;
    border: 1px solid var(--border-default);
    border-radius: 8px;
    background: var(--bg-element);
    color: var(--text-tertiary);
    font-size: 13px;
    text-align: left;
    transition: border-color 120ms ease, background 120ms ease;
  }
  .search span { flex: 1; }
  .search:hover { border-color: var(--border-focus); }
  .search:active { transform: scale(0.99); }
  kbd {
    padding: 1px 6px;
    border: 1px solid var(--border-subtle);
    border-radius: 5px;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--text-secondary);
  }
  .quick {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: 6px;
    margin-top: 10px;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 26px;
    padding: 0 10px;
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
    color: var(--text-secondary);
    font-size: 12px;
    transition: color 120ms ease, background 120ms ease, transform 200ms var(--ease-snappy);
  }
  .chip:hover { color: var(--text-primary); background: var(--bg-element-hover); }
  .chip:active { transform: scale(0.96); }

  /* ── Grid ── */
  .grid {
    display: grid;
    grid-template-columns: minmax(0, 7fr) minmax(0, 5fr);
    gap: 0 40px;
    width: 100%;
    max-width: 1180px;
    margin: 36px auto 0;
    padding: 0 28px;
  }
  @media (max-width: 960px) {
    .grid { grid-template-columns: minmax(0, 1fr); }
  }
  .col { min-width: 0; }

  .rows { list-style: none; margin: 0; padding: 0; }
  .rows li + li { border-top: 1px solid var(--border-subtle); }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    min-width: 0;
    height: 34px;
    padding: 0 8px;
    border-radius: 6px;
    font-size: 12.5px;
    text-align: left;
    transition: background 120ms ease;
  }
  .row:hover,
  .row:focus-visible { background: var(--bg-element-hover); outline: none; }
  .row :global(.go) { flex-shrink: 0; color: var(--text-tertiary); opacity: 0; transition: opacity 120ms ease; }
  .row:hover :global(.go) { opacity: 1; }
  /* Lists that are there to glance at: names a step back, lit on hover. */
  .rows.muted .primary { color: var(--text-secondary); transition: color 120ms ease; }
  .rows.muted .row:hover .primary,
  .rows.muted .row:focus-visible .primary { color: var(--text-primary); }

  .mono { font-family: var(--font-mono); }
  .primary {
    min-width: 0;
    max-width: 46%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-primary);
    font-size: 12px;
  }
  .secondary {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-secondary);
  }
  .num {
    flex-shrink: 0;
    font-size: 11px;
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
  }
  .ago { width: 56px; text-align: right; font-size: 11px; }

  .tone { display: flex; flex-shrink: 0; }
  [data-tone="red"] { color: var(--accent-red); }
  [data-tone="amber"] { color: var(--accent-amber); }
  [data-tone="green"] { color: var(--accent-green); }

  .status { flex-shrink: 0; width: 28px; font-size: 11px; }

  .chain {
    display: flex;
    flex: 1;
    justify-content: flex-end;
    gap: 2px;
    min-width: 0;
  }
  .chain span { height: 4px; border-radius: 2px; opacity: 0.85; }

  .backlog {
    display: flex;
    flex: 1;
    height: 6px;
    overflow: hidden;
    border-radius: 3px;
    background: var(--bg-element);
  }
  .backlog .visible { background: var(--accent-amber); }
  .backlog .inflight { background: var(--accent-green); }
  .backlog .delayed { background: var(--text-tertiary); opacity: 0.6; }

  .state {
    flex-shrink: 0;
    padding: 1px 7px;
    border: 1px solid color-mix(in srgb, currentColor 45%, transparent);
    border-radius: 6px;
    background: color-mix(in srgb, currentColor 10%, transparent);
    font-size: 10.5px;
  }

  .empty {
    padding: 4px 8px;
    font-size: 12px;
    line-height: 1.5;
    color: var(--text-secondary);
  }
  .more { padding: 6px 8px 0; font-size: 11px; color: var(--text-tertiary); }

  .link {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 11.5px;
    color: var(--text-secondary);
    transition: color 120ms ease;
  }
  .link:hover { color: var(--text-primary); }

  /* ── Log pulse ── */
  .pulse {
    display: flex;
    align-items: flex-end;
    gap: 3px;
    width: 100%;
    height: 64px;
    padding: 0 8px;
  }
  .pulse span {
    flex: 1;
    min-height: 3px;
    border-radius: 2px;
    background: var(--accent-green);
    transition: height 280ms var(--ease-snappy);
  }
  .pulse span[data-severity="1"] { background: var(--accent-amber); }
  .pulse span[data-severity="2"] { background: var(--accent-red); }
  .pulse-legend {
    display: flex;
    gap: 14px;
    margin-top: 8px;
    padding: 0 8px;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--text-secondary);
  }


  /* ── Inside a request ── */
  .seg {
    display: flex;
    gap: 2px;
    padding: 2px;
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
  }
  .seg button {
    height: 20px;
    padding: 0 8px;
    border-radius: 6px;
    font-size: 11px;
    color: var(--text-secondary);
    transition: color 120ms ease, background 120ms ease;
  }
  .seg button:hover { color: var(--text-primary); }
  .seg button.on { background: var(--bg-element); color: var(--text-primary); }
  .waterfall {
    display: flex;
    flex-direction: column;
    gap: 2px;
    width: 100%;
    padding: 4px 8px 6px;
    border-radius: 6px;
    text-align: left;
    transition: background 120ms ease;
  }
  .waterfall:hover,
  .waterfall:focus-visible { background: var(--bg-element-hover); outline: none; }
  .wf-row {
    display: grid;
    grid-template-columns: minmax(0, 32%) minmax(0, 1fr) 48px;
    align-items: center;
    gap: 10px;
    height: 22px;
  }
  .wf-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 11px;
    color: var(--text-secondary);
  }
  .waterfall:hover .wf-name { color: var(--text-primary); }
  .wf-track {
    position: relative;
    height: 8px;
    border-radius: 3px;
    background: var(--bg-element);
  }
  .wf-bar {
    position: absolute;
    top: 0;
    bottom: 0;
    border-radius: 3px;
    opacity: 0.9;
    transform-origin: left;
    animation: wfGrow 420ms var(--ease-snappy) both;
    animation-delay: calc(var(--i) * 40ms);
  }
  .wf-bar.error { outline: 1px solid var(--accent-red); outline-offset: 1px; }
  .wf-ms {
    font-size: 10.5px;
    text-align: right;
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
  }
  .wf-axis {
    display: flex;
    justify-content: space-between;
    margin-top: 4px;
    padding-left: calc(32% + 10px);
    padding-right: 58px;
    font-size: 10px;
    color: var(--text-secondary);
  }

  /* ── The map ── */
  .map {
    height: var(--stage-h, 100vh);
    margin-top: 40px;
    scroll-snap-align: start;
  }
  /* It fills the stage's cutout edge to edge: no frame of its own, the
     stage's rounded corners are its corners. */
  .map-frame {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
    transform-origin: 50% 0;
  }
  /* Arriving, it opens out from an inset card to the whole stage. */
  @supports (animation-timeline: view()) {
    .map-frame {
      animation: mapArrive linear both;
      animation-timeline: view();
      animation-range: entry 0% cover 45%;
    }
  }
  .map-head {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    justify-content: space-between;
    gap: 12px;
    padding: 14px 22px 12px;
  }
  .map-head h2 { font-size: 13px; font-weight: 600; color: var(--text-primary); }
  .map-head p { margin-top: 2px; font-size: 12px; color: var(--text-secondary); }
  .map-filter { flex: 1 1 240px; max-width: 440px; min-width: 0; }
  .map-search { display: flex; align-items: center; gap: 6px; min-height: 28px; padding: 0 6px; border-radius: 4px; color: var(--text-secondary); font-size: 12px; }
  .map-search:hover { color: var(--text-primary); background: var(--bg-element-hover); }
  .map-search:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }
  .map-actions { display: flex; align-items: center; gap: 12px; }
  .explore {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 26px;
    padding: 0 12px;
    border: 1px solid var(--border-default);
    border-radius: 8px;
    font-size: 12px;
    color: var(--text-primary);
    transition: background 120ms ease, border-color 120ms ease, transform 160ms var(--ease-snappy);
  }
  .explore:hover { background: var(--bg-element-hover); border-color: var(--border-focus); }
  .explore:active { transform: scale(0.96); }
  .explore.on { background: var(--text-primary); border-color: var(--text-primary); color: var(--bg-stage); }
  .explore .hint {
    font-family: var(--font-mono);
    font-size: 10px;
    line-height: 1;
    padding: 1px 5px;
    border: 1px solid color-mix(in srgb, currentColor 30%, transparent);
    border-radius: 4px;
    opacity: 0.7;
    color: inherit;
  }
  .map-canvas { flex: 1; min-height: 0; }

  @keyframes wfGrow { from { transform: scaleX(0); } }
  @keyframes mapArrive {
    from { scale: 0.92; opacity: 0.4; border-radius: 14px; }
    to { scale: 1; opacity: 1; border-radius: 0; }
  }
  @keyframes seamIn { from { opacity: 0; transform: translateY(6px); } }
  @media (prefers-reduced-motion: reduce) {
    .seam,
    .wf-bar,
    .map-frame { animation: none; }
    .pulse span { transition: none; }
  }
</style>
