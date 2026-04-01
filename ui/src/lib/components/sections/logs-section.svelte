<script lang="ts">
  import {
    ScrollIcon,
    MagnifyingGlassIcon,
    FunnelIcon,
    ArrowLeftIcon,
    ArrowsClockwiseIcon,
    CaretDownIcon,
    XIcon,
    TrashIcon,
    SortAscendingIcon,
    SortDescendingIcon,
    ClipboardTextIcon,
  } from "phosphor-svelte";
  import Badge from "$lib/components/ui/badge/badge.svelte";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import EmptyState from "$lib/components/common/empty-state.svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import SectionHeader from "./section-header.svelte";
  import {
    fetchLogGroups,
    fetchLogEvents,
    fetchAllLogEvents,
    clearLogGroup,
    type FetchLogEventsParams,
  } from "$lib/api";
  import { highlightJSON } from "$lib/json-format";
  import type { LogGroupSummary, LogEvent } from "$lib/types";

  let {
    initialGroup = "",
    initialTimestamp = "",
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    initialGroup?: string;
    initialTimestamp?: string;
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  // ── State ────────────────────────────────────────────────────────────
  let groups = $state<LogGroupSummary[]>([]);
  let groupsLoading = $state(true);
  let groupsError = $state("");

  let selectedGroup = $state("");
  let events = $state<LogEvent[]>([]);
  let eventsTotal = $state(0);
  let eventsLoading = $state(false);
  let eventsError = $state("");

  // Filters
  let filterLevel = $state("");
  let filterPattern = $state("");
  let filterStream = $state("");
  let eventsLimit = $state(200);
  let eventsCursor = $state<string | null>(null);
  let prevCursors = $state<string[]>([]);
  let showFilters = $state(false);
  let autoRefresh = $state(false);
  let autoRefreshTimer = $state<ReturnType<typeof setInterval> | null>(null);
  let groupSearch = $state("");
  let serviceFilter = $state("all");
  let sortOrder = $state<"desc" | "asc">("desc");

  // Detail panel
  let selectedEvent = $state<LogEvent | null>(null);

  // Clear logs
  let clearing = $state(false);
  let copyingStream = $state(false);

  // Deep-link highlight
  let highlightTimestamp = $state("");

  // "All" virtual group
  const ALL_GROUP = "__all__";
  const isAllGroup = $derived(selectedGroup === ALL_GROUP);

  // ── Lifecycle ────────────────────────────────────────────────────────
  $effect(() => {
    loadGroups();
  });

  $effect(() => {
    if (initialGroup) {
      selectedGroup = initialGroup;
      eventsCursor = null;
      prevCursors = [];
    }
  });

  $effect(() => {
    if (initialTimestamp) {
      highlightTimestamp = initialTimestamp;
    }
  });

  $effect(() => {
    if (selectedGroup) {
      loadEvents();
    }
  });

  $effect(() => {
    if (autoRefresh && selectedGroup) {
      autoRefreshTimer = setInterval(() => {
        loadEvents();
      }, 3000);
    }
    return () => {
      if (autoRefreshTimer) {
        clearInterval(autoRefreshTimer);
        autoRefreshTimer = null;
      }
    };
  });

  // Auto-scroll to highlighted event after events load
  $effect(() => {
    if (highlightTimestamp && events.length > 0) {
      // Wait for DOM to render
      requestAnimationFrame(() => {
        const el = document.getElementById(`log-event-${highlightTimestamp}`);
        if (el) {
          el.scrollIntoView({ behavior: "smooth", block: "center" });
          // Find and select the event
          const match = events.find((e) => e.timestamp === highlightTimestamp);
          if (match) selectedEvent = match;
        }
      });
    }
  });

  // ── Data fetching ────────────────────────────────────────────────────
  async function loadGroups() {
    groupsLoading = true;
    groupsError = "";
    try {
      groups = await fetchLogGroups();
    } catch (err) {
      groupsError =
        err instanceof Error ? err.message : "Failed to load log groups";
    } finally {
      groupsLoading = false;
    }
  }

  async function loadEvents() {
    if (!selectedGroup) return;
    eventsLoading = true;
    eventsError = "";
    try {
      const params: FetchLogEventsParams = {
        limit: eventsLimit,
        order: sortOrder,
      };
      if (eventsCursor) params.cursor = eventsCursor;
      if (filterLevel) params.level = filterLevel;
      if (filterPattern) params.pattern = filterPattern;
      if (filterStream) params.stream = filterStream;

      let result;
      if (isAllGroup) {
        result = await fetchAllLogEvents(params);
      } else {
        result = await fetchLogEvents(selectedGroup, params);
      }
      events = result.events ?? [];
      eventsTotal = result.total ?? 0;
      nextCursor = result.nextCursor || null;
    } catch (err) {
      eventsError =
        err instanceof Error ? err.message : "Failed to load log events";
    } finally {
      eventsLoading = false;
    }
  }

  // ── Helpers ──────────────────────────────────────────────────────────
  function selectGroup(name: string) {
    selectedGroup = name;
    eventsCursor = null;
    prevCursors = [];
    selectedEvent = null;
    highlightTimestamp = "";
    if (name === ALL_GROUP) {
      window.location.hash = "logs?group=__all__";
    } else {
      window.location.hash = `logs?group=${encodeURIComponent(name)}`;
    }
  }

  function backToGroups() {
    selectedGroup = "";
    events = [];
    eventsTotal = 0;
    eventsCursor = null;
    prevCursors = [];
    nextCursor = null;
    autoRefresh = false;
    selectedEvent = null;
    highlightTimestamp = "";
    window.location.hash = "logs";
  }

  function applyFilters() {
    eventsCursor = null;
    prevCursors = [];
    nextCursor = null;
    selectedEvent = null;
    loadEvents();
  }

  function clearFilters() {
    filterLevel = "";
    filterPattern = "";
    filterStream = "";
    eventsCursor = null;
    prevCursors = [];
    nextCursor = null;
    selectedEvent = null;
    loadEvents();
  }

  function toggleSort() {
    sortOrder = sortOrder === "desc" ? "asc" : "desc";
    eventsCursor = null;
    prevCursors = [];
    nextCursor = null;
    selectedEvent = null;
    loadEvents();
  }

  async function handleClearLogs() {
    if (!selectedGroup || isAllGroup) return;
    clearing = true;
    try {
      await clearLogGroup(selectedGroup);
      events = [];
      eventsTotal = 0;
      selectedEvent = null;
      eventsCursor = null;
      prevCursors = [];
      nextCursor = null;
      // Refresh group list to update counts
      loadGroups();
    } catch (err) {
      eventsError = err instanceof Error ? err.message : "Failed to clear logs";
    } finally {
      clearing = false;
    }
  }

  async function handleCopyStream() {
    if (!filterStream || !selectedGroup) return;
    copyingStream = true;
    try {
      // Fetch the entire stream (up to 10k events) from the API
      const params: FetchLogEventsParams = {
        limit: 10000,
        stream: filterStream,
        order: sortOrder,
      };
      const result = isAllGroup
        ? await fetchAllLogEvents(params)
        : await fetchLogEvents(selectedGroup, params);
      const allEvents = result.events ?? [];
      const text = allEvents
        .map((e) => {
          const ts = new Date(e.timestamp).toISOString();
          return `${ts} [${e.level}] ${e.message}`;
        })
        .join("\n");
      await navigator.clipboard.writeText(text);
      setTimeout(() => {
        copyingStream = false;
      }, 2000);
    } catch {
      copyingStream = false;
    }
  }

  let nextCursor: string | null = null;
  function nextPage() {
    if (nextCursor) {
      prevCursors = [...prevCursors, eventsCursor || ""];
      eventsCursor = nextCursor;
      selectedEvent = null;
      loadEvents();
    }
  }

  function prevPage() {
    if (prevCursors.length > 0) {
      eventsCursor = prevCursors[prevCursors.length - 1] || null;
      prevCursors = prevCursors.slice(0, -1);
    } else {
      eventsCursor = null;
    }
    selectedEvent = null;
    loadEvents();
  }

  function selectEvent(event: LogEvent) {
    if (selectedEvent === event) {
      selectedEvent = null;
    } else {
      selectedEvent = event;
    }
  }

  function formatDetailTimestamp(ts: string): string {
    try {
      return new Date(ts)
        .toISOString()
        .replace("T", "  ")
        .replace("Z", "  UTC");
    } catch {
      return ts;
    }
  }

  function formatCompactTime(ts: string): string {
    try {
      const d = new Date(ts);
      const h = String(d.getHours()).padStart(2, "0");
      const m = String(d.getMinutes()).padStart(2, "0");
      const s = String(d.getSeconds()).padStart(2, "0");
      const ms = String(d.getMilliseconds()).padStart(3, "0");
      return `${h}:${m}:${s}.${ms}`;
    } catch {
      return ts;
    }
  }

  // ── Log message analysis ─────────────────────────────────────────────

  interface ParsedSpringBootLog {
    pid: string;
    thread: string;
    logger: string;
    message: string;
  }

  /** Parse Spring Boot/Log4j format: LEVEL PID --- [THREAD] LOGGER : MESSAGE */
  function parseSpringBootLog(raw: string): ParsedSpringBootLog | null {
    const m = raw.match(
      /^\w+\s+(\d+)\s+---\s+\[([^\]]+)\]\s+([\w.$]+)\s*:\s([\s\S]+)$/,
    );
    if (!m) return null;
    return { pid: m[1], thread: m[2].trim(), logger: m[3], message: m[4] };
  }

  function hasJavaToString(str: string): boolean {
    return /[A-Z][a-zA-Z0-9]*\([a-z]/.test(str);
  }

  function looksLikeJSON(str: string): boolean {
    const t = str.trim();
    return t.startsWith("{") || t.startsWith("[") || t.startsWith('\\{') || t.startsWith('\\"');
  }

  function isComplexMessage(msg: string): boolean {
    return msg.length > 300 || hasJavaToString(msg) || looksLikeJSON(msg);
  }

  /**
   * Format Java toString output (Lombok/etc) into an indented, readable tree.
   * Converts `ClassName(key=value, ...)` and `{key=value}` map literals.
   */
  function formatJavaToString(str: string): string {
    let indent = 0;
    let result = "";
    for (let i = 0; i < str.length; i++) {
      const ch = str[i];
      const next = i + 1 < str.length ? str[i + 1] : "";
      if ((ch === "(" && next === ")") || (ch === "{" && next === "}")) {
        result += ch + next;
        i++; // skip the paired closing char
      } else if (ch === "(" || ch === "{") {
        result += ch + "\n";
        indent++;
        result += "  ".repeat(indent);
      } else if (ch === ")" || ch === "}") {
        indent = Math.max(0, indent - 1);
        result += "\n" + "  ".repeat(indent) + ch;
      } else if (ch === "," && next === " ") {
        result += ",\n" + "  ".repeat(indent);
        i++; // skip the trailing space
      } else if (ch === "=") {
        result += ": ";
      } else {
        result += ch;
      }
    }
    return result.trim();
  }

  /** Recursively unescape JSON string values that are themselves JSON. */
  function deepUnescapeJSON(val: unknown): unknown {
    if (typeof val === "string") {
      const t = val.trim();
      if (t.startsWith("{") || t.startsWith("[")) {
        try {
          return deepUnescapeJSON(JSON.parse(t));
        } catch {
          return val;
        }
      }
      return val;
    }
    if (Array.isArray(val)) return val.map(deepUnescapeJSON);
    if (val && typeof val === "object") {
      const out: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(val)) out[k] = deepUnescapeJSON(v);
      return out;
    }
    return val;
  }

  function computeFormattedMessage(content: string): string | null {
    // JSON first — with recursive unescape of stringified nested JSON
    try {
      const parsed = JSON.parse(content);
      return JSON.stringify(deepUnescapeJSON(parsed), null, 2);
    } catch {
      /* not JSON */
    }
    // Try unescaping backslash-quoted JSON (common in log output: {\"key\":\"value\"})
    if (content.includes('\\"')) {
      const cleaned = content.replace(/\\"/g, '"');
      try {
        const parsed = JSON.parse(cleaned);
        return JSON.stringify(deepUnescapeJSON(parsed), null, 2);
      } catch {
        /* still not JSON */
      }
    }
    // Java toString
    if (hasJavaToString(content)) {
      return formatJavaToString(content);
    }
    return null;
  }

  // ── Inline JSON formatting for compact view ─────────────────────────
  function tryFormatInlineJSON(msg: string): {
    isJSON: boolean;
    formatted: string;
  } {
    const trimmed = msg.trim();
    if (trimmed.startsWith("{") || trimmed.startsWith("[")) {
      try {
        const parsed = JSON.parse(trimmed);
        const unescaped = deepUnescapeJSON(parsed);
        return { isJSON: true, formatted: JSON.stringify(unescaped, null, 2) };
      } catch {
        /* not JSON */
      }
    }
    // Try unescaping backslash-quoted JSON
    if (trimmed.includes('\\"')) {
      const cleaned = trimmed.replace(/\\"/g, '"');
      try {
        const parsed = JSON.parse(cleaned);
        const unescaped = deepUnescapeJSON(parsed);
        return { isJSON: true, formatted: JSON.stringify(unescaped, null, 2) };
      } catch {
        /* still not JSON */
      }
    }
    return { isJSON: false, formatted: msg };
  }

  // ── Syntax highlighting ───────────────────────────────────────────────

  function escapeHTML(s: string): string {
    return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }

  const H = {
    key: "color:var(--lh-key)",
    cls: "color:var(--lh-cls)",
    null_: "color:var(--lh-null);font-style:italic",
    bool: "color:var(--lh-bool)",
    num: "color:var(--lh-num)",
    str: "color:var(--lh-str)",
    punct: "color:var(--lh-punct)",
  };

  function jtsValue(rawVal: string): string {
    const hasComma = rawVal.endsWith(",");
    const v = hasComma ? rawVal.slice(0, -1) : rawVal;
    const comma = hasComma ? `<span style="${H.punct}">,</span>` : "";
    if (v === "null") return `<span style="${H.null_}">null</span>` + comma;
    if (v === "true" || v === "false")
      return `<span style="${H.bool}">${escapeHTML(v)}</span>` + comma;
    if (/^-?\d+(\.\d+)?([Ee][+-]?\d+)?[fFdDlL]?$/.test(v))
      return `<span style="${H.num}">${escapeHTML(v)}</span>` + comma;
    const cm = v.match(/^([A-Z][a-zA-Z0-9$_]*)([({].*)$/);
    if (cm)
      return (
        `<span style="${H.cls}">${escapeHTML(cm[1])}</span><span style="${H.punct}">${escapeHTML(cm[2])}</span>` +
        comma
      );
    if (v === "{" || v === "[")
      return `<span style="${H.punct}">${escapeHTML(v)}</span>` + comma;
    return `<span style="${H.str}">${escapeHTML(v)}</span>` + comma;
  }

  function highlightJavaToString(text: string): string {
    return text
      .split("\n")
      .map((line) => {
        const m = line.match(/^(\s*)([\s\S]*)$/);
        const indent = m?.[1] ?? "";
        const content = m?.[2] ?? "";
        if (!content) return escapeHTML(indent);
        // Closing bracket lines
        if (/^[)\}]+,?$/.test(content)) {
          return (
            escapeHTML(indent) +
            `<span style="${H.punct}">${escapeHTML(content)}</span>`
          );
        }
        // key: value
        const kv = content.match(/^([a-z_$][a-zA-Z0-9_$]*)(: )([\s\S]*)$/);
        if (kv) {
          return (
            escapeHTML(indent) +
            `<span style="${H.key}">${escapeHTML(kv[1])}</span>` +
            `<span style="${H.punct}">: </span>` +
            jtsValue(kv[3])
          );
        }
        // ClassName( or ClassName{
        const cls = content.match(/^([A-Z][a-zA-Z0-9$_]*)([({,]?.*)$/);
        if (cls) {
          return (
            escapeHTML(indent) +
            `<span style="${H.cls}">${escapeHTML(cls[1])}</span>` +
            (cls[2]
              ? `<span style="${H.punct}">${escapeHTML(cls[2])}</span>`
              : "")
          );
        }
        if (content === "{" || content === "[") {
          return (
            escapeHTML(indent) +
            `<span style="${H.punct}">${escapeHTML(content)}</span>`
          );
        }
        return escapeHTML(indent) + escapeHTML(content);
      })
      .join("\n");
  }

  function highlightFormatted(text: string): string {
    return looksLikeJSON(text)
      ? highlightJSON(text)
      : highlightJavaToString(text);
  }

  const parsedSpringBootLog = $derived(
    selectedEvent ? parseSpringBootLog(selectedEvent.message) : null,
  );
  const panelDisplayMessage = $derived(
    selectedEvent
      ? parsedSpringBootLog
        ? parsedSpringBootLog.message
        : selectedEvent.message
      : "",
  );
  const panelFormattedMessage = $derived(
    selectedEvent && isComplexMessage(selectedEvent.message)
      ? computeFormattedMessage(panelDisplayMessage)
      : null,
  );
  const panelIsComplex = $derived(
    selectedEvent ? isComplexMessage(selectedEvent.message) : false,
  );
  const panelHighlightedHtml = $derived(
    panelFormattedMessage !== null
      ? highlightFormatted(panelFormattedMessage)
      : null,
  );

  function levelColor(
    level: string,
  ): "default" | "destructive" | "amber" | "secondary" | "outline" {
    switch (level) {
      case "ERROR":
        return "destructive";
      case "WARN":
        return "amber";
      case "DEBUG":
        return "secondary";
      case "INFO":
        return "default";
      default:
        return "outline";
    }
  }

  function levelStripColor(level: string): string {
    switch (level) {
      case "ERROR":
        return "bg-red-500/70";
      case "WARN":
        return "bg-amber-400/70";
      case "DEBUG":
        return "bg-text-faint/40";
      case "INFO":
        return "bg-accent/60";
      default:
        return "bg-border";
    }
  }

  function levelTextColor(level: string): string {
    switch (level) {
      case "ERROR":
        return "text-red-400";
      case "WARN":
        return "text-amber-400";
      case "DEBUG":
        return "text-muted-foreground/50";
      case "INFO":
        return "text-primary/70";
      default:
        return "text-muted-foreground";
    }
  }

  function groupDisplayName(name: string): string {
    if (name.startsWith("/aws/lambda/"))
      return name.slice("/aws/lambda/".length);
    if (name.startsWith("/tarn/")) return name.slice(1);
    return name;
  }

  function groupCategory(name: string): string {
    return serviceLabel(groupServiceKey(name));
  }

  function groupServiceKey(name: string): string {
    if (name.startsWith("/aws/lambda/")) return "lambda";
    if (name === "/tarn/api") return "api";
    if (name === "/tarn/system") return "system";
    if (name.startsWith("/tarn/apigateway")) return "apigatewayv2";
    if (name.startsWith("/tarn/sns")) return "sns";
    if (name.startsWith("/tarn/sqs")) return "sqs";
    if (name.startsWith("/tarn/secrets")) return "secretsmanager";
    if (name.startsWith("/tarn/")) return "system";
    return "other";
  }

  function serviceLabel(key: string): string {
    switch (key) {
      case "all":
        return "All";
      case "lambda":
        return "Lambda";
      case "api":
        return "API";
      case "system":
        return "System";
      case "apigatewayv2":
        return "API Gateway";
      case "sns":
        return "SNS";
      case "sqs":
        return "SQS";
      case "secretsmanager":
        return "Secrets";
      default:
        return "Other";
    }
  }

  function isLambdaGroup(name: string): boolean {
    return name.startsWith("/aws/lambda/");
  }

  function isLambdaOutputEvent(event: LogEvent): boolean {
    return isLambdaGroup(selectedGroup) && event.source === "output";
  }

  function truncateMessage(msg: string, maxLen: number = 200): string {
    if (msg.length <= maxLen) return msg;
    return msg.slice(0, maxLen) + "...";
  }

  const selectedGroupSummary = $derived(
    groups.find((group) => group.name === selectedGroup) ?? null,
  );
  const latestKnownEventTimestamp = $derived(
    events.length > 0
      ? events[0].timestamp
      : (selectedGroupSummary?.lastEvent ?? ""),
  );
  const selectedGroupIsActive = $derived(
    (() => {
      if (!latestKnownEventTimestamp) return false;
      const ts = new Date(latestKnownEventTimestamp).getTime();
      if (Number.isNaN(ts)) return false;
      return Date.now() - ts < 15000;
    })(),
  );
  const showLiveLoadingSkeleton = $derived(
    autoRefresh &&
      eventsLoading &&
      events.length > 0 &&
      selectedGroupIsActive,
  );
  const hasNextPage = $derived(!!nextCursor);
  const hasPrevPage = $derived(prevCursors.length > 0 || !!eventsCursor);
  const selectedGroupIsLambda = $derived(isLambdaGroup(selectedGroup));
  const pageInfo = $derived(
    eventsTotal > 0 && events.length > 0
      ? `${events.length} of ${eventsTotal} events`
      : "No events",
  );
  const hasPagination = $derived(eventsTotal > eventsLimit);
  const totalEventCount = $derived(
    groups.reduce((sum, g) => sum + g.eventCount, 0),
  );
  const serviceOptions = $derived(
    (() => {
      const counts = new Map<string, number>();
      for (const group of groups) {
        const key = groupServiceKey(group.name);
        counts.set(key, (counts.get(key) ?? 0) + 1);
      }

      const orderedKeys = [
        "lambda",
        "api",
        "system",
        "apigatewayv2",
        "sns",
        "sqs",
        "secretsmanager",
        "other",
      ];
      const options = [
        { key: "all", label: serviceLabel("all"), count: groups.length },
      ];
      for (const key of orderedKeys) {
        const count = counts.get(key) ?? 0;
        if (count > 0) {
          options.push({ key, label: serviceLabel(key), count });
        }
      }
      return options;
    })(),
  );
  const filteredGroups = $derived(
    groups.filter((group) => {
      if (
        serviceFilter !== "all" &&
        groupServiceKey(group.name) !== serviceFilter
      ) {
        return false;
      }

      const query = groupSearch.trim().toLowerCase();
      if (!query) {
        return true;
      }

      return (
        group.name.toLowerCase().includes(query) ||
        groupDisplayName(group.name).toLowerCase().includes(query)
      );
    }),
  );
  const groupsCountLabel = $derived(
    filteredGroups.length === groups.length
      ? `${groups.length} groups`
      : `${filteredGroups.length} of ${groups.length} groups`,
  );
</script>

<svelte:window
  onkeydown={(e) => {
    if (e.key === "Escape" && selectedEvent) selectedEvent = null;
  }}
/>

{#if selectedGroup}
  <!-- ── Event viewer ─────────────────────────────────────────────── -->
  <div class="space-y-3">
    <!-- Header -->
    <SectionHeader
      title={isAllGroup ? "All log groups" : selectedGroup}
      description={pageInfo}
      icon={ScrollIcon}
      {sidebarCollapsed}
      {onToggleSidebar}
    >
      {#snippet lead()}
        <button
          type="button"
          onclick={backToGroups}
          class="flex items-center justify-center h-7 w-7 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors shrink-0"
          aria-label="Back to log groups"
        >
          <ArrowLeftIcon size={16} />
        </button>
      {/snippet}

      {#snippet actions()}
        <div class="flex items-center gap-2 shrink-0">
        <!-- Sort toggle -->
        <button
          type="button"
          onclick={toggleSort}
          class="inline-flex items-center gap-1.5 rounded-full border border-border px-2.5 py-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
          title={sortOrder === "desc"
            ? "Showing newest first"
            : "Showing oldest first"}
        >
          {#if sortOrder === "desc"}
            <SortDescendingIcon size={12} />
            Newest
          {:else}
            <SortAscendingIcon size={12} />
            Oldest
          {/if}
        </button>
        <button
          type="button"
          onclick={() => (autoRefresh = !autoRefresh)}
          class="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs transition-colors {autoRefresh
            ? 'border-primary/50 bg-primary/10 text-primary'
            : 'border-border text-muted-foreground hover:text-foreground'}"
        >
          <ArrowsClockwiseIcon
            size={12}
            class={autoRefresh ? "animate-spin" : ""}
          />
          {autoRefresh ? "Live" : "Auto"}
        </button>
        <button
          type="button"
          onclick={() => (showFilters = !showFilters)}
          class="inline-flex items-center gap-1.5 rounded-full border border-border px-2.5 py-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          <FunnelIcon size={12} />
          Filters
          <CaretDownIcon
            size={10}
            class="transition-transform {showFilters ? 'rotate-180' : ''}"
          />
        </button>
        {#if !isAllGroup}
          <button
            type="button"
            onclick={handleClearLogs}
            disabled={clearing || events.length === 0}
            class="inline-flex items-center gap-1.5 rounded-md border border-red/30 bg-destructive/10 px-2.5 py-1 text-xs text-destructive hover:bg-red/14 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
            title="Clear all logs in this group"
          >
            <TrashIcon size={12} />
            Clear
          </button>
        {/if}
        <button
          type="button"
          onclick={loadEvents}
          disabled={eventsLoading}
          class="inline-flex items-center gap-1.5 rounded-md border border-primary/50 bg-primary/10 px-2.5 py-1 text-xs text-primary hover:bg-primary/20 transition-colors disabled:opacity-50"
        >
          <ArrowsClockwiseIcon
            size={12}
            class={eventsLoading ? "animate-spin" : ""}
          />
          Refresh
        </button>
        </div>
      {/snippet}
    </SectionHeader>

    <!-- Filters panel -->
    {#if showFilters}
      <div class="rounded-lg border border-border bg-card px-4 py-3">
        <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
          <div class="space-y-1">
            <label
              class="text-[10px] font-medium uppercase tracking-wider text-muted-foreground/70"
              for="log-level">Level</label
            >
            <select
              id="log-level"
              bind:value={filterLevel}
              class="w-full rounded-md border border-border bg-muted px-2 py-1.5 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary"
            >
              <option value="">All levels</option>
              <option value="DEBUG">DEBUG</option>
              <option value="INFO">INFO</option>
              <option value="WARN">WARN</option>
              <option value="ERROR">ERROR</option>
            </select>
          </div>
          <div class="space-y-1">
            <label
              class="text-[10px] font-medium uppercase tracking-wider text-muted-foreground/70"
              for="log-pattern">Search pattern</label
            >
            <div class="relative">
              <MagnifyingGlassIcon
                size={12}
                class="absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground/70"
              />
              <input
                id="log-pattern"
                type="text"
                placeholder="Filter messages..."
                bind:value={filterPattern}
                onkeydown={(e) => {
                  if (e.key === "Enter") applyFilters();
                }}
                class="w-full rounded-md border border-border bg-muted pl-7 pr-2 py-1.5 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary placeholder:text-muted-foreground/70"
              />
            </div>
          </div>
          <div class="space-y-1">
            <label
              class="text-[10px] font-medium uppercase tracking-wider text-muted-foreground/70"
              for="log-stream">Stream</label
            >
            <input
              id="log-stream"
              type="text"
              placeholder="Stream name..."
              bind:value={filterStream}
              onkeydown={(e) => {
                if (e.key === "Enter") applyFilters();
              }}
              class="w-full rounded-md border border-border bg-muted px-2 py-1.5 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary placeholder:text-muted-foreground/70"
            />
          </div>
          <div class="flex items-end gap-2">
            <button
              type="button"
              onclick={applyFilters}
              class="rounded-md border border-primary/50 bg-primary/10 px-3 py-1.5 text-xs text-primary hover:bg-primary/20 transition-colors"
            >
              Apply
            </button>
            <button
              type="button"
              onclick={clearFilters}
              class="rounded-md border border-border px-3 py-1.5 text-xs text-muted-foreground hover:bg-muted transition-colors"
            >
              Clear
            </button>
            <button
              type="button"
              onclick={handleCopyStream}
              disabled={!filterStream}
              class="inline-flex items-center gap-1 rounded-md border border-border px-3 py-1.5 text-xs text-muted-foreground hover:bg-muted transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
              title={filterStream
                ? "Copy entire stream output to clipboard"
                : "Filter by a stream name first"}
            >
              <ClipboardTextIcon size={12} />
              {copyingStream ? "Copied!" : "Copy entire stream"}
            </button>
          </div>
        </div>
      </div>
    {/if}

    <!-- Events list -->
    {#if eventsError}
      <div
        class="rounded-lg border border-red/20 bg-red-muted px-4 py-3 text-xs text-destructive"
      >
        {eventsError}
      </div>
    {:else if eventsLoading && events.length === 0}
      <div class="rounded-lg border border-border overflow-hidden">
        <div class="p-3 space-y-1">
          {#each Array(12) as _, i (i)}
            <Skeleton class="h-5 w-full" />
          {/each}
        </div>
      </div>
    {:else if events.length === 0}
      <EmptyState
        message="No log events found. Try adjusting your filters or invoke a function."
        icon={ScrollIcon}
      />
    {:else}
      <!-- Two-panel layout: event list + detail panel -->
      <div
        class="rounded-lg border border-border overflow-hidden flex flex-1"
        style={`min-height: 0; height: calc(100vh - ${hasPagination ? "12.75rem" : "10rem"})`}
      >
        <!-- Event rows — compact view -->
        <div class="flex-1 min-w-0 overflow-y-auto font-mono text-[12px] leading-tight">
          {#if showLiveLoadingSkeleton}
            <div class="sticky top-0 z-10 border-b border-border bg-card/95 backdrop-blur px-3 py-2">
              <div class="mb-1.5 flex items-center gap-2 text-[10px] uppercase tracking-wider text-muted-foreground/70">
                <ArrowsClockwiseIcon size={11} class="animate-spin" />
                Loading latest logs
              </div>
              <div class="space-y-1.5">
                {#each Array(3) as _, i (i)}
                  <div class="flex items-center gap-2">
                    <Skeleton class="h-3 w-24 shrink-0" />
                    <Skeleton class="h-3 w-10 shrink-0" />
                    <Skeleton class="h-3 w-full" />
                  </div>
                {/each}
              </div>
            </div>
          {/if}
          {#each events as event, i (event.timestamp + "-" + i + "-" + event.streamName)}
            {@const isSelected = selectedEvent === event}
            {@const isHighlighted =
              highlightTimestamp && event.timestamp === highlightTimestamp}
            <div
              id="log-event-{event.timestamp}"
              role="button"
              tabindex="0"
              onclick={() => selectEvent(event)}
              onkeydown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  selectEvent(event);
                }
              }}
              class="group relative flex items-start cursor-pointer transition-colors duration-75 border-b border-border/30 {isHighlighted
                ? 'bg-amber-500/10 border-l-2 border-l-amber-400'
                : isSelected
                  ? 'bg-muted'
                  : 'hover:bg-muted/50'}"
            >
              <!-- Level strip -->
              <div
                class="w-0.5 self-stretch shrink-0 {levelStripColor(
                  event.level,
                )} {isSelected || isHighlighted
                  ? 'opacity-100'
                  : 'opacity-30 group-hover:opacity-60'} transition-opacity"
              ></div>

              <div
                class="flex items-baseline gap-2 px-2 py-[3px] flex-1 min-w-0"
              >
                <!-- Compact timestamp -->
                <span
                  class="text-[11px] text-muted-foreground/50 whitespace-nowrap shrink-0 tabular-nums select-none"
                  >{formatCompactTime(event.timestamp)}</span
                >

                <!-- Level — compact text instead of badge -->
                <span
                  class="text-[10px] font-semibold uppercase shrink-0 w-[38px] {levelTextColor(
                    event.level,
                  )} select-none">{event.level}</span
                >

                <!-- Source group tag (for "All" view) -->
                {#if isAllGroup}
                  <span
                    class="text-[10px] text-primary/60 whitespace-nowrap shrink-0 max-w-[120px] truncate"
                    title={event.streamName}
                  >
                    {event.streamName.split("/").slice(1, -1).join("/")}
                  </span>
                {/if}

                <!-- Message — single line, truncated -->
                <span
                  class="truncate flex-1 min-w-0 {isSelected
                    ? 'text-foreground'
                    : event.level === 'ERROR'
                      ? 'text-red-300/80'
                      : 'text-foreground/80'}"
                  title={event.message}
                >
                  {truncateMessage(event.message.replace(/\n/g, " "))}
                </span>

                <!-- Stream name (compact) -->
                {#if event.streamName && !selectedEvent && !isAllGroup}
                  <span
                    class="text-[10px] text-muted-foreground/40 whitespace-nowrap shrink-0 hidden lg:inline"
                    title={event.streamName}
                  >
                    {event.streamName.length > 20
                      ? event.streamName.slice(-20)
                      : event.streamName}
                  </span>
                {/if}
              </div>
            </div>
          {/each}
        </div>

        <!-- Detail panel -->
        <div
          class="shrink-0 overflow-hidden border-l border-border/70 bg-background/60 transition-[width,opacity] duration-200 ease-out {selectedEvent
            ? 'opacity-100'
            : 'opacity-0'}"
          style="width: {selectedEvent ? '420px' : '0px'}"
        >
          {#if selectedEvent}
            {@const ev = selectedEvent}
            <div class="flex flex-col h-full min-w-[420px]">
              <!-- Panel header -->
              <div
                class="flex shrink-0 items-center justify-between gap-3 border-b border-border/70 bg-background/35 px-4 py-2.5"
              >
                <div class="flex items-center gap-2 min-w-0">
                  <Badge variant={levelColor(ev.level)} class="shrink-0"
                    >{ev.level}</Badge
                  >
                  <span
                    class="text-[11px] text-muted-foreground/70 font-mono tabular-nums truncate"
                  >
                    {formatCompactTime(ev.timestamp)}
                  </span>
                </div>
                <button
                  type="button"
                  onclick={() => (selectedEvent = null)}
                  class="flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground/70 transition-colors hover:bg-background-subtle hover:text-foreground"
                  aria-label="Close detail panel"
                >
                  <XIcon size={14} />
                </button>
              </div>

              <!-- Panel body -->
              <div class="flex-1 overflow-y-auto p-4 space-y-4">
                <!-- Message section -->
                <div>
                  <div class="mb-2">
                    <p
                      class="text-[10px] font-medium uppercase tracking-widest text-muted-foreground/70"
                    >
                      Message
                    </p>
                  </div>

                  {#if panelIsComplex}
                    <FormattedMessageViewer
                      raw={ev.message}
                      formatted={panelHighlightedHtml === null
                        ? panelDisplayMessage
                        : panelFormattedMessage}
                      formattedHtml={panelHighlightedHtml}
                      variant="tabs"
                      formattedContentClass="text-[12px] text-foreground"
                      rawContentClass="text-[11px] text-muted-foreground"
                      formattedMaxHeightClass="max-h-[55vh]"
                      rawMaxHeightClass="max-h-[40vh]"
                    />
                  {:else if looksLikeJSON(ev.message)}
                    {@const jsonResult = tryFormatInlineJSON(ev.message)}
                    {#if jsonResult.isJSON}
                      <pre
                        class="max-h-[55vh] overflow-y-auto rounded-md border border-border/70 bg-[var(--code-bg)] px-3 py-3 text-[12px] font-mono leading-relaxed whitespace-pre-wrap break-all text-foreground">{@html highlightJSON(
                          jsonResult.formatted,
                        )}</pre>
                    {:else}
                      <pre
                        class="rounded-md border border-border/70 bg-[var(--code-bg)] px-3 py-3 text-[13px] font-mono leading-relaxed whitespace-pre-wrap break-all text-foreground">{ev.message}</pre>
                    {/if}
                  {:else}
                    <pre
                      class="rounded-md border border-border/70 bg-[var(--code-bg)] px-3 py-3 text-[13px] font-mono leading-relaxed whitespace-pre-wrap break-all text-foreground">{ev.message}</pre>
                  {/if}
                </div>

                <!-- Metadata table -->
                <div>
                  <p
                    class="text-[10px] font-medium uppercase tracking-widest text-muted-foreground/70 mb-2"
                  >
                    Details
                  </p>
                  <div
                    class="overflow-hidden rounded-md border border-border/70 divide-y divide-border/60"
                  >
                    <div class="flex items-start gap-4 px-3 py-2">
                      <span
                        class="text-[10px] uppercase tracking-wider text-muted-foreground/70 w-20 shrink-0 pt-px"
                        >Time</span
                      >
                      <span
                        class="text-[12px] font-mono text-muted-foreground break-all leading-snug"
                        >{formatDetailTimestamp(ev.timestamp)}</span
                      >
                    </div>
                    <div class="flex items-center gap-4 px-3 py-2">
                      <span
                        class="text-[10px] uppercase tracking-wider text-muted-foreground/70 w-20 shrink-0"
                        >Level</span
                      >
                      <Badge variant={levelColor(ev.level)} class="text-[10px]"
                        >{ev.level}</Badge
                      >
                    </div>
                    {#if parsedSpringBootLog}
                      <div class="flex items-start gap-4 px-3 py-2">
                        <span
                          class="text-[10px] uppercase tracking-wider text-muted-foreground/70 w-20 shrink-0 pt-px"
                          >Thread</span
                        >
                        <span
                          class="text-[12px] font-mono text-muted-foreground break-all leading-snug"
                          >{parsedSpringBootLog.thread}</span
                        >
                      </div>
                      <div class="flex items-start gap-4 px-3 py-2">
                        <span
                          class="text-[10px] uppercase tracking-wider text-muted-foreground/70 w-20 shrink-0 pt-px"
                          >Logger</span
                        >
                        <span
                          class="text-[12px] font-mono text-muted-foreground break-all leading-snug"
                          >{parsedSpringBootLog.logger}</span
                        >
                      </div>
                      <div class="flex items-center gap-4 px-3 py-2">
                        <span
                          class="text-[10px] uppercase tracking-wider text-muted-foreground/70 w-20 shrink-0"
                          >PID</span
                        >
                        <span
                          class="text-[12px] font-mono text-muted-foreground"
                          >{parsedSpringBootLog.pid}</span
                        >
                      </div>
                    {/if}
                    {#if ev.streamName}
                      <div class="flex items-start gap-4 px-3 py-2">
                        <span
                          class="text-[10px] uppercase tracking-wider text-muted-foreground/70 w-20 shrink-0 pt-px"
                          >Stream</span
                        >
                        <span
                          class="text-[12px] font-mono text-muted-foreground break-all leading-snug"
                          >{ev.streamName}</span
                        >
                      </div>
                    {/if}
                    {#if ev.source}
                      <div class="flex items-center gap-4 px-3 py-2">
                        <span
                          class="text-[10px] uppercase tracking-wider text-muted-foreground/70 w-20 shrink-0"
                          >Source</span
                        >
                        <span
                          class="text-[12px] font-mono text-muted-foreground"
                          >{ev.source}</span
                        >
                      </div>
                    {/if}
                  </div>
                </div>
              </div>
            </div>
          {/if}
        </div>
      </div>

      <!-- Pagination -->
      {#if hasPagination}
        <div class="flex items-center justify-between px-1 pt-3">
          <p class="text-xs text-muted-foreground/70">{pageInfo}</p>
          <div class="flex items-center gap-2">
            <button
              type="button"
              onclick={prevPage}
              disabled={!hasPrevPage}
              class="rounded-md border border-border px-2.5 py-1 text-xs text-muted-foreground hover:bg-muted transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
            >
              Previous
            </button>
            <button
              type="button"
              onclick={nextPage}
              disabled={!hasNextPage}
              class="rounded-md border border-border px-2.5 py-1 text-xs text-muted-foreground hover:bg-muted transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
            >
              Next
            </button>
          </div>
        </div>
      {/if}
    {/if}
  </div>
{:else}
  <!-- ── Groups list ──────────────────────────────────────────────── -->
  <div class="space-y-3">
    <!-- Header -->
    <SectionHeader
      title="Log groups"
      description={groupsCountLabel}
      icon={ScrollIcon}
      {sidebarCollapsed}
      {onToggleSidebar}
    >
      {#snippet actions()}
        <button
          type="button"
          onclick={loadGroups}
          disabled={groupsLoading}
          class="inline-flex items-center gap-1.5 rounded-md border border-primary/50 bg-primary/10 px-2.5 py-1 text-xs text-primary hover:bg-primary/20 transition-colors disabled:opacity-50"
        >
          <ArrowsClockwiseIcon
            size={12}
            class={groupsLoading ? "animate-spin" : ""}
          />
          Refresh
        </button>
      {/snippet}
    </SectionHeader>

    <div class="space-y-3 mt-1">
      <div class="relative">
        <MagnifyingGlassIcon
          size={12}
          class="absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground/70"
        />
        <input
          type="text"
          placeholder="Search services or log groups..."
          bind:value={groupSearch}
          class="w-full rounded border border-border bg-background pl-7 pr-2.5 py-1.5 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary placeholder:text-muted-foreground/70"
        />
      </div>
      <div class="flex flex-wrap gap-2">
        {#each serviceOptions as option (option.key)}
          <button
            type="button"
            onclick={() => (serviceFilter = option.key)}
            class="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs transition-colors {serviceFilter ===
            option.key
              ? 'border-primary/50 bg-primary/10 text-primary'
              : 'border-border text-muted-foreground hover:text-foreground hover:bg-muted'}"
          >
            <span>{option.label}</span>
            <span
              class="rounded-full bg-black/5 px-1.5 py-0.5 text-[10px] tabular-nums dark:bg-white/10"
              >{option.count}</span
            >
          </button>
        {/each}
      </div>
    </div>

    <!-- Error state -->
    {#if groupsError}
      <div
        class="rounded-lg border border-red/20 bg-red-muted px-4 py-3 text-xs text-destructive"
      >
        {groupsError}
      </div>
    {/if}

    <!-- Loading state -->
    {#if groupsLoading}
      <div class="space-y-2">
        {#each Array(4) as _, i (i)}
          <Skeleton class="h-16 w-full rounded-lg" />
        {/each}
      </div>
    {:else if groups.length === 0}
      <EmptyState
        message="No log groups yet. Create a Lambda function or invoke one to see logs."
        icon={ScrollIcon}
      />
    {:else}
      <!-- "All" virtual group card -->
      <button
        type="button"
        onclick={() => selectGroup(ALL_GROUP)}
        class="group flex w-full items-center justify-between gap-4 rounded-lg border border-dashed border-primary/30 bg-primary/5 px-4 py-3 text-left transition-all duration-150 hover:-translate-y-px hover:border-primary/50 hover:bg-primary/10 hover:shadow-[0_8px_24px_-18px_var(--color-primary)]"
      >
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2 mb-1">
            <span class="text-[10px] font-mono text-primary">All Services</span>
            <span class="text-sm font-medium text-foreground">All Logs</span>
          </div>
          <p class="text-[10px] font-mono text-muted-foreground/70">
            Aggregated view across all log groups — Lambda, Secrets, API,
            System, etc.
          </p>
        </div>
        <div class="flex items-center gap-4 shrink-0 text-right">
          <div>
            <p class="text-sm font-semibold text-foreground tabular-nums">
              {totalEventCount}
            </p>
            <p class="text-[10px] text-muted-foreground/70">total events</p>
          </div>
          <div>
            <p class="text-sm font-semibold text-foreground tabular-nums">
              {groups.length}
            </p>
            <p class="text-[10px] text-muted-foreground/70">groups</p>
          </div>
        </div>
      </button>

      {#if filteredGroups.length === 0}
        <EmptyState
          message="No log groups match the current service or search filters."
          icon={ScrollIcon}
        />
      {:else}
        <div class="grid grid-cols-1 gap-2">
          {#each filteredGroups as group (group.name)}
            <button
              type="button"
              onclick={() => selectGroup(group.name)}
              class="group flex items-center justify-between gap-4 rounded-lg border border-border bg-card px-4 py-3 text-left transition-all duration-150 hover:-translate-y-px hover:border-primary/30 hover:bg-muted/60 hover:shadow-[0_12px_30px_-24px_rgba(0,0,0,0.45)]"
            >
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2 mb-1">
                  <span class="text-[10px] font-mono text-muted-foreground/70"
                    >{groupCategory(group.name)}</span
                  >
                  <span class="text-sm font-medium text-foreground truncate"
                    >{groupDisplayName(group.name)}</span
                  >
                </div>
                <p
                  class="text-[10px] font-mono text-muted-foreground/70 truncate"
                >
                  {group.name}
                </p>
              </div>
              <div class="flex items-center gap-4 shrink-0 text-right">
                <div>
                  <p class="text-sm font-semibold text-foreground tabular-nums">
                    {group.eventCount}
                  </p>
                  <p class="text-[10px] text-muted-foreground/70">events</p>
                </div>
                <div>
                  <p class="text-sm font-semibold text-foreground tabular-nums">
                    {group.streamCount}
                  </p>
                  <p class="text-[10px] text-muted-foreground/70">streams</p>
                </div>
                {#if group.lastEvent}
                  <div class="hidden sm:block">
                    <p class="text-xs text-muted-foreground tabular-nums">
                      {new Date(group.lastEvent).toLocaleTimeString()}
                    </p>
                    <p class="text-[10px] text-muted-foreground/70">
                      last event
                    </p>
                  </div>
                {/if}
              </div>
            </button>
          {/each}
        </div>
      {/if}
    {/if}
  </div>
{/if}
