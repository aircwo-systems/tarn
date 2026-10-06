<script lang="ts">
  import { onMount, tick } from "svelte";
  import { fade, fly } from "svelte/transition";
  import { cubicOut } from "svelte/easing";
  import {
    XIcon,
    MagnifyingGlassIcon,
    LightningIcon,
    GlobeHemisphereWestIcon,
    ChatCircleIcon,
    BellIcon,
    HardDriveIcon,
    DatabaseIcon,
    KeyIcon,
    BridgeIcon,
    FlowArrowIcon,
    ScrollIcon,
    DetectiveIcon,
    CubeIcon,
    GearIcon,
    ShieldWarningIcon,
    StackIcon,
    ArrowsClockwiseIcon,
    IdentificationBadgeIcon,
  } from "phosphor-svelte";
  import { getDashboard, refresh } from "$lib/state.svelte";
  import { timeAgo } from "$lib/utils";
  import type { Component } from "svelte";

  type FilterId = "pages" | "actions" | "functions" | "queues" | "apis" | "tables" | "buckets" | "secrets" | "pools" | "rules" | "machines";

  export interface PaletteItem {
    id: string;
    title: string;
    subtitle?: string;
    group: string;
    filter: FilterId;
    keywords?: string;
    mono?: boolean;
    tag?: string;
    icon: Component<any>;
    action: () => void;
  }

  let {
    open = $bindable(false),
    onNavigate,
  }: {
    open: boolean;
    onNavigate: (target: string) => void;
  } = $props();

  const dashboard = getDashboard();

  const FILTERS: Array<{ id: FilterId; label: string }> = [
    { id: "pages",     label: "Pages" },
    { id: "functions", label: "Functions" },
    { id: "queues",    label: "Queues" },
    { id: "apis",      label: "APIs" },
    { id: "tables",    label: "Tables" },
    { id: "buckets",   label: "Buckets" },
    { id: "secrets",   label: "Secrets" },
    { id: "pools",     label: "User pools" },
    { id: "rules",     label: "Rules" },
    { id: "machines",  label: "State machines" },
    { id: "actions",   label: "Actions" },
  ];

  let query = $state("");
  let filter = $state<FilterId | "all">("all");
  let selectedIndex = $state(0);
  let inputEl: HTMLInputElement | null = $state(null);
  let listEl: HTMLDivElement | null = $state(null);

  $effect(() => {
    if (open) {
      query = "";
      filter = "all";
      selectedIndex = 0;
      pillAnimate = false;
      tick().then(() => inputEl?.focus());
    }
  });

  const allItems = $derived.by(() => {
    const data = dashboard.data;
    const items: PaletteItem[] = [];
    const page = (id: string, title: string, subtitle: string, icon: Component<any>, keywords = "") =>
      items.push({ id: `nav-${id}`, title, subtitle, group: "Pages", filter: "pages", keywords, icon, action: () => onNavigate(id) });
    const count = (n: number | undefined, one: string, many = `${one}s`) => `${n ?? 0} ${n === 1 ? one : many}`;

    page("overview",      "Overview",        "Health and resource topology",              StackIcon, "topology home");
    page("functions",     "Functions",       count(data?.functions?.length, "function"),  LightningIcon, "lambda fn");
    page("gateways",      "API Gateway",     count(data?.gateways?.length, "API"),        GlobeHemisphereWestIcon, "apigw gw http");
    page("queues",        "Queues",          count(data?.queues?.length, "queue"),        ChatCircleIcon, "sqs");
    page("topics",        "Topics",          count(data?.topics?.length, "topic"),        BellIcon, "sns pubsub");
    page("dynamodb",      "DynamoDB",        count(data?.dynamodbTables?.length, "table"), DatabaseIcon, "ddb tables");
    page("storage",       "Storage",         count(data?.buckets?.length, "bucket"),      HardDriveIcon, "s3 buckets");
    page("secrets",       "Secrets",         count(data?.secrets?.length, "secret"),      KeyIcon, "secrets manager");
    page("cognito",       "Cognito",         count(data?.cognitoPools?.length, "user pool"), IdentificationBadgeIcon, "cognito user pools auth users codes jwt tokens");
    page("eventbridge",   "EventBridge",     count(data?.eventBridgeRules?.length, "rule"), BridgeIcon, "events rules schedule cron");
    page("stepfunctions", "Step Functions",  count(data?.stateMachines?.length, "state machine"), FlowArrowIcon, "sfn states");
    page("ecs",           "ECS",             "Clusters, services and tasks",              CubeIcon, "containers docker");
    page("logs",          "Logs",            count(data?.counts?.logGroups, "log group"), ScrollIcon, "cloudwatch");
    page("xray",          "X-Ray",           count(data?.recentTraces?.length, "recent trace"), DetectiveIcon, "traces tracing");
    page("chaos",         "Chaos",           "Fault injection and latency rules",         ShieldWarningIcon, "faults disruptors");
    page("settings",      "Settings",        "Accounts, appearance and polling",          GearIcon, "preferences theme");

    items.push({
      id: "act-refresh",
      title: "Refresh now",
      subtitle: "Re-poll the backend and probe infrastructure",
      group: "Actions",
      filter: "actions",
      keywords: "reload poll",
      icon: ArrowsClockwiseIcon,
      action: () => void refresh(),
    });

    for (const fn of data?.functions ?? []) {
      items.push({
        id: `fn-${fn.name}`,
        title: fn.name,
        subtitle: `${fn.runtime} · ${fn.memoryMB} MB · ${fn.timeoutSec}s timeout`,
        group: "Functions",
        filter: "functions",
        keywords: "lambda fn",
        mono: true,
        icon: LightningIcon,
        action: () => onNavigate(`functions?function=${encodeURIComponent(fn.name)}`),
      });
    }

    for (const q of data?.queues ?? []) {
      items.push({
        id: `q-${q.name}`,
        title: q.name,
        subtitle: `${q.approxVisible} visible · ${q.approxInFlight} in flight`,
        group: "Queues",
        filter: "queues",
        keywords: "sqs",
        mono: true,
        tag: q.fifo ? "FIFO" : undefined,
        icon: ChatCircleIcon,
        action: () => onNavigate(`queues?queue=${encodeURIComponent(q.name)}`),
      });
    }

    for (const gw of data?.gateways ?? []) {
      items.push({
        id: `gw-${gw.apiId}`,
        title: gw.name || gw.apiId,
        subtitle: `${gw.protocolType} · ${count(gw.routes, "route")}`,
        group: "APIs",
        filter: "apis",
        keywords: `apigw gw ${gw.apiId}`,
        mono: true,
        icon: GlobeHemisphereWestIcon,
        action: () => onNavigate(`gateways?api=${encodeURIComponent(gw.apiId)}`),
      });
    }

    for (const tbl of data?.dynamodbTables ?? []) {
      items.push({
        id: `tbl-${tbl.name}`,
        title: tbl.name,
        subtitle: `${count(tbl.itemCount, "item")} · ${tbl.keySchema}`,
        group: "Tables",
        filter: "tables",
        keywords: "dynamodb ddb",
        mono: true,
        tag: tbl.status && tbl.status !== "ACTIVE" ? tbl.status.toLowerCase() : undefined,
        icon: DatabaseIcon,
        action: () => onNavigate(`dynamodb?table=${encodeURIComponent(tbl.name)}`),
      });
    }

    for (const b of data?.buckets ?? []) {
      items.push({
        id: `bucket-${b.name}`,
        title: b.name,
        subtitle: b.createdDate ? `Created ${timeAgo(b.createdDate)}` : "Bucket",
        group: "Buckets",
        filter: "buckets",
        keywords: "s3 storage",
        mono: true,
        icon: HardDriveIcon,
        action: () => onNavigate(`storage?bucket=${encodeURIComponent(b.name)}`),
      });
    }

    for (const s of data?.secrets ?? []) {
      items.push({
        id: `sec-${s.name}`,
        title: s.name,
        subtitle: s.description || "Secret",
        group: "Secrets",
        filter: "secrets",
        keywords: "secrets manager",
        mono: true,
        icon: KeyIcon,
        action: () => onNavigate(`secrets?secret=${encodeURIComponent(s.name)}`),
      });
    }

    for (const p of data?.cognitoPools ?? []) {
      items.push({
        id: `pool-${p.id}`,
        title: p.name,
        subtitle: `${p.id} · ${count(p.users, "user")}${p.pendingCodes ? ` · ${count(p.pendingCodes, "code")} waiting` : ""}`,
        group: "User pools",
        filter: "pools",
        keywords: `cognito user pool ${p.id}`,
        icon: IdentificationBadgeIcon,
        action: () => onNavigate(`cognito?pool=${encodeURIComponent(p.id)}`),
      });
    }

    for (const r of data?.eventBridgeRules ?? []) {
      items.push({
        id: `rule-${r.name}`,
        title: r.name,
        subtitle: r.scheduleExpression || "Event pattern",
        group: "Rules",
        filter: "rules",
        keywords: "eventbridge events",
        mono: true,
        tag: r.state === "DISABLED" ? "disabled" : undefined,
        icon: BridgeIcon,
        action: () => onNavigate(`eventbridge?rule=${encodeURIComponent(r.name)}`),
      });
    }

    for (const sm of data?.stateMachines ?? []) {
      const type = (sm.type || "STANDARD").toLowerCase();
      items.push({
        id: `sm-${sm.name}`,
        title: sm.name,
        subtitle: `${type[0].toUpperCase()}${type.slice(1)} state machine`,
        group: "State machines",
        filter: "machines",
        keywords: "step functions sfn",
        mono: true,
        icon: FlowArrowIcon,
        action: () => onNavigate(`stepfunctions?name=${encodeURIComponent(sm.name)}`),
      });
    }

    return items;
  });

  // Query matches across every filter, so chip counts follow the search.
  const matchedItems = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return allItems;

    const matches: Array<{ item: PaletteItem; score: number }> = [];
    for (const item of allItems) {
      const title = item.title.toLowerCase();
      let score = 0;
      if (title === q) score = 100;
      else if (title.startsWith(q)) score = 80;
      else if (title.includes(q)) score = 60;
      else if ((item.subtitle ?? "").toLowerCase().includes(q)) score = 40;
      else if (`${item.group} ${item.keywords ?? ""}`.toLowerCase().includes(q)) score = 20;
      if (score) matches.push({ item, score });
    }
    // Stable sort keeps the catalogue order within a score band.
    matches.sort((a, b) => b.score - a.score);
    return matches.map((m) => m.item);
  });

  // Only offer filters that exist in the catalogue; counts reflect the query.
  const filters = $derived.by(() => {
    const present = new Set(allItems.map((it) => it.filter));
    const counts = new Map<FilterId, number>();
    for (const it of matchedItems) counts.set(it.filter, (counts.get(it.filter) ?? 0) + 1);
    return [
      { id: "all" as const, label: "All", count: matchedItems.length },
      ...FILTERS.filter((f) => present.has(f.id)).map((f) => ({ ...f, count: counts.get(f.id) ?? 0 })),
    ];
  });

  const filteredItems = $derived(
    (filter === "all" ? matchedItems : matchedItems.filter((it) => it.filter === filter)).slice(0, 50),
  );

  const groups = $derived.by(() => {
    const map = new Map<string, Array<{ item: PaletteItem; index: number }>>();
    filteredItems.forEach((item, index) => {
      if (!map.has(item.group)) map.set(item.group, []);
      map.get(item.group)!.push({ item, index });
    });
    return [...map.entries()].map(([name, entries]) => ({ name, entries }));
  });

  $effect(() => {
    if (selectedIndex >= filteredItems.length) {
      selectedIndex = Math.max(0, filteredItems.length - 1);
    }
  });

  // ── Sliding selection pill ────────────────────────────────────────
  let pillTop = $state(0);
  let pillHeight = $state(0);
  let pillVisible = $state(false);
  let pillAnimate = $state(false);

  $effect(() => {
    void selectedIndex;
    void filteredItems;
    if (!open) return;
    tick().then(() => {
      const el = listEl?.querySelector<HTMLElement>(`[data-index="${selectedIndex}"]`);
      if (!el) {
        pillVisible = false;
        return;
      }
      pillTop = el.offsetTop;
      pillHeight = el.offsetHeight;
      pillVisible = true;
      // Skip the slide on first placement so it doesn't sweep in from the top.
      requestAnimationFrame(() => (pillAnimate = true));
    });
  });

  function selectItem(item: PaletteItem) {
    open = false;
    item.action();
  }

  function setFilter(id: FilterId | "all") {
    filter = id;
    selectedIndex = 0;
    listEl?.scrollTo({ top: 0 });
    inputEl?.focus();
  }

  function move(delta: number) {
    if (filteredItems.length === 0) return;
    selectedIndex = (selectedIndex + delta + filteredItems.length) % filteredItems.length;
    tick().then(() => {
      listEl?.querySelector<HTMLElement>(`[data-index="${selectedIndex}"]`)?.scrollIntoView({ block: "nearest" });
    });
  }

  function handleKeydown(e: KeyboardEvent) {
    if (!open) return;
    if (e.key === "Escape") {
      e.preventDefault();
      open = false;
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      move(1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      move(-1);
    } else if (e.key === "Enter") {
      e.preventDefault();
      const item = filteredItems[selectedIndex];
      if (item) selectItem(item);
    } else if (e.key === "Tab") {
      e.preventDefault();
      const i = filters.findIndex((f) => f.id === filter);
      const next = (i + (e.shiftKey ? -1 : 1) + filters.length) % filters.length;
      setFilter(filters[next].id);
    }
  }

  onMount(() => {
    window.addEventListener("keydown", handleKeydown);
    return () => window.removeEventListener("keydown", handleKeydown);
  });
</script>

{#if open}
  <div class="scrim" role="presentation" onclick={() => (open = false)} transition:fade={{ duration: 120 }}></div>

  <div class="frame">
    <div
      class="palette"
      role="dialog"
      aria-modal="true"
      aria-label="Search"
      in:fly={{ y: 6, duration: 240, easing: cubicOut }}
      out:fade={{ duration: 100 }}
    >
      <label class="search">
        <MagnifyingGlassIcon size={15} />
        <input
          bind:this={inputEl}
          bind:value={query}
          oninput={() => (selectedIndex = 0)}
          placeholder="Search resources, pages and actions"
          spellcheck="false"
          autocomplete="off"
          role="combobox"
          aria-expanded="true"
          aria-controls="palette-list"
          aria-activedescendant={filteredItems.length ? `palette-opt-${selectedIndex}` : undefined}
        />
        {#if query}
          <button
            type="button"
            class="icon-btn"
            onclick={() => { query = ""; inputEl?.focus(); }}
            aria-label="Clear search"
          >
            <XIcon size={12} />
          </button>
        {/if}
        <button type="button" class="key-btn" onclick={() => (open = false)} aria-label="Close">Esc</button>
      </label>

      <div class="chips" role="tablist" aria-label="Filter results">
        {#each filters as f (f.id)}
          <button
            type="button"
            role="tab"
            class="chip"
            class:on={filter === f.id}
            aria-selected={filter === f.id}
            onclick={() => setFilter(f.id)}
          >
            {f.label}
            <span class="chip-count">{f.count}</span>
          </button>
        {/each}
      </div>

      <div class="list" id="palette-list" role="listbox" bind:this={listEl}>
        <span
          class="pill"
          class:animate={pillAnimate}
          style:transform="translate3d(0, {pillTop}px, 0)"
          style:height="{pillHeight}px"
          style:opacity={pillVisible ? 1 : 0}
          aria-hidden="true"
        ></span>

        {#each groups as group (group.name)}
          <div class="subhead" role="presentation">
            <span>{group.name}</span>
            <span class="subhead-count">{group.entries.length}</span>
          </div>
          {#each group.entries as { item, index } (item.id)}
            {@const Icon = item.icon}
            {@const active = index === selectedIndex}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <div
              id="palette-opt-{index}"
              class="row"
              class:selected={active}
              data-index={index}
              role="option"
              tabindex="-1"
              aria-selected={active}
              onclick={() => selectItem(item)}
              onpointermove={() => { if (!active) selectedIndex = index; }}
            >
              <span class="row-icon"><Icon size={14} weight={active ? "bold" : "regular"} /></span>
              <span class="row-main">
                <span class="row-title" class:mono={item.mono} title={item.title}>{item.title}</span>
                {#if item.subtitle}
                  <span class="row-sub" title={item.subtitle}>{item.subtitle}</span>
                {/if}
              </span>
              {#if item.tag}
                <span class="tag">{item.tag}</span>
              {/if}
              <kbd class="enter" aria-hidden="true">↵</kbd>
            </div>
          {/each}
        {:else}
          <p class="empty">
            {#if query}No results for “{query}”{:else}Nothing here yet{/if}
          </p>
        {/each}
      </div>

      <footer class="foot">
        <span><kbd>↑</kbd><kbd>↓</kbd> Navigate</span>
        <span><kbd>↵</kbd> Open</span>
        <span><kbd>Tab</kbd> Filter</span>
        <span class="foot-count">{filteredItems.length} {filteredItems.length === 1 ? "result" : "results"}</span>
      </footer>
    </div>
  </div>
{/if}

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 120;
    background: color-mix(in srgb, var(--bg-app) 72%, transparent);
  }

  .frame {
    position: fixed;
    inset: 0;
    z-index: 125;
    display: flex;
    justify-content: center;
    align-items: flex-start;
    padding: 14vh 16px 16px;
    pointer-events: none;
  }

  .palette {
    pointer-events: auto;
    display: flex;
    flex-direction: column;
    width: min(40rem, 100%);
    max-height: min(32rem, 74vh);
    border: 1px solid var(--border-default);
    border-radius: 12px;
    background: var(--bg-stage);
    overflow: hidden;
  }

  /* Search */
  .search, .chips, .foot { flex-shrink: 0; }

  .search {
    display: flex;
    align-items: center;
    gap: 10px;
    height: 46px;
    padding: 0 10px 0 14px;
    border-bottom: 1px solid var(--border-subtle);
    color: var(--text-tertiary);
  }
  .search input {
    flex: 1;
    min-width: 0;
    border: 0;
    outline: none;
    background: transparent;
    font-size: 13.5px;
    color: var(--text-primary);
  }
  .search input::placeholder { color: var(--text-tertiary); }

  .icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    flex-shrink: 0;
    border-radius: 8px;
    color: var(--text-tertiary);
    transition: color 120ms ease, background 120ms ease;
  }
  .icon-btn:hover { color: var(--text-primary); background: var(--bg-element-hover); }

  .key-btn {
    height: 22px;
    padding: 0 7px;
    flex-shrink: 0;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    font-size: 10.5px;
    color: var(--text-tertiary);
    transition: color 120ms ease, border-color 120ms ease, background 120ms ease, transform 120ms ease;
  }
  .key-btn:hover { color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element-hover); }
  .key-btn:active { transform: scale(0.96); }

  /* Filter chips — same as Settings */
  .chips {
    display: flex;
    gap: 4px;
    padding: 8px 10px;
    overflow-x: auto;
    border-bottom: 1px solid var(--border-subtle);
    scrollbar-width: none;
  }
  .chips::-webkit-scrollbar { display: none; }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
    height: 26px;
    padding: 0 10px;
    border-radius: 8px;
    border: 1px solid var(--border-subtle);
    font-size: 11.5px;
    color: var(--text-secondary);
    background: transparent;
    white-space: nowrap;
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .chip:hover { color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element-hover); }
  .chip:active { transform: scale(0.96); }
  .chip.on {
    color: var(--text-primary);
    border-color: color-mix(in srgb, var(--accent-green) 45%, transparent);
    background: color-mix(in srgb, var(--accent-green) 10%, transparent);
  }
  .chip-count {
    font-size: 10.5px;
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  /* Results */
  .list {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 4px 8px 8px;
    scrollbar-width: thin;
    scrollbar-color: var(--border-subtle) transparent;
  }

  .pill {
    position: absolute;
    top: 0;
    left: 8px;
    right: 8px;
    border-radius: 8px;
    background: var(--bg-element);
    border: 1px solid var(--border-default);
    pointer-events: none;
    will-change: transform, height;
  }
  .pill.animate {
    transition: transform 200ms var(--ease-snappy), height 160ms ease, opacity 80ms ease;
  }
  .pill::before {
    content: "";
    position: absolute;
    left: 6px;
    top: 10px;
    bottom: 10px;
    width: 2.5px;
    border-radius: 2px;
    background: var(--text-primary);
    opacity: 0.9;
  }

  .subhead {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 10px 4px;
    font-size: 10.5px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--text-tertiary);
  }
  .subhead-count { font-variant-numeric: tabular-nums; letter-spacing: 0; }

  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 7px 10px;
    border-radius: 8px;
    cursor: pointer;
    transition: padding-left 220ms var(--ease-snappy);
  }
  .row.selected { padding-left: 18px; }

  .row-icon {
    display: flex;
    flex-shrink: 0;
    color: var(--text-tertiary);
    transition: color 120ms ease;
  }
  .row.selected .row-icon { color: var(--text-primary); }

  .row-main { display: flex; flex-direction: column; gap: 1px; flex: 1; min-width: 0; }
  .row-title {
    font-size: 12.5px;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    transition: color 120ms ease;
  }
  .row-title.mono { font-family: var(--font-mono, ui-monospace, monospace); font-size: 12px; }
  .row.selected .row-title { color: var(--text-primary); }
  .row-sub {
    font-size: 10.5px;
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tag {
    flex-shrink: 0;
    height: 20px;
    display: inline-flex;
    align-items: center;
    padding: 0 7px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    font-size: 10.5px;
    color: var(--text-secondary);
  }

  kbd {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 18px;
    height: 18px;
    padding: 0 4px;
    border-radius: 5px;
    border: 1px solid var(--border-subtle);
    font: inherit;
    font-size: 10px;
    color: var(--text-tertiary);
  }
  .enter {
    flex-shrink: 0;
    opacity: 0;
    transition: opacity 120ms ease;
  }
  .row.selected .enter { opacity: 1; }

  .empty {
    padding: 28px 10px;
    text-align: center;
    font-size: 12px;
    color: var(--text-tertiary);
  }

  /* Footer */
  .foot {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 8px 14px;
    border-top: 1px solid var(--border-subtle);
    font-size: 11px;
    color: var(--text-tertiary);
  }
  .foot span { display: inline-flex; align-items: center; gap: 4px; }
  .foot-count { margin-left: auto; font-variant-numeric: tabular-nums; }

  button:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }

  @media (max-width: 560px) {
    .foot span:not(.foot-count) { display: none; }
  }

  @media (prefers-reduced-motion: reduce) {
    .pill.animate, .row, .chip, .key-btn { transition: none; }
  }
</style>
