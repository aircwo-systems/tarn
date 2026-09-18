<script lang="ts">
  import {
    ScrollIcon,
    MagnifyingGlassIcon,
    FunnelIcon,
    ArrowLeftIcon,
    ArrowsClockwiseIcon,
    XIcon,
    TrashIcon,
    SortAscendingIcon,
    SortDescendingIcon,
    ClipboardTextIcon,
    ArrowUpRightIcon,
    CaretRightIcon,
    CheckIcon,
    PlusIcon,
  } from "phosphor-svelte";
  import { fly } from "svelte/transition";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import EmptyState from "$lib/components/common/empty-state.svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import { untrack } from "svelte";
  import SectionHeader from "./section-header.svelte";
  import LogStream, { keyEvents } from "$lib/components/logs/log-stream.svelte";
  import { PaneGroup, Pane, Handle } from "$lib/components/ui/resizable";
  import {
    fetchLogGroups,
    fetchLogEvents,
    fetchAllLogEvents,
    scanLogs,
    clearLogGroup,
    fetchTraceForLog,
    type FetchLogEventsParams,
    type LogScanResult,
    type LogGroupScanResult,
  } from "$lib/api";
  import { highlightJSON } from "$lib/json-format";
  import {
    parseSpringBootLog,
    looksLikeJSON,
    isComplexMessage,
    computeFormattedMessage,
    tryFormatInlineJSON,
    highlightFormatted,
  } from "$lib/log-format";
  import {
    spanColor,
    spanKindLabel,
    formatMs,
    buildWaterfall,
    traceTitle,
    type WaterfallRow,
  } from "$lib/trace-utils";
  import type { LogGroupSummary, LogEvent, RequestTrace } from "$lib/types";
  import {
    logLocationWithFilters,
    type LogNavigationFilters,
    type LogSortOrder,
  } from "$lib/navigation-history";

  let {
    initialGroup = "",
    initialTimestamp = "",
    initialLevels = [],
    initialPattern = "",
    initialStream = "",
    initialOrder = "desc",
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    initialGroup?: string;
    initialTimestamp?: string;
    initialLevels?: string[];
    initialPattern?: string;
    initialStream?: string;
    initialOrder?: LogSortOrder;
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const LIVE_POLL_MS = 2000;
  const LIVE_BUFFER = 5000;
  const LEVELS = ["ERROR", "WARN", "INFO", "DEBUG"] as const;

  // ── State ────────────────────────────────────────────────────────────
  let groups = $state<LogGroupSummary[]>([]);
  let groupsLoading = $state(true);
  let groupsError = $state("");

  const ALL_GROUP = "__all__";

  let selectedGroup = $state("");
  let checkedGroups = $state<string[]>([]);
  let lastClickedIdx = $state<number | null>(null);

  const selectedGroupList = $derived(
    selectedGroup.includes(",")
      ? selectedGroup.split(",").filter(Boolean)
      : selectedGroup && selectedGroup !== ALL_GROUP
      ? [selectedGroup]
      : []
  );
  const isMultiGroup = $derived(selectedGroupList.length > 1);
  const selectedEventCount = $derived.by(() => {
    const set = new Set(checkedGroups);
    return groups.filter((g) => set.has(g.name)).reduce((sum, g) => sum + g.eventCount, 0);
  });

  let events = $state<LogEvent[]>([]);
  let eventsTotal = $state(0);
  let eventsLoading = $state(false);
  let eventsError = $state("");

  // Filters
  // Selected levels; empty = all. Sent as a comma set (level=ERROR,WARN).
  let filterLevels = $state<string[]>([]);
  let filterPattern = $state("");
  let filterStream = $state("");
  let eventsLimit = $state(200);
  let eventsCursor = $state<string | null>(null);
  let nextCursor = $state<string | null>(null);
  let prevCursors = $state<string[]>([]);
  let showFilters = $state(false);
  let autoRefresh = $state(false);
  let groupSearch = $state("");
  let serviceFilter = $state("all");
  let sortOrder = $state<LogSortOrder>("desc");

  // Log full-text scan state
  let scanResult = $state<LogScanResult | null>(null);
  let scanning = $state(false);
  let scanAbortController = $state<AbortController | null>(null);

  let groupSearchTimer: ReturnType<typeof setTimeout> | null = null;
  $effect(() => {
    const query = groupSearch.trim();
    if (!query) {
      if (scanAbortController) {
        scanAbortController.abort();
        scanAbortController = null;
      }
      scanResult = null;
      scanning = false;
      return;
    }

    if (groupSearchTimer) clearTimeout(groupSearchTimer);
    groupSearchTimer = setTimeout(async () => {
      if (scanAbortController) {
        scanAbortController.abort();
      }
      const ac = new AbortController();
      scanAbortController = ac;
      scanning = true;
      try {
        const res = await scanLogs({ pattern: query }, ac.signal);
        scanResult = res;
      } catch (err: any) {
        if (err?.name !== "AbortError") {
          // Ignore
        }
      } finally {
        if (scanAbortController === ac) {
          scanning = false;
        }
      }
    }, 200);
  });

  const scanMatchMap = $derived.by(() => {
    const map = new Map<string, number>();
    if (!scanResult) return map;
    for (const g of scanResult.groups) {
      map.set(g.groupName, g.matchCount);
    }
    return map;
  });

  // Detail panel
  let selectedEvent = $state<LogEvent | null>(null);
  let selectedKey = $state<string | null>(null);
  let logTrace = $state<RequestTrace | null>(null);
  let logTraceLoading = $state(false);

  let clearing = $state(false);
  let copyingStream = $state(false);

  // Deep-link highlight
  let highlightTimestamp = $state("");

  let stream = $state<ReturnType<typeof LogStream> | null>(null);


  const isAllGroup = $derived(selectedGroup === ALL_GROUP);
  const keys = $derived(keyEvents(events));
  const isFirstPage = $derived(!eventsCursor);
  const isLive = $derived(autoRefresh && isFirstPage);

  // ── Lifecycle ────────────────────────────────────────────────────────
  $effect(() => {
    loadGroups();
  });

  $effect(() => {
    if (initialGroup) {
      selectedGroup = initialGroup;
      if (initialGroup.includes(",")) {
        checkedGroups = initialGroup.split(",").filter(Boolean);
      } else if (initialGroup !== ALL_GROUP) {
        checkedGroups = [initialGroup];
      }
      eventsCursor = null;
      prevCursors = [];
    }
  });

  $effect(() => {
    if (initialTimestamp) highlightTimestamp = initialTimestamp;
  });

  // Seed filters from the URL before the loadEvents effect below.
  $effect(() => {
    filterLevels = initialLevels;
    filterPattern = initialPattern;
    filterStream = initialStream;
    sortOrder = initialOrder;
    if (initialStream) showFilters = true;
  });

  $effect(() => {
    if (selectedGroup) untrack(() => loadEvents());
  });

  // Live tail: merge newest events into the buffer rather than replacing it.
  $effect(() => {
    if (!isLive || !selectedGroup) return;
    const handle = setInterval(() => loadEvents({ merge: true }), LIVE_POLL_MS);
    return () => clearInterval(handle);
  });

  const highlightKey = $derived.by(() => {
    if (!highlightTimestamp) return null;
    const idx = events.findIndex((e) => e.timestamp === highlightTimestamp);
    return idx >= 0 ? keys[idx] : null;
  });

  // Scroll to and select the deep-linked event once it's loaded (once per link,
  // so live merges don't keep yanking the reader back).
  let handledHighlight = "";
  $effect(() => {
    const key = highlightKey;
    const s = stream;
    if (!key || !s || handledHighlight === highlightTimestamp) return;
    const idx = keys.indexOf(key);
    if (idx < 0) return;
    handledHighlight = highlightTimestamp;
    requestAnimationFrame(() => s.scrollToIndex(idx));
    selectedEvent = events[idx];
    selectedKey = key;
  });

  // Fetch trace for selected lambda log event
  $effect(() => {
    const ev = selectedEvent;
    if (!ev) {
      logTrace = null;
      logTraceLoading = false;
      return;
    }
    const evGroup = isMultiGroup
      ? (selectedGroupList.find((g) => ev.streamName.startsWith(g + "/") || ev.streamName === g) ?? selectedGroup)
      : selectedGroup;
    if (!evGroup || !evGroup.startsWith("/aws/lambda/")) {
      logTrace = null;
      logTraceLoading = false;
      return;
    }
    const ctrl = new AbortController();
    const functionName = evGroup.slice("/aws/lambda/".length);
    logTraceLoading = true;
    fetchTraceForLog(functionName, ev.timestamp, ctrl.signal)
      .then((t) => (logTrace = t))
      .catch(() => (logTrace = null))
      .finally(() => (logTraceLoading = false));
    return () => ctrl.abort();
  });

  // ── Data fetching ────────────────────────────────────────────────────
  async function loadGroups() {
    groupsLoading = true;
    groupsError = "";
    try {
      groups = await fetchLogGroups();
    } catch (err) {
      groupsError = err instanceof Error ? err.message : "Failed to load log groups";
    } finally {
      groupsLoading = false;
    }
  }

  let eventsController: AbortController | null = null;

  async function loadEvents(opts: { merge?: boolean } = {}) {
    if (!selectedGroup) return;
    // A background tick never interrupts a user-initiated load.
    if (opts.merge && eventsController) return;
    eventsController?.abort();
    const ctrl = new AbortController();
    eventsController = ctrl;
    if (!opts.merge) eventsLoading = true;
    eventsError = "";
    try {
      const params: FetchLogEventsParams = { limit: eventsLimit, order: sortOrder };
      if (eventsCursor && !opts.merge) params.cursor = eventsCursor;
      if (filterLevels.length) params.level = filterLevels.join(",");
      if (filterPattern) params.pattern = filterPattern;
      if (filterStream) params.stream = filterStream;

      const result = isAllGroup
        ? await fetchAllLogEvents(params, ctrl.signal)
        : isMultiGroup
        ? await fetchAllLogEvents({ ...params, groups: selectedGroupList }, ctrl.signal)
        : await fetchLogEvents(selectedGroup, params, ctrl.signal);
      if (ctrl.signal.aborted) return;
      let fetched = result.events ?? [];
      if (isMultiGroup) {
        fetched = fetched.filter((e) =>
          selectedGroupList.some((g) => e.streamName.startsWith(g + "/") || e.streamName === g),
        );
      }
      eventsTotal = result.total ?? 0;

      if (opts.merge) {
        events = mergeEvents(events, fetched);
      } else {
        events = fetched;
        nextCursor = result.nextCursor || null;
      }
    } catch (err) {
      if (ctrl.signal.aborted) return;
      eventsError = err instanceof Error ? err.message : "Failed to load log events";
    } finally {
      if (eventsController === ctrl) {
        eventsController = null;
        eventsLoading = false;
      }
    }
  }

  function mergeEvents(current: LogEvent[], fetched: LogEvent[]): LogEvent[] {
    if (fetched.length === 0) return current;
    const known = new Set(keyEvents(current));
    const fetchedKeys = keyEvents(fetched);
    const fresh: LogEvent[] = [];
    for (let i = 0; i < fetched.length; i++) {
      if (!known.has(fetchedKeys[i])) fresh.push(fetched[i]);
    }
    if (fresh.length === 0) return current;

    const merged = sortOrder === "desc" ? [...fresh, ...current] : [...current, ...fresh];
    const dir = sortOrder === "desc" ? -1 : 1;
    // Stable sort keeps arrival order for identical timestamps.
    merged.sort((a, b) => dir * (Date.parse(a.timestamp) - Date.parse(b.timestamp)));
    if (merged.length > LIVE_BUFFER) {
      return sortOrder === "desc" ? merged.slice(0, LIVE_BUFFER) : merged.slice(-LIVE_BUFFER);
    }
    return merged;
  }

  // ── Helpers ──────────────────────────────────────────────────────────
  function resetPaging() {
    eventsCursor = null;
    prevCursors = [];
    nextCursor = null;
    selectedEvent = null;
    selectedKey = null;
  }

  function navigationFilters(): LogNavigationFilters {
    return {
      levels: filterLevels,
      pattern: filterPattern,
      stream: filterStream,
      order: sortOrder,
    };
  }

  function filteredLogLocation(hash: string): string {
    return logLocationWithFilters(hash, navigationFilters());
  }

  function syncLogLocation(): void {
    const nextHash = filteredLogLocation(window.location.hash);
    history.replaceState(history.state, "", nextHash);
  }

  function selectGroup(name: string, initialPattern?: string) {
    selectedGroup = name;
    if (initialPattern) {
      filterPattern = initialPattern;
    }
    if (name && name !== ALL_GROUP && !checkedGroups.includes(name)) {
      checkedGroups = [name];
    }
    resetPaging();
    highlightTimestamp = "";
    window.location.hash = filteredLogLocation(`#logs?group=${encodeURIComponent(name)}`);
  }

  function handleGroupClick(e: MouseEvent, group: LogGroupSummary, idx: number) {
    const isCheckbox = (e.target as HTMLElement)?.closest(".rc-checkbox") !== null;

    if (e.shiftKey) {
      e.preventDefault();
      window.getSelection()?.removeAllRanges();
      if (lastClickedIdx === null || lastClickedIdx === undefined) {
        lastClickedIdx = idx;
        if (!checkedGroups.includes(group.name)) {
          checkedGroups = [...checkedGroups, group.name];
        }
      } else {
        const start = Math.min(lastClickedIdx, idx);
        const end = Math.max(lastClickedIdx, idx);
        const rangeNames = filteredGroups.slice(start, end + 1).map((g) => g.name);
        const set = new Set([...checkedGroups, ...rangeNames]);
        checkedGroups = Array.from(set);
        lastClickedIdx = idx;
      }
      return;
    }

    if (e.metaKey || e.ctrlKey || isCheckbox) {
      e.preventDefault();
      if (checkedGroups.includes(group.name)) {
        checkedGroups = checkedGroups.filter((n) => n !== group.name);
      } else {
        checkedGroups = [...checkedGroups, group.name];
      }
      lastClickedIdx = idx;
      return;
    }

    if (checkedGroups.length > 0) {
      if (checkedGroups.includes(group.name)) {
        checkedGroups = checkedGroups.filter((n) => n !== group.name);
      } else {
        checkedGroups = [...checkedGroups, group.name];
      }
      lastClickedIdx = idx;
      return;
    }

    selectGroup(group.name, groupSearch.trim());
  }

  function toggleGroup(name: string, idx?: number) {
    if (checkedGroups.includes(name)) {
      checkedGroups = checkedGroups.filter((n) => n !== name);
    } else {
      checkedGroups = [...checkedGroups, name];
    }
    if (idx !== undefined) {
      lastClickedIdx = idx;
    }
  }

  function clearGroupSelection() {
    checkedGroups = [];
    lastClickedIdx = null;
  }

  function selectAllFiltered() {
    const all = filteredGroups.map((g) => g.name);
    checkedGroups = Array.from(new Set([...checkedGroups, ...all]));
  }

  function viewSelectedGroups() {
    if (checkedGroups.length === 0) return;
    if (checkedGroups.length === 1) {
      selectGroup(checkedGroups[0], groupSearch.trim());
      return;
    }
    selectedGroup = checkedGroups.join(",");
    if (groupSearch.trim()) {
      filterPattern = groupSearch.trim();
    }
    resetPaging();
    highlightTimestamp = "";
    window.location.hash = filteredLogLocation(`#logs?groups=${encodeURIComponent(selectedGroup)}`);
    loadEvents();
  }

  function removeGroupFromSelection(name: string) {
    const updated = selectedGroupList.filter((g) => g !== name);
    checkedGroups = checkedGroups.filter((g) => g !== name);
    if (updated.length === 0) {
      selectedGroup = "";
      window.location.hash = "logs";
    } else if (updated.length === 1) {
      selectGroup(updated[0]);
    } else {
      selectedGroup = updated.join(",");
      resetPaging();
      highlightTimestamp = "";
      window.location.hash = filteredLogLocation(`#logs?groups=${encodeURIComponent(selectedGroup)}`);
      loadEvents();
    }
  }

  function handleListKeyDown(e: KeyboardEvent) {
    if (selectedGroup) return;
    const target = e.target as HTMLElement;
    if (target?.tagName === "INPUT" || target?.tagName === "TEXTAREA") {
      return;
    }
    if (e.key === "Escape" && checkedGroups.length > 0) {
      e.preventDefault();
      e.stopPropagation();
      clearGroupSelection();
    } else if (e.key === "Enter" && checkedGroups.length > 0) {
      e.preventDefault();
      e.stopPropagation();
      viewSelectedGroups();
    }
  }

  function backToGroups() {
    selectedGroup = "";
    events = [];
    eventsTotal = 0;
    resetPaging();
    autoRefresh = false;
    highlightTimestamp = "";
    window.location.hash = "logs";
    loadGroups();
  }

  function applyFilters() {
    resetPaging();
    syncLogLocation();
    loadEvents();
  }

  function clearFilters() {
    filterLevels = [];
    filterPattern = "";
    filterStream = "";
    applyFilters();
  }

  function setLevel(level: string) {
    filterLevels = filterLevels.includes(level)
      ? filterLevels.filter((l) => l !== level)
      : [...filterLevels, level];
    applyFilters();
  }

  let patternTimer: ReturnType<typeof setTimeout> | null = null;
  function onPatternInput() {
    syncLogLocation();
    if (patternTimer) clearTimeout(patternTimer);
    patternTimer = setTimeout(applyFilters, 280);
  }

  function toggleSort() {
    sortOrder = sortOrder === "desc" ? "asc" : "desc";
    applyFilters();
  }

  async function handleClearLogs() {
    if (!selectedGroup || isAllGroup) return;
    clearing = true;
    try {
      await clearLogGroup(selectedGroup);
      events = [];
      eventsTotal = 0;
      resetPaging();
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
      const params: FetchLogEventsParams = { limit: 10000, stream: filterStream, order: sortOrder };
      const result = isAllGroup
        ? await fetchAllLogEvents(params)
        : await fetchLogEvents(selectedGroup, params);
      const text = (result.events ?? [])
        .map((e) => `${new Date(e.timestamp).toISOString()} [${e.level}] ${e.message}`)
        .join("\n");
      await navigator.clipboard.writeText(text);
      setTimeout(() => (copyingStream = false), 2000);
    } catch {
      copyingStream = false;
    }
  }

  function nextPage() {
    if (!nextCursor) return;
    prevCursors = [...prevCursors, eventsCursor || ""];
    eventsCursor = nextCursor;
    selectedEvent = null;
    selectedKey = null;
    loadEvents();
  }

  function prevPage() {
    if (prevCursors.length > 0) {
      eventsCursor = prevCursors[prevCursors.length - 1] || null;
      prevCursors = prevCursors.slice(0, -1);
    } else {
      eventsCursor = null;
    }
    selectedEvent = null;
    selectedKey = null;
    loadEvents();
  }

  function selectEvent(event: LogEvent, key: string) {
    selectedEvent = event;
    selectedKey = key;
  }

  function closeDetail() {
    selectedEvent = null;
    selectedKey = null;
  }

  function formatDetailTimestamp(ts: string): string {
    try {
      return new Date(ts).toISOString().replace("T", "  ").replace("Z", "  UTC");
    } catch {
      return ts;
    }
  }

  function formatCompactTime(ts: string): string {
    const d = new Date(ts);
    if (Number.isNaN(d.getTime())) return ts;
    const p = (n: number, w = 2) => String(n).padStart(w, "0");
    return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`;
  }

  function relativeAge(ts: string | undefined): string {
    if (!ts) return "—";
    const ms = Date.now() - Date.parse(ts);
    if (Number.isNaN(ms)) return "—";
    if (ms < 5_000) return "now";
    if (ms < 60_000) return `${Math.floor(ms / 1000)}s`;
    if (ms < 3_600_000) return `${Math.floor(ms / 60_000)}m`;
    if (ms < 86_400_000) return `${Math.floor(ms / 3_600_000)}h`;
    return `${Math.floor(ms / 86_400_000)}d`;
  }

  function isRecent(ts: string | undefined, windowMs = 15_000): boolean {
    if (!ts) return false;
    const t = Date.parse(ts);
    return !Number.isNaN(t) && Date.now() - t < windowMs;
  }

  // ── Log message analysis ─────────────────────────────────────────────
  const parsedSpringBootLog = $derived(selectedEvent ? parseSpringBootLog(selectedEvent.message) : null);
  const panelDisplayMessage = $derived(
    selectedEvent ? (parsedSpringBootLog ? parsedSpringBootLog.message : selectedEvent.message) : "",
  );
  const panelIsComplex = $derived(selectedEvent ? isComplexMessage(selectedEvent.message) : false);
  const panelFormattedMessage = $derived(panelIsComplex ? computeFormattedMessage(panelDisplayMessage) : null);
  const panelHighlightedHtml = $derived(
    panelFormattedMessage !== null ? highlightFormatted(panelFormattedMessage) : null,
  );

  function groupDisplayName(name: string): string {
    if (name.startsWith("/aws/lambda/")) return name.slice("/aws/lambda/".length);
    if (name.startsWith("/tarn/")) return name.slice(1);
    return name;
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
      case "all": return "All";
      case "lambda": return "Lambda";
      case "api": return "API";
      case "system": return "System";
      case "apigatewayv2": return "API Gateway";
      case "sns": return "SNS";
      case "sqs": return "SQS";
      case "secretsmanager": return "Secrets";
      default: return "Other";
    }
  }

  function openInXRay(trace: RequestTrace) {
    window.location.hash = `xray?trace=${encodeURIComponent(trace.id)}`;
  }

  const logWaterfallRows = $derived(
    logTrace ? buildWaterfall(logTrace.spans, logTrace.durationMs) : ([] as WaterfallRow[]),
  );
  const selectedGroupIsLambda = $derived(selectedGroup.startsWith("/aws/lambda/"));
  const hasNextPage = $derived(!!nextCursor);
  const hasPrevPage = $derived(prevCursors.length > 0 || !!eventsCursor);
  const hasPagination = $derived(!isLive && (hasNextPage || hasPrevPage));
  const pageInfo = $derived(
    events.length > 0 ? `${events.length.toLocaleString()} of ${eventsTotal.toLocaleString()} events` : "No events",
  );

  // Level mix of the loaded window, for the quick-filter chips.
  const levelCounts = $derived.by(() => {
    const counts: Record<string, number> = { ERROR: 0, WARN: 0, INFO: 0, DEBUG: 0 };
    for (const ev of events) counts[ev.level] = (counts[ev.level] ?? 0) + 1;
    return counts;
  });

  // Level chips only earn their space once logs mix levels. Stay visible while
  // a level filter is active so the user can always clear it.
  const showLevelChips = $derived(
    filterLevels.length > 0 || Object.values(levelCounts).filter((n) => n > 0).length > 1,
  );

  // ── Timeline ribbon: loaded events bucketed across their time span ──
  const RIBBON_BUCKETS = 72;
  const ribbon = $derived.by(() => {
    const n = events.length;
    if (n === 0) return null;
    let min = Infinity;
    let max = -Infinity;
    const times = new Float64Array(n);
    for (let i = 0; i < n; i++) {
      const t = Date.parse(events[i].timestamp);
      times[i] = t;
      if (t < min) min = t;
      if (t > max) max = t;
    }
    const span = Math.max(1, max - min);
    const counts = new Array<number>(RIBBON_BUCKETS).fill(0);
    const errors = new Array<number>(RIBBON_BUCKETS).fill(0);
    const firstIdx = new Array<number>(RIBBON_BUCKETS).fill(-1);
    for (let i = 0; i < n; i++) {
      const b = Math.min(RIBBON_BUCKETS - 1, Math.floor(((times[i] - min) / span) * RIBBON_BUCKETS));
      counts[b]++;
      if (events[i].level === "ERROR") errors[b]++;
      if (firstIdx[b] < 0) firstIdx[b] = i;
    }
    const peak = Math.max(...counts);
    const bucketMs = span / RIBBON_BUCKETS;
    // Events/sec over the most recent 10s of the window.
    let recent = 0;
    for (let i = 0; i < n; i++) if (max - times[i] <= 10_000) recent++;
    return { counts, errors, firstIdx, peak, min, max, bucketMs, rate: recent / 10 };
  });

  let ribbonHover = $state(-1);

  function ribbonLabel(b: number): string {
    if (!ribbon) return "";
    const from = new Date(ribbon.min + b * ribbon.bucketMs);
    const count = ribbon.counts[b];
    const errs = ribbon.errors[b];
    return `${formatCompactTime(from.toISOString())} · ${count} event${count === 1 ? "" : "s"}${errs ? ` · ${errs} err` : ""}`;
  }

  // ── Groups list ──────────────────────────────────────────────────────
  const totalEventCount = $derived(groups.reduce((sum, g) => sum + g.eventCount, 0));
  const serviceOptions = $derived.by(() => {
    const counts = new Map<string, number>();
    for (const group of groups) {
      const key = groupServiceKey(group.name);
      counts.set(key, (counts.get(key) ?? 0) + 1);
    }
    const orderedKeys = ["lambda", "api", "system", "apigatewayv2", "sns", "sqs", "secretsmanager", "other"];
    const options = [{ key: "all", label: serviceLabel("all"), count: groups.length }];
    for (const key of orderedKeys) {
      const count = counts.get(key) ?? 0;
      if (count > 0) options.push({ key, label: serviceLabel(key), count });
    }
    return options;
  });
  const filteredGroups = $derived(
    groups
      .filter((group) => {
        if (serviceFilter !== "all" && groupServiceKey(group.name) !== serviceFilter) return false;
        const query = groupSearch.trim().toLowerCase();
        if (!query) return true;
        const nameMatch = group.name.toLowerCase().includes(query) || groupDisplayName(group.name).toLowerCase().includes(query);
        const scanMatches = scanMatchMap.get(group.name) ?? 0;
        return nameMatch || scanMatches > 0;
      })
      .sort((a, b) => {
        const aMatches = scanMatchMap.get(a.name) ?? 0;
        const bMatches = scanMatchMap.get(b.name) ?? 0;
        if (aMatches !== bMatches) return bMatches - aMatches;
        return b.eventCount - a.eventCount;
      }),
  );
  const groupsCountLabel = $derived(
    filteredGroups.length === groups.length
      ? `${groups.length} groups`
      : `${filteredGroups.length} of ${groups.length} groups`,
  );
  const maxGroupEvents = $derived(Math.max(1, ...groups.map((g) => g.eventCount)));
  const latestGroupEvent = $derived(
    groups.reduce<string | undefined>(
      (latest, g) => (g.lastEvent && (!latest || g.lastEvent > latest) ? g.lastEvent : latest),
      undefined,
    ),
  );

  // Group rows share the sidebar's motion-pill language.
  let groupListEl = $state<HTMLElement | null>(null);
  let groupHover = $state<{ top: number; height: number } | null>(null);

  function onGroupPointerMove(e: PointerEvent) {
    const row = (e.target as HTMLElement).closest<HTMLElement>("[data-group-row]");
    if (!row || !groupListEl) return;
    groupHover = { top: row.offsetTop, height: row.offsetHeight };
  }
</script>

<svelte:window
  onkeydown={(e) => {
    if (e.key === "Escape" && selectedEvent) {
      closeDetail();
    } else {
      handleListKeyDown(e);
    }
  }}
/>

{#snippet toolButton(label: string, icon: any, onclick: () => void, opts: { active?: boolean; spin?: boolean; disabled?: boolean; tone?: "danger"; title?: string } = {})}
  {@const Icon = icon}
  <button
    type="button"
    {onclick}
    disabled={opts.disabled}
    title={opts.title}
    class="rc-tool"
    class:active={opts.active}
    class:danger={opts.tone === "danger"}
  >
    <Icon size={12} class={opts.spin ? "animate-spin" : ""} />
    {label}
  </button>
{/snippet}

{#if selectedGroup}
  <!-- ── Event viewer ─────────────────────────────────────────────── -->
  <div class="flex h-full min-h-0 flex-col gap-3">
    <SectionHeader
      title={isAllGroup
        ? "All log groups"
        : isMultiGroup
        ? `Selected log groups (${selectedGroupList.length})`
        : groupDisplayName(selectedGroup)}
      description={isAllGroup
        ? pageInfo
        : isMultiGroup
        ? `${selectedGroupList.length} groups in selection · ${pageInfo}`
        : `${selectedGroup} · ${pageInfo}`}
      {sidebarCollapsed}
      {onToggleSidebar}
    >
      {#snippet lead()}
        <button type="button" onclick={backToGroups} class="rc-icon-btn" aria-label="Back to log groups">
          <ArrowLeftIcon size={15} />
        </button>
      {/snippet}

      {#snippet actions()}
        <div class="flex shrink-0 items-center gap-1.5">
          <button
            type="button"
            class="rc-live"
            class:on={autoRefresh}
            onclick={() => (autoRefresh = !autoRefresh)}
            title={autoRefresh ? "Stop tailing" : "Tail new events"}
          >
            <span class="rc-live-dot" aria-hidden="true"></span>
            {autoRefresh ? "Live" : "Tail"}
            {#if autoRefresh && ribbon}
              <span class="rc-live-rate">{ribbon.rate.toFixed(1)}/s</span>
            {/if}
          </button>
          {@render toolButton(sortOrder === "desc" ? "Newest" : "Oldest", sortOrder === "desc" ? SortDescendingIcon : SortAscendingIcon, toggleSort, {
            title: sortOrder === "desc" ? "Showing newest first" : "Showing oldest first",
          })}
          {@render toolButton("Filters", FunnelIcon, () => (showFilters = !showFilters), { active: showFilters || !!filterStream })}
          {#if !isAllGroup && !isMultiGroup}
            {@render toolButton("Clear", TrashIcon, handleClearLogs, {
              tone: "danger",
              disabled: clearing || events.length === 0,
              title: "Clear all logs in this group",
            })}
          {/if}
          {@render toolButton("Refresh", ArrowsClockwiseIcon, () => loadEvents(), { spin: eventsLoading, disabled: eventsLoading })}
        </div>
      {/snippet}
    </SectionHeader>

    <!-- Query bar: search + level chips -->
    <div class="rc-querybar">
      <label class="rc-search">
        <MagnifyingGlassIcon size={13} class="shrink-0 text-[var(--text-tertiary)]" />
        <input
          type="text"
          placeholder="Filter messages"
          bind:value={filterPattern}
          oninput={onPatternInput}
          onkeydown={(e) => {
            if (e.key === "Enter") applyFilters();
          }}
        />
        {#if filterPattern}
          <span class="rc-mono text-[10.5px] text-[var(--text-tertiary)] shrink-0 pr-1">
            {eventsTotal.toLocaleString()} {eventsTotal === 1 ? "match" : "matches"}
          </span>
          <button type="button" class="rc-search-clear" aria-label="Clear search" onclick={() => { filterPattern = ""; applyFilters(); }}>
            <XIcon size={11} />
          </button>
        {/if}
      </label>

      {#if showLevelChips}
      <div class="rc-levels" role="group" aria-label="Level filter" transition:fly={{ x: 6, duration: 220 }}>
        {#each LEVELS as level, i (level)}
          <button
            type="button"
            class="rc-level rc-level-reveal"
            style:--i={i}
            class:active={filterLevels.includes(level)}
            data-level={level}
            onclick={() => setLevel(level)}
            aria-pressed={filterLevels.includes(level)}
          >
            {level.toLowerCase()}
            <span class="rc-level-count">{levelCounts[level] ?? 0}</span>
          </button>
        {/each}
      </div>
      {/if}
    </div>

    {#if isMultiGroup}
      <div class="rc-selection-chips">
        <span class="rc-selection-label">Selection:</span>
        {#each selectedGroupList as g (g)}
          <span class="rc-group-chip">
            <span class="rc-chip-dot"></span>
            <span class="rc-chip-text" title={g}>{groupDisplayName(g)}</span>
            <button
              type="button"
              class="rc-chip-remove"
              onclick={() => removeGroupFromSelection(g)}
              aria-label="Remove {g} from selection"
            >
              <XIcon size={11} />
            </button>
          </span>
        {/each}
        <button
          type="button"
          class="rc-chip-add"
          onclick={backToGroups}
        >
          <PlusIcon size={11} /> Edit selection
        </button>
      </div>
    {/if}

    {#if showFilters}
      <div class="rc-filters">
        <label class="rc-field">
          <span>Stream</span>
          <input
            type="text"
            placeholder="Stream name"
            bind:value={filterStream}
            onkeydown={(e) => {
              if (e.key === "Enter") applyFilters();
            }}
          />
        </label>
        <div class="flex items-end gap-1.5">
          {@render toolButton("Apply", CaretRightIcon, applyFilters, { active: true })}
          {@render toolButton("Reset", XIcon, clearFilters)}
          {@render toolButton(copyingStream ? "Copied" : "Copy stream", ClipboardTextIcon, handleCopyStream, {
            disabled: !filterStream,
            title: filterStream ? "Copy entire stream output to clipboard" : "Filter by a stream name first",
          })}
        </div>
      </div>
    {/if}

    {#if eventsError}
      <div class="rc-error">{eventsError}</div>
    {/if}

    {#if eventsLoading && events.length === 0}
      <div class="rc-panel flex-1 space-y-1.5">
        {#each Array(14) as _, i (i)}
          <Skeleton class="h-4 w-full" />
        {/each}
      </div>
    {:else if events.length === 0 && !eventsError}
      <EmptyState message="No log events found. Try adjusting your filters or invoke a function." icon={ScrollIcon} />
    {:else if events.length > 0}
      {#snippet streamPanel()}
        <div class="flex h-full min-h-0 flex-col">
          {#if ribbon}
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="rc-ribbon" onpointerleave={() => (ribbonHover = -1)}>
              {#each ribbon.counts as count, b (b)}
                <button
                  type="button"
                  class="rc-ribbon-bar"
                  class:hot={ribbonHover === b}
                  disabled={count === 0}
                  onpointerenter={() => (ribbonHover = b)}
                  onclick={() => stream?.scrollToIndex(ribbon.firstIdx[b])}
                  aria-label={ribbonLabel(b)}
                >
                  <span
                    class="rc-ribbon-fill"
                    style:height="{count === 0 ? 0 : 12 + Math.sqrt(count / ribbon.peak) * 88}%"
                  ></span>
                  {#if ribbon.errors[b] > 0}
                    <span class="rc-ribbon-err" style:height="{Math.max(18, (ribbon.errors[b] / count) * 100)}%"></span>
                  {/if}
                </button>
              {/each}
              <span class="rc-ribbon-label">
                {ribbonHover >= 0 ? ribbonLabel(ribbonHover) : `${formatCompactTime(new Date(ribbon.min).toISOString())} → ${formatCompactTime(new Date(ribbon.max).toISOString())}`}
              </span>
            </div>
          {/if}
          <div class="min-h-0 flex-1">
            <LogStream
              bind:this={stream}
              {events}
              {keys}
              {selectedKey}
              {highlightKey}
              highlightPattern={filterPattern}
              order={sortOrder}
              showGroup={isAllGroup || isMultiGroup}
              showStream={!selectedEvent}
              onSelect={selectEvent}
            />
          </div>
        </div>
      {/snippet}

      <div class="rc-panel min-h-0 flex-1">
        {#if selectedEvent}
          {@const ev = selectedEvent}
          <PaneGroup direction="horizontal" class="h-full min-h-0">
            <Pane defaultSize={62} minSize={35} class="flex min-h-0 flex-col overflow-hidden">
              {@render streamPanel()}
            </Pane>
            <Handle />
            <Pane defaultSize={38} minSize={25} class="flex min-h-0 flex-col overflow-hidden">
              <div class="rc-detail" data-level={ev.level}>
                <div class="rc-detail-head">
                  <span class="rc-badge" data-level={ev.level}>{ev.level}</span>
                  <span class="rc-mono truncate text-[11px] text-[var(--text-tertiary)]">{formatCompactTime(ev.timestamp)}</span>
                  <button type="button" onclick={closeDetail} class="rc-icon-btn ml-auto" aria-label="Close detail panel">
                    <XIcon size={13} />
                  </button>
                </div>

                <div class="flex-1 space-y-5 overflow-y-auto p-4">
                  <section>
                    <p class="rc-label">Message</p>
                    {#if panelIsComplex}
                      <FormattedMessageViewer
                        raw={ev.message}
                        formatted={panelHighlightedHtml === null ? panelDisplayMessage : panelFormattedMessage}
                        formattedHtml={panelHighlightedHtml}
                        variant="tabs"
                        formattedContentClass="text-[12px] text-foreground"
                        rawContentClass="text-[11px] text-muted-foreground"
                        formattedMaxHeightClass="max-h-[55vh]"
                        rawMaxHeightClass="max-h-[40vh]"
                      />
                    {:else if looksLikeJSON(ev.message) && tryFormatInlineJSON(ev.message).isJSON}
                      <pre class="rc-code max-h-[55vh] overflow-y-auto">{@html highlightJSON(tryFormatInlineJSON(ev.message).formatted)}</pre>
                    {:else}
                      <pre class="rc-code">{ev.message}</pre>
                    {/if}
                  </section>

                  <section>
                    <p class="rc-label">Details</p>
                    <dl class="rc-kv">
                      <div><dt>Time</dt><dd>{formatDetailTimestamp(ev.timestamp)}</dd></div>
                      {#if parsedSpringBootLog}
                        <div><dt>Thread</dt><dd>{parsedSpringBootLog.thread}</dd></div>
                        <div><dt>Logger</dt><dd>{parsedSpringBootLog.logger}</dd></div>
                        <div><dt>PID</dt><dd>{parsedSpringBootLog.pid}</dd></div>
                      {/if}
                      {#if ev.streamName}
                        <div>
                          <dt>Stream</dt>
                          <dd>
                            <button
                              type="button"
                              class="rc-link"
                              title="Filter to this stream"
                              onclick={() => {
                                filterStream = ev.streamName;
                                showFilters = true;
                                applyFilters();
                              }}>{ev.streamName}</button
                            >
                          </dd>
                        </div>
                      {/if}
                      {#if ev.source}
                        <div><dt>Source</dt><dd>{ev.source}</dd></div>
                      {/if}
                    </dl>
                  </section>

                  {#if selectedGroupIsLambda}
                    <section>
                      <div class="flex items-center justify-between">
                        <p class="rc-label">Trace context</p>
                        {#if logTrace}
                          {@const lt = logTrace}
                          <button type="button" onclick={() => openInXRay(lt)} class="rc-link inline-flex items-center gap-1 text-[10.5px]">
                            <ArrowUpRightIcon size={10} />
                            Traces
                          </button>
                        {/if}
                      </div>

                      {#if logTraceLoading}
                        <div class="rc-subpanel space-y-2 px-3 py-2.5">
                          <Skeleton class="h-3 w-3/4" />
                          <Skeleton class="h-2.5 w-full" />
                          <Skeleton class="h-2.5 w-4/5" />
                        </div>
                      {:else if logTrace}
                        {@const t = logTrace}
                        <div class="rc-subpanel overflow-hidden">
                          <div class="flex items-center gap-2 border-b border-[var(--border-subtle)] px-3 py-2">
                            <span class="rc-mono shrink-0 text-[10px] text-[var(--text-tertiary)]">via</span>
                            <span class="rc-mono truncate text-[11px] text-[var(--text-primary)]">{traceTitle(t)}</span>
                            <span class="rc-mono ml-auto shrink-0 text-[10px] text-[var(--text-tertiary)]">{formatMs(t.durationMs)}</span>
                          </div>
                          <div class="space-y-1.5 px-3 py-2">
                            {#each logWaterfallRows as { span, offsetPct, widthPct, nested }, i (i)}
                              {@const color = spanColor(span.kind)}
                              {@const opacity = span.status === "error" ? 0.85 : nested ? 0.4 : 0.5}
                              <div class="flex items-center gap-2 {nested ? 'pl-3' : ''}">
                                {#if nested}
                                  <span class="rc-mono shrink-0 select-none text-[10px] text-[var(--text-tertiary)]">└</span>
                                {/if}
                                <div class="w-[5.5rem] shrink-0">
                                  <span
                                    class="rc-mono block whitespace-nowrap rounded px-1 py-0.5 text-right text-[9px]"
                                    style="color:{color};background:{color}18;border:1px solid {color}28"
                                  >{spanKindLabel(span.kind)}</span>
                                </div>
                                <span class="rc-mono w-24 shrink-0 truncate text-[10px] text-[var(--text-secondary)]" title={span.name}>{span.name}</span>
                                <div class="relative flex-1 overflow-hidden rounded bg-[var(--bg-element)] {nested ? 'h-2' : 'h-2.5'}">
                                  <div
                                    class="absolute top-0 h-full rounded"
                                    style="left:{offsetPct}%;width:{widthPct}%;background:{color};opacity:{opacity};min-width:2px"
                                  ></div>
                                </div>
                                <span class="rc-mono w-10 shrink-0 text-right text-[9px] text-[var(--text-tertiary)]">{formatMs(span.durationMs)}</span>
                              </div>
                            {/each}
                          </div>
                        </div>
                      {:else}
                        <p class="text-[11px] italic text-[var(--text-tertiary)]">No trace recorded for this event</p>
                      {/if}
                    </section>
                  {/if}
                </div>
              </div>
            </Pane>
          </PaneGroup>
        {:else}
          {@render streamPanel()}
        {/if}
      </div>

      {#if hasPagination}
        <div class="flex shrink-0 items-center justify-between px-1">
          <p class="rc-mono text-[11px] text-[var(--text-tertiary)]">{pageInfo}</p>
          <div class="flex items-center gap-1.5">
            {@render toolButton("Previous", ArrowLeftIcon, prevPage, { disabled: !hasPrevPage })}
            {@render toolButton("Next", CaretRightIcon, nextPage, { disabled: !hasNextPage })}
          </div>
        </div>
      {/if}
    {/if}
  </div>
{:else}
  <!-- ── Groups list ──────────────────────────────────────────────── -->
  <div class="relative flex h-full min-h-0 flex-col gap-3">
    <SectionHeader title="Log groups" description={groupsCountLabel} {sidebarCollapsed} {onToggleSidebar}>
      {#snippet actions()}
        {@render toolButton("Refresh", ArrowsClockwiseIcon, loadGroups, { spin: groupsLoading, disabled: groupsLoading })}
      {/snippet}
    </SectionHeader>

    <div class="rc-querybar">
      <label class="rc-search">
        <MagnifyingGlassIcon size={13} class="shrink-0 text-[var(--text-tertiary)]" />
        <input type="text" placeholder="Search logs (e.g. correlation-id) or groups" bind:value={groupSearch} />
        {#if scanning}
          <span class="rc-mono text-[10.5px] text-[var(--text-tertiary)] animate-pulse shrink-0">Scanning...</span>
        {:else if groupSearch.trim()}
          <button type="button" class="rc-search-clear" aria-label="Clear search" onclick={() => { groupSearch = ""; }}>
            <XIcon size={11} />
          </button>
        {/if}
      </label>
      <div class="rc-levels" role="group" aria-label="Service filter">
        {#each serviceOptions as option (option.key)}
          <button
            type="button"
            class="rc-level"
            class:active={serviceFilter === option.key}
            onclick={() => (serviceFilter = option.key)}
            aria-pressed={serviceFilter === option.key}
          >
            {option.label}
            <span class="rc-level-count">{option.count}</span>
          </button>
        {/each}
      </div>
      {#if filteredGroups.length > 1}
        <div class="flex items-center gap-1.5 ml-auto">
          {#if checkedGroups.length > 0}
            <button type="button" class="rc-tool" onclick={clearGroupSelection}>
              Deselect ({checkedGroups.length})
            </button>
          {/if}
          <button
            type="button"
            class="rc-tool"
            onclick={selectAllFiltered}
            title="Select all {filteredGroups.length} visible groups"
          >
            Select all
          </button>
        </div>
      {/if}
    </div>

    {#if scanResult && groupSearch.trim()}
      <div class="flex items-center justify-between px-3 py-2 rounded-lg bg-[color-mix(in_srgb,var(--accent-amber)_8%,transparent)] border border-[color-mix(in_srgb,var(--accent-amber)_25%,transparent)] text-[11.5px]">
        <div class="flex items-center gap-2">
          <span class="font-semibold text-[var(--accent-amber)]">
            {scanResult.totalMatches.toLocaleString()} {scanResult.totalMatches === 1 ? "match" : "matches"}
          </span>
          <span class="text-[var(--text-secondary)]">
            across {scanResult.groups.length} {scanResult.groups.length === 1 ? "group" : "groups"} (scanned {scanResult.totalScanned.toLocaleString()} events in {scanResult.durationMs}ms)
          </span>
        </div>
        {#if scanResult.totalMatches > 0}
          <button
            type="button"
            class="rc-tool text-[11px] font-medium text-[var(--accent-amber)] hover:underline flex items-center gap-1"
            onclick={() => selectGroup(ALL_GROUP, groupSearch.trim())}
          >
            View all matches &rarr;
          </button>
        {/if}
      </div>
    {/if}

    {#if groupsError}
      <div class="rc-error">{groupsError}</div>
    {/if}

    {#if groupsLoading && groups.length === 0}
      <div class="space-y-1.5">
        {#each Array(5) as _, i (i)}
          <Skeleton class="h-11 w-full rounded-md" />
        {/each}
      </div>
    {:else if groups.length === 0}
      <EmptyState message="No log groups yet. Create a Lambda function or invoke one to see logs." icon={ScrollIcon} />
    {:else}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="rc-groups min-h-0 flex-1 overflow-y-auto"
        class:has-selection={checkedGroups.length > 0}
        bind:this={groupListEl}
        onpointermove={onGroupPointerMove}
        onpointerleave={() => (groupHover = null)}
      >
        <div
          class="rc-group-pill"
          style:transform="translate3d(0, {groupHover?.top ?? 0}px, 0)"
          style:height="{groupHover?.height ?? 0}px"
          style:opacity={groupHover ? 1 : 0}
          aria-hidden="true"
        ></div>

        <button type="button" data-group-row class="rc-group all" onclick={() => selectGroup(ALL_GROUP, groupSearch.trim())}>
          <span class="rc-checkbox-spacer" aria-hidden="true"></span>
          <span aria-hidden="true"></span>
          <span class="rc-group-main">
            <span class="rc-group-name">All logs</span>
            <span class="rc-group-path">
              {#if scanResult && groupSearch.trim()}
                {scanResult.totalMatches.toLocaleString()} matching {scanResult.totalMatches === 1 ? "event" : "events"} across {scanResult.groups.length} {scanResult.groups.length === 1 ? "group" : "groups"} ({scanResult.durationMs}ms)
              {:else}
                Aggregated across every group
              {/if}
            </span>
          </span>
          {#if scanResult && groupSearch.trim()}
            <span class="rc-badge font-mono text-[11px] font-semibold text-[var(--accent-amber)] bg-[color-mix(in_srgb,var(--accent-amber)_15%,transparent)] border border-[color-mix(in_srgb,var(--accent-amber)_30%,transparent)] px-2 py-0.5 rounded-full">
              {scanResult.totalMatches.toLocaleString()} matches
            </span>
          {/if}
          <span class="rc-group-meter" aria-hidden="true"><span style:width="100%"></span></span>
          <span class="rc-group-stat">{totalEventCount.toLocaleString()}<small>events</small></span>
          <span class="rc-group-stat streams">{groups.length}<small>groups</small></span>
          <span class="rc-group-stat age">{relativeAge(latestGroupEvent)}</span>
        </button>

        <div class="rc-groups-divider"></div>

        {#if filteredGroups.length === 0}
          <p class="px-3 py-6 text-center text-[12px] text-[var(--text-tertiary)]">No log groups match the current filters.</p>
        {:else}
          {#each filteredGroups as group, idx (group.name)}
            {@const isSelected = checkedGroups.includes(group.name)}
            {@const matchCount = scanMatchMap.get(group.name) ?? 0}
            <div
              role="button"
              tabindex="0"
              data-group-row
              class="rc-group"
              class:is-selected={isSelected}
              onclick={(e) => handleGroupClick(e, group, idx)}
              onkeydown={(e) => {
                if (e.key === " ") {
                  e.preventDefault();
                  toggleGroup(group.name, idx);
                } else if (e.key === "Enter") {
                  e.preventDefault();
                  e.stopPropagation();
                  if (checkedGroups.length > 0) {
                    viewSelectedGroups();
                  } else {
                    selectGroup(group.name, groupSearch.trim());
                  }
                }
              }}
            >
              <span
                class="rc-checkbox"
                class:checked={isSelected}
                role="checkbox"
                aria-checked={isSelected}
                tabindex="-1"
                aria-label="Select {group.name}"
              >
                {#if isSelected}
                  <CheckIcon size={10} weight="bold" />
                {/if}
              </span>
              <span class="rc-group-activity" class:live={isRecent(group.lastEvent)} aria-hidden="true"></span>
              <span class="rc-group-main">
                <span class="rc-group-name">
                  <span class="rc-group-kind">{serviceLabel(groupServiceKey(group.name))}</span>
                  {groupDisplayName(group.name)}
                </span>
                <span class="rc-group-path">{group.name}</span>
              </span>
              <span class="rc-group-meter" aria-hidden="true">
                <span style:width="{Math.max(2, (group.eventCount / maxGroupEvents) * 100)}%"></span>
              </span>
              <span class="rc-group-stat">{group.eventCount.toLocaleString()}<small>events</small></span>
              <span class="rc-group-stat streams">{group.streamCount}<small>streams</small></span>
              {#if matchCount > 0}
                <span class="rc-badge font-mono text-[10.5px] font-medium text-[var(--accent-amber)] bg-[color-mix(in_srgb,var(--accent-amber)_15%,transparent)] border border-[color-mix(in_srgb,var(--accent-amber)_30%,transparent)] px-1.5 py-0.5 rounded">
                  {matchCount.toLocaleString()} {matchCount === 1 ? "match" : "matches"}
                </span>
              {/if}
              <span class="rc-group-stat age" title={group.lastEvent ? new Date(group.lastEvent).toLocaleString() : undefined}>
                {relativeAge(group.lastEvent)}
              </span>
            </div>
          {/each}
        {/if}
      </div>

      {#if checkedGroups.length > 0}
        <div class="rc-selectionbar" transition:fly={{ y: 16, duration: 220 }}>
          <span class="rc-selectionbar-dot"></span>
          <span class="rc-selectionbar-text">
            <strong>{checkedGroups.length}</strong> log {checkedGroups.length === 1 ? "group" : "groups"} selected
            <span class="rc-selectionbar-count">({selectedEventCount.toLocaleString()} events)</span>
          </span>
          <button type="button" class="rc-pill-btn ghost" onclick={clearGroupSelection}>
            Clear <kbd>Esc</kbd>
          </button>
          <button type="button" class="rc-pill-btn primary" onclick={viewSelectedGroups}>
            View logs ({checkedGroups.length}) <kbd>↵</kbd>
          </button>
        </div>
      {/if}
    {/if}
  </div>
{/if}

<style>
  .rc-mono {
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
  }

  /* ─── Toolbar ─── */
  .rc-tool,
  .rc-live {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 26px;
    padding: 0 9px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    background: transparent;
    color: var(--text-secondary);
    font-size: 11.5px;
    white-space: nowrap;
    cursor: pointer;
    transition: color 100ms ease, background 100ms ease, border-color 100ms ease;
  }

  .rc-tool:hover:not(:disabled),
  .rc-live:hover {
    color: var(--text-primary);
    border-color: var(--border-default);
    background: var(--bg-element-hover);
  }

  .rc-tool:active:not(:disabled),
  .rc-live:active {
    transform: scale(0.97);
  }

  .rc-tool.active {
    color: var(--text-primary);
    border-color: var(--border-default);
    background: var(--bg-element);
  }

  .rc-tool.danger:hover:not(:disabled) {
    color: var(--accent-red);
    border-color: color-mix(in srgb, var(--accent-red) 35%, transparent);
  }

  .rc-tool:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .rc-icon-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    flex-shrink: 0;
    border-radius: 6px;
    border: 1px solid transparent;
    color: var(--text-tertiary);
    transition: color 100ms ease, background 100ms ease, border-color 100ms ease;
  }

  .rc-icon-btn:hover {
    color: var(--text-primary);
    background: var(--bg-element-hover);
    border-color: var(--border-subtle);
  }

  /* Live tail toggle */
  .rc-live-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--text-tertiary);
    transition: background 150ms ease;
  }

  .rc-live.on {
    color: var(--text-primary);
    border-color: color-mix(in srgb, var(--accent-green) 40%, transparent);
    background: color-mix(in srgb, var(--accent-green) 8%, transparent);
  }

  .rc-live.on .rc-live-dot {
    background: var(--accent-green);
    animation: live-pulse 1.6s ease-out infinite;
  }

  .rc-live-rate {
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 10.5px;
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  @keyframes live-pulse {
    0% { box-shadow: 0 0 0 0 color-mix(in srgb, var(--accent-green) 55%, transparent); }
    100% { box-shadow: 0 0 0 6px transparent; }
  }

  /* ─── Query bar ─── */
  .rc-querybar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }

  .rc-search {
    display: flex;
    align-items: center;
    gap: 7px;
    flex: 1 1 260px;
    max-width: 420px;
    height: 28px;
    padding: 0 8px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    transition: border-color 100ms ease, background 100ms ease;
  }

  .rc-search:focus-within {
    border-color: var(--border-focus);
    background: var(--bg-element-hover);
  }

  .rc-search input {
    flex: 1;
    min-width: 0;
    background: transparent;
    border: none;
    outline: none;
    font-size: 12px;
    color: var(--text-primary);
  }

  .rc-search input::placeholder {
    color: var(--text-tertiary);
  }

  .rc-search-clear {
    display: flex;
    color: var(--text-tertiary);
  }

  .rc-search-clear:hover {
    color: var(--text-primary);
  }

  .rc-levels {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }

  .rc-level {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 24px;
    padding: 0 8px;
    border-radius: 8px;
    border: 1px solid var(--border-subtle);
    color: var(--text-secondary);
    font-size: 11.5px;
    transition: color 100ms ease, background 100ms ease, border-color 100ms ease, transform 100ms ease;
  }

  .rc-level:hover {
    color: var(--text-primary);
    border-color: var(--border-default);
  }

  .rc-level.active {
    color: var(--text-primary);
    border-color: var(--border-default);
    background: var(--bg-element);
  }

  .rc-level { --lvl: var(--text-secondary); }
  .rc-level-reveal { animation: levelIn 260ms var(--ease-snappy) both; animation-delay: calc(var(--i) * 35ms); }
  @keyframes levelIn { from { opacity: 0; transform: translateY(3px) scale(0.97); } }
  @media (prefers-reduced-motion: reduce) { .rc-level-reveal { animation: none; } }
  .rc-level[data-level="ERROR"] { --lvl: var(--accent-red); }
  .rc-level[data-level="WARN"] { --lvl: var(--accent-amber); }
  .rc-level[data-level="INFO"] { --lvl: var(--accent-green); }
  .rc-level:active { transform: scale(0.96); }
  .rc-level[data-level].active {
    color: var(--lvl);
    background: color-mix(in srgb, var(--lvl) 10%, transparent);
    border-color: color-mix(in srgb, var(--lvl) 45%, transparent);
  }

  .rc-level-count {
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 10.5px;
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .rc-filters {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: 10px;
    padding: 10px 12px;
    border-radius: 8px;
    border: 1px solid var(--border-subtle);
    flex-shrink: 0;
    animation: drop-in 180ms var(--ease-snappy) both;
  }

  @keyframes drop-in {
    from { opacity: 0; transform: translateY(-3px); }
    to { opacity: 1; transform: translateY(0); }
  }

  .rc-field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 240px;
  }

  .rc-field span {
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  .rc-field input {
    height: 26px;
    padding: 0 8px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    background: transparent;
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 11.5px;
    color: var(--text-primary);
    outline: none;
  }

  .rc-field input:focus {
    border-color: var(--border-focus);
  }

  .rc-error {
    flex-shrink: 0;
    padding: 8px 12px;
    border-radius: 6px;
    border: 1px solid color-mix(in srgb, var(--accent-red) 30%, transparent);
    color: var(--accent-red);
    font-size: 12px;
  }

  /* ─── Stream panel ─── */
  .rc-panel {
    overflow: hidden;
  }

  .rc-ribbon {
    position: relative;
    display: flex;
    align-items: flex-end;
    gap: 1px;
    height: 34px;
    padding: 6px 6px 0;
    border-bottom: 1px solid var(--border-subtle);
    flex-shrink: 0;
  }

  .rc-ribbon-bar {
    position: relative;
    flex: 1;
    height: 18px;
    display: flex;
    align-items: flex-end;
    cursor: pointer;
  }

  .rc-ribbon-bar:disabled {
    cursor: default;
  }

  .rc-ribbon-fill {
    width: 100%;
    border-radius: 1.5px 1.5px 0 0;
    background: var(--text-tertiary);
    opacity: 0.45;
    transition: height 260ms var(--ease-snappy), opacity 100ms ease, background 100ms ease;
  }

  .rc-ribbon-err {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    border-radius: 1.5px 1.5px 0 0;
    background: var(--accent-red);
    opacity: 0.85;
  }

  .rc-ribbon-bar.hot .rc-ribbon-fill {
    opacity: 1;
    background: var(--text-primary);
  }

  .rc-ribbon-label {
    position: absolute;
    right: 10px;
    top: 2px;
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 10px;
    color: var(--text-tertiary);
    pointer-events: none;
    font-variant-numeric: tabular-nums;
    padding-left: 6px;
    background: var(--bg-stage);
  }

  /* ─── Detail panel ─── */
  .rc-detail {
    display: flex;
    flex-direction: column;
    height: 100%;
    border-left: 1px solid var(--border-subtle);
  }

  .rc-detail-head {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 40px;
    padding: 0 8px 0 14px;
    border-bottom: 1px solid var(--border-subtle);
    flex-shrink: 0;
  }

  .rc-badge {
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.04em;
    padding: 1px 6px;
    border-radius: 4px;
    border: 1px solid var(--border-default);
    color: var(--text-secondary);
  }

  .rc-badge[data-level="ERROR"] {
    color: var(--accent-red);
    border-color: color-mix(in srgb, var(--accent-red) 35%, transparent);
    background: color-mix(in srgb, var(--accent-red) 8%, transparent);
  }

  .rc-badge[data-level="WARN"] {
    color: var(--accent-amber);
    border-color: color-mix(in srgb, var(--accent-amber) 35%, transparent);
    background: color-mix(in srgb, var(--accent-amber) 8%, transparent);
  }

  .rc-badge[data-level="INFO"] {
    color: var(--accent-green);
    border-color: color-mix(in srgb, var(--accent-green) 35%, transparent);
    background: color-mix(in srgb, var(--accent-green) 8%, transparent);
  }

  .rc-label {
    margin-bottom: 8px;
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  .rc-code {
    padding: 10px 12px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    background: var(--code-bg, var(--bg-element-hover));
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 12px;
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-all;
    color: var(--text-primary);
  }

  .rc-subpanel {
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
  }

  .rc-kv {
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
  }

  .rc-kv > div {
    display: flex;
    align-items: baseline;
    gap: 14px;
    padding: 7px 12px;
  }

  .rc-kv > div + div {
    border-top: 1px solid var(--border-subtle);
  }

  .rc-kv dt {
    width: 64px;
    flex-shrink: 0;
    font-size: 10px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  .rc-kv dd {
    min-width: 0;
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 11.5px;
    color: var(--text-secondary);
    word-break: break-all;
  }

  .rc-link {
    color: var(--text-secondary);
    text-align: left;
    text-decoration: underline;
    text-decoration-color: var(--border-default);
    text-underline-offset: 3px;
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    transition: color 100ms ease, text-decoration-color 100ms ease;
  }

  .rc-link:hover {
    color: var(--text-primary);
    text-decoration-color: var(--text-secondary);
  }

  /* ─── Groups list ─── */
  .rc-groups {
    position: relative;
    padding: 2px 0;
    scrollbar-width: thin;
  }

  .rc-group-pill {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    border-radius: 7px;
    background: var(--bg-element-hover);
    border: 1px solid var(--border-subtle);
    pointer-events: none;
    will-change: transform, height, opacity;
    transition:
      transform 130ms var(--ease-snappy),
      height 120ms ease,
      opacity 80ms ease;
    z-index: 0;
  }

  .rc-groups.has-selection {
    padding-bottom: 56px;
  }

  .rc-group {
    position: relative;
    z-index: 1;
    display: grid;
    grid-template-columns: 15px 6px minmax(0, 1fr) 120px 76px 64px 44px;
    align-items: center;
    gap: 12px;
    width: 100%;
    padding: 8px 14px 8px 10px;
    text-align: left;
    border-radius: 7px;
    border: 1px solid transparent;
    cursor: pointer;
    user-select: none;
    transition: transform 80ms ease, background 120ms ease, border-color 120ms ease;
  }

  .rc-group.is-selected {
    background: color-mix(in srgb, var(--accent-green) 7%, var(--bg-element));
    border-color: color-mix(in srgb, var(--accent-green) 32%, var(--border-default));
  }

  .rc-group.is-selected::before {
    content: "";
    position: absolute;
    left: 0;
    top: 6px;
    bottom: 6px;
    width: 2.5px;
    border-radius: 2px 0 0 2px;
    background: var(--accent-green);
    opacity: 0.95;
  }

  .rc-checkbox {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 15px;
    height: 15px;
    border-radius: 4px;
    border: 1px solid var(--border-default);
    background: transparent;
    color: var(--bg-stage);
    cursor: pointer;
    flex-shrink: 0;
    transition: border-color 100ms ease, background 100ms ease, opacity 100ms ease;
    opacity: 0.35;
  }

  .rc-group:hover .rc-checkbox,
  .rc-checkbox.checked,
  .rc-checkbox:focus-visible {
    opacity: 1;
  }

  .rc-checkbox.checked {
    background: var(--text-primary);
    border-color: var(--text-primary);
    color: var(--bg-stage);
  }

  .rc-checkbox-spacer {
    width: 15px;
    height: 15px;
    flex-shrink: 0;
  }

  /* ─── Selection bar (inline with settings savebar style) ─── */
  .rc-selectionbar {
    position: absolute;
    bottom: 14px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 20;
    display: inline-flex;
    align-items: center;
    gap: 10px;
    height: 42px;
    padding: 0 8px 0 14px;
    border-radius: 9px;
    border: 1px solid var(--border-default);
    background: var(--bg-element);
    box-shadow: 0 4px 20px rgba(0, 0, 0, 0.24), 0 1px 4px rgba(0, 0, 0, 0.12);
    backdrop-filter: blur(8px);
  }

  .rc-selectionbar-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent-green);
    animation: breathe 1.6s ease-in-out infinite;
    flex-shrink: 0;
  }

  @keyframes breathe {
    50% {
      opacity: 0.35;
    }
  }

  .rc-selectionbar-text {
    font-size: 12px;
    color: var(--text-primary);
    display: flex;
    align-items: baseline;
    gap: 6px;
    margin-right: 4px;
    white-space: nowrap;
  }

  .rc-selectionbar-count {
    font-size: 11px;
    color: var(--text-tertiary);
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
  }

  .rc-pill-btn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    height: 28px;
    padding: 0 12px;
    border-radius: 7px;
    font-size: 11.5px;
    font-weight: 500;
    cursor: pointer;
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 100ms ease;
    white-space: nowrap;
  }

  .rc-pill-btn:active {
    transform: scale(0.97);
  }

  .rc-pill-btn.ghost {
    color: var(--text-secondary);
    border: 1px solid var(--border-subtle);
    background: transparent;
  }

  .rc-pill-btn.ghost:hover {
    color: var(--text-primary);
    border-color: var(--border-default);
    background: var(--bg-element-hover);
  }

  .rc-pill-btn.primary {
    color: var(--bg-stage);
    background: var(--text-primary);
    border: 1px solid var(--text-primary);
  }

  .rc-pill-btn.primary:hover {
    opacity: 0.9;
  }

  .rc-selectionbar kbd {
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 10px;
    opacity: 0.6;
    margin-left: 2px;
  }

  /* ─── Selection chips in event viewer ─── */
  .rc-selection-chips {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    padding: 2px 2px 4px;
  }

  .rc-selection-label {
    font-size: 11px;
    color: var(--text-tertiary);
    font-weight: 500;
    margin-right: 2px;
  }

  .rc-group-chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 24px;
    padding: 0 6px 0 9px;
    border-radius: 6px;
    border: 1px solid color-mix(in srgb, var(--accent-green) 40%, transparent);
    background: color-mix(in srgb, var(--accent-green) 10%, transparent);
    font-size: 11px;
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    color: var(--text-primary);
  }

  .rc-chip-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--accent-green);
    flex-shrink: 0;
  }

  .rc-chip-text {
    max-width: 160px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .rc-chip-remove {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 15px;
    height: 15px;
    border-radius: 3px;
    color: var(--text-tertiary);
    cursor: pointer;
    transition: color 100ms ease, background 100ms ease;
  }

  .rc-chip-remove:hover {
    color: var(--accent-red);
    background: color-mix(in srgb, var(--accent-red) 15%, transparent);
  }

  .rc-chip-add {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: 24px;
    padding: 0 8px;
    border-radius: 6px;
    border: 1px dashed var(--border-default);
    background: transparent;
    color: var(--text-tertiary);
    font-size: 11px;
    cursor: pointer;
    transition: color 100ms ease, border-color 100ms ease;
  }

  .rc-chip-add:hover {
    color: var(--text-primary);
    border-color: var(--text-secondary);
  }

  .rc-group:active {
    transform: scale(0.995);
  }

  .rc-group:focus-visible {
    outline: none;
    box-shadow: 0 0 0 1.5px var(--border-focus);
  }

  .rc-group-activity {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--border-default);
  }

  .rc-group-activity.live {
    background: var(--accent-green);
    animation: live-pulse 1.6s ease-out infinite;
  }

  .rc-group-main {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }

  .rc-group-name {
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font-size: 12.5px;
    font-weight: 500;
    color: var(--text-primary);
  }

  .rc-group-kind {
    flex-shrink: 0;
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 10px;
    font-weight: 500;
    color: var(--text-tertiary);
  }

  .rc-group-path {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 10.5px;
    color: var(--text-tertiary);
  }

  .rc-group-meter {
    height: 3px;
    border-radius: 9999px;
    background: var(--border-subtle);
    overflow: hidden;
  }

  .rc-group-meter > span {
    display: block;
    height: 100%;
    border-radius: inherit;
    background: var(--text-tertiary);
    transition: width 300ms var(--ease-snappy), background 100ms ease;
  }

  .rc-group:hover .rc-group-meter > span {
    background: var(--text-secondary);
  }

  .rc-group-stat {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 12px;
    color: var(--text-primary);
    font-variant-numeric: tabular-nums;
    line-height: 1.2;
  }

  .rc-group-stat small {
    font-family: var(--font-ui-sans, sans-serif);
    font-size: 10px;
    color: var(--text-tertiary);
  }

  .rc-group-stat.age {
    color: var(--text-tertiary);
  }

  .rc-groups-divider {
    height: 1px;
    margin: 4px 12px;
    background: var(--border-subtle);
  }

  @media (max-width: 720px) {
    .rc-group {
      grid-template-columns: 10px minmax(0, 1fr) 64px 44px;
    }
    .rc-group-meter,
    .rc-group-stat.streams {
      display: none;
    }
  }
</style>
