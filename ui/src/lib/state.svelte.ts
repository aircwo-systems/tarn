import { AccountArchivedError, fetchOverview, fetchUserServices, pruneOldLogs, saveUserServices, setApiAccount } from "$lib/api";
import type { InfraProbe, OverviewResponse, UserService } from "$lib/types";

export type InfraProbeKind =
  | "docker"
  | "postgresql"
  | "redis"
  | "mysql"
  | "mongodb"
  | "http";

const DEFAULT_INFRA_ENABLED_KINDS: InfraProbeKind[] = [
  "docker",
  "postgresql",
  "redis",
  "mysql",
  "mongodb",
  "http",
];

/** Shape of services saved in localStorage before they moved to the backend. */
interface LegacyFrontendTarget {
  name: string;
  host: string;
  port: number;
}

let data = $state<OverviewResponse | null>(null);
let loading = $state(true);
let error = $state("");
let lastRefresh = $state("");

let pollHandle: ReturnType<typeof setInterval> | null = null;
let refreshPromise: Promise<void> | null = null;
let refreshQueued = false;
let refreshController: AbortController | null = null;
let accountGeneration = 0;

const SETTINGS_COOKIE = "tarn-ui-settings";
const INFRA_SETTINGS_KEY = "tarn-infra-settings";
const PROJECT_SETTINGS_KEY = "tarn-project-settings";
const ACCOUNTS_KEY = "tarn-accounts";
const DEFAULT_POLLING_INTERVAL_SECONDS = 5;
const MIN_POLLING_INTERVAL_SECONDS = 1;
const MAX_POLLING_INTERVAL_SECONDS = 120;
const DEFAULT_PERSISTENCE_ENABLED = false;
const MAX_SCHEMA_SOURCE_LENGTH = 1024;
const DEFAULT_LOG_RETENTION_MINUTES = 30;
const MIN_LOG_RETENTION_MINUTES = 1;
const MAX_LOG_RETENTION_MINUTES = 1440; // 24 hours

export type ThemeMode = "system" | "light" | "dark";
export type CollapsedSidebarMode = "icons" | "hidden";

let pollingIntervalSeconds = $state(DEFAULT_POLLING_INTERVAL_SECONDS);
let themeMode = $state<ThemeMode>("system");
let collapsedSidebarMode = $state<CollapsedSidebarMode>("icons");
let formatDatadogLogs = $state(true);
let resolvedTheme = $state<"light" | "dark">("dark");
let persistenceEnabled = $state(DEFAULT_PERSISTENCE_ENABLED);
let dashboardTagFilter = $state("");
let settingsInitialized = false;
let schemaSourceDir = $state("");
let logRetentionMinutes = $state(DEFAULT_LOG_RETENTION_MINUTES);

let infraEnabledKinds = $state<InfraProbeKind[]>([...DEFAULT_INFRA_ENABLED_KINDS]);
let userServices = $state<UserService[]>([]);
let userServicesLoaded = $state(false);
let servicesLoadController: AbortController | null = null;
let legacyFrontendTargets: LegacyFrontendTarget[] = [];

export interface KnownAccount {
  id: string;
  label: string;
}

const DEFAULT_ACCOUNT: KnownAccount = { id: "000000000000", label: "Default" };
let activeAccountId = $state("000000000000");
let knownAccounts = $state<KnownAccount[]>([{ ...DEFAULT_ACCOUNT }]);

let systemThemeMediaQuery: MediaQueryList | null = null;
let systemThemeListener: ((event: MediaQueryListEvent) => void) | null = null;

export function getDashboard() {
  return {
    get data() {
      return data;
    },
    get loading() {
      return loading;
    },
    get error() {
      return error;
    },
    get lastRefresh() {
      return lastRefresh;
    },
  };
}

export function getUISettings() {
  return {
    get pollingIntervalSeconds() {
      return pollingIntervalSeconds;
    },
    get themeMode() {
      return themeMode;
    },
    get collapsedSidebarMode() {
      return collapsedSidebarMode;
    },
    get formatDatadogLogs() {
      return formatDatadogLogs;
    },
    get resolvedTheme() {
      return resolvedTheme;
    },
    get persistenceEnabled() {
      return persistenceEnabled;
    },
    get schemaSourceDir() {
      return schemaSourceDir;
    },
    get logRetentionMinutes() {
      return logRetentionMinutes;
    },
  };
}

export function getDashboardFilters() {
  return {
    get tagFilter() {
      return dashboardTagFilter;
    },
  };
}

export function refresh(): Promise<void> {
  if (refreshPromise) {
    refreshQueued = true;
    return refreshPromise;
  }

  refreshController = new AbortController();
  refreshPromise = refreshDashboard(accountGeneration, refreshController.signal);
  return refreshPromise;
}

async function refreshDashboard(generation: number, signal: AbortSignal) {
  try {
    do {
      refreshQueued = false;
      if (!loading) error = "";

      try {
        const overview = await fetchOverview(signal);
        if (generation !== accountGeneration) return;
        data = overview;
        lastRefresh = new Date().toLocaleTimeString();
        error = "";
      } catch (err) {
        if (generation !== accountGeneration) return;
        if (err instanceof AccountArchivedError && activeAccountId !== DEFAULT_ACCOUNT.id) {
          // The selected account was archived (here or by another client):
          // fall back to the default account rather than showing an error.
          switchAccount(DEFAULT_ACCOUNT.id);
          return;
        }
        error = err instanceof Error ? err.message : "Failed to load dashboard data";
      } finally {
        if (generation === accountGeneration) loading = false;
      }
    } while (refreshQueued);
  } finally {
    // A superseded load must not clear the newer account's in-flight refresh.
    if (generation === accountGeneration) {
      refreshPromise = null;
      refreshController = null;
    }
  }
}

export function startPolling() {
  initUISettings();
  refresh();
  loadUserServices();
  schedulePolling();
}

export function stopPolling() {
  if (pollHandle) {
    clearInterval(pollHandle);
    pollHandle = null;
  }
}

export function initUISettings() {
  if (settingsInitialized) return;
  settingsInitialized = true;

  const settings = readSettingsFromCookie();
  pollingIntervalSeconds = normalizePollingInterval(settings.pollingIntervalSeconds);
  themeMode = normalizeThemeMode(settings.themeMode);
  collapsedSidebarMode = normalizeCollapsedSidebarMode(settings.collapsedSidebarMode);
  formatDatadogLogs = settings.formatDatadogLogs !== false;
  persistenceEnabled = normalizePersistenceEnabled(settings.persistenceEnabled);
  logRetentionMinutes = normalizeLogRetention(settings.logRetentionMinutes);
  applyTheme(themeMode);
  initInfraSettings();
  initProjectSettings();
  initAccountSettings();
}

export function getInfraSettings() {
  return {
    get enabledKinds() {
      return infraEnabledKinds;
    },
    /** services registered on the backend; [] until the first load completes */
    get userServices() {
      return userServices;
    },
    get userServicesLoaded() {
      return userServicesLoaded;
    },
  };
}

export function getAccountSettings() {
  return {
    get activeAccountId() {
      return activeAccountId;
    },
    get knownAccounts() {
      return knownAccounts;
    },
  };
}

export function switchAccount(id: string) {
  if (id === activeAccountId) return;
  activeAccountId = id;
  setApiAccount(id);
  accountGeneration += 1;
  refreshController?.abort();
  refreshController = null;
  refreshPromise = null;
  refreshQueued = false;
  data = null;
  error = "";
  lastRefresh = "";
  loading = true;
  userServices = [];
  userServicesLoaded = false;
  void loadUserServices();
  persistAccountSettings();
  refresh();
}

export function addKnownAccount(id: string, label: string): boolean {
  const trimmed = id.trim();
  if (!/^\d{12}$/.test(trimmed)) return false;
  if (knownAccounts.some((a) => a.id === trimmed)) return false;
  knownAccounts = [...knownAccounts, { id: trimmed, label: label.trim() || trimmed }];
  persistAccountSettings();
  return true;
}

/** Saves a browser-local label, including for an account discovered by the server. */
export function setAccountLabel(id: string, label: string) {
  if (!knownAccounts.some((account) => account.id === id)) {
    addKnownAccount(id, label);
    return;
  }
  const normalized = label.trim() || (id === DEFAULT_ACCOUNT.id ? DEFAULT_ACCOUNT.label : id);
  knownAccounts = knownAccounts.map((account) =>
    account.id === id ? { ...account, label: normalized } : account,
  );
  persistAccountSettings();
}

export function removeKnownAccount(id: string) {
  if (id === "000000000000") return;
  knownAccounts = knownAccounts.filter((a) => a.id !== id);
  if (activeAccountId === id) {
    switchAccount(DEFAULT_ACCOUNT.id);
  }
  persistAccountSettings();
}

export function setInfraEnabledKinds(kinds: InfraProbeKind[]) {
  infraEnabledKinds = kinds;
  persistInfraSettings();
}

/** Replaces the registered services on the backend. Throws with the server's validation message. */
export async function setUserServices(services: UserService[]): Promise<void> {
  const accountId = activeAccountId;
  const generation = accountGeneration;
  const saved = await saveUserServices({ services, accountId });
  if (generation !== accountGeneration) return;
  userServices = saved;
  await refresh();
}

// Services the user registered are always shown; kind toggles only filter built-in probes.
export function getVisibleInfra(backendInfra: InfraProbe[]): InfraProbe[] {
  return backendInfra.filter(
    (p) => p.source === "user" || (infraEnabledKinds as string[]).includes(p.kind),
  );
}

export function setPollingIntervalSeconds(next: number) {
  const normalized = normalizePollingInterval(next);
  if (normalized === pollingIntervalSeconds) return;

  pollingIntervalSeconds = normalized;
  persistSettingsToCookie();
  restartPollingIfActive();
}

export function setThemeMode(next: ThemeMode) {
  const normalized = normalizeThemeMode(next);
  if (normalized === themeMode) return;

  themeMode = normalized;
  applyTheme(themeMode);
  persistSettingsToCookie();
}

export function setCollapsedSidebarMode(next: CollapsedSidebarMode) {
  const normalized = normalizeCollapsedSidebarMode(next);
  if (normalized === collapsedSidebarMode) return;

  collapsedSidebarMode = normalized;
  persistSettingsToCookie();
}

export function setFormatDatadogLogs(next: boolean) {
  if (next === formatDatadogLogs) return;
  formatDatadogLogs = next;
  persistSettingsToCookie();
}

export function setPersistenceEnabled(next: boolean) {
  const normalized = normalizePersistenceEnabled(next);
  if (normalized === persistenceEnabled) return;

  persistenceEnabled = normalized;
  persistSettingsToCookie();
}

export function setSchemaSourceDir(next: string) {
  const normalized = sanitizeSchemaSourceDir(next);
  if (normalized === schemaSourceDir) return;

  schemaSourceDir = normalized;
  persistProjectSettings();
}

export function setLogRetentionMinutes(next: number) {
  const normalized = normalizeLogRetention(next);
  if (normalized === logRetentionMinutes) return;

  logRetentionMinutes = normalized;
  persistSettingsToCookie();
}

export function setDashboardTagFilter(next: string) {
  dashboardTagFilter = next.trim();
}

export { matchesTagFilter } from "$lib/filter-utils";

function schedulePolling() {
  stopPolling();
  pollHandle = setInterval(() => {
    if (!document.hidden) {
      refresh();
      pruneLogsIfNeeded();
    }
  }, pollingIntervalSeconds * 1000);
}

let lastPruneTime = 0;
const PRUNE_INTERVAL_MS = 60_000; // Only call prune endpoint once per minute

function pruneLogsIfNeeded() {
  const now = Date.now();
  if (now - lastPruneTime < PRUNE_INTERVAL_MS) return;
  lastPruneTime = now;
  pruneOldLogs(logRetentionMinutes).catch(() => {
    // Silently ignore prune failures
  });
}

function restartPollingIfActive() {
  if (!pollHandle) return;
  schedulePolling();
}

function applyTheme(mode: ThemeMode) {
  if (typeof window === "undefined" || typeof document === "undefined") return;

  const root = document.documentElement;
  const resolved = resolveThemeMode(mode);

  resolvedTheme = resolved;
  root.classList.toggle("dark", resolved === "dark");
  root.classList.toggle("light", resolved === "light");

  if (mode === "system") {
    ensureSystemThemeListener();
  } else {
    detachSystemThemeListener();
  }
}

function resolveThemeMode(mode: ThemeMode): "light" | "dark" {
  if (mode === "light" || mode === "dark") return mode;
  if (typeof window === "undefined") return "dark";
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function ensureSystemThemeListener() {
  if (typeof window === "undefined") return;
  if (!systemThemeMediaQuery) {
    systemThemeMediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
  }
  if (systemThemeListener) return;

  systemThemeListener = () => {
    if (themeMode !== "system") return;
    applyTheme("system");
  };
  systemThemeMediaQuery.addEventListener("change", systemThemeListener);
}

function detachSystemThemeListener() {
  if (!systemThemeMediaQuery || !systemThemeListener) return;
  systemThemeMediaQuery.removeEventListener("change", systemThemeListener);
  systemThemeListener = null;
}

function persistSettingsToCookie() {
  if (typeof document === "undefined") return;

  const payload = encodeURIComponent(
    JSON.stringify({
      pollingIntervalSeconds,
      themeMode,
      collapsedSidebarMode,
      formatDatadogLogs,
      persistenceEnabled,
      logRetentionMinutes,
    }),
  );
  document.cookie = `${SETTINGS_COOKIE}=${payload}; Path=/; Max-Age=31536000; SameSite=Lax`;
}

function readSettingsFromCookie(): {
  pollingIntervalSeconds?: number;
  themeMode?: ThemeMode;
  collapsedSidebarMode?: CollapsedSidebarMode;
  formatDatadogLogs?: boolean;
  persistenceEnabled?: boolean;
  logRetentionMinutes?: number;
} {
  if (typeof document === "undefined") return {};

  const raw = document.cookie.split("; ").find((entry) => entry.startsWith(`${SETTINGS_COOKIE}=`));

  if (!raw) return {};

  const encoded = raw.slice(SETTINGS_COOKIE.length + 1);
  try {
    const parsed = JSON.parse(decodeURIComponent(encoded)) as {
      pollingIntervalSeconds?: number;
      themeMode?: ThemeMode;
      collapsedSidebarMode?: CollapsedSidebarMode;
      formatDatadogLogs?: boolean;
      persistenceEnabled?: boolean;
      logRetentionMinutes?: number;
    };
    return parsed ?? {};
  } catch {
    return {};
  }
}

function normalizePollingInterval(value: unknown): number {
  const numeric = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(numeric)) return DEFAULT_POLLING_INTERVAL_SECONDS;
  const rounded = Math.round(numeric);
  return Math.min(MAX_POLLING_INTERVAL_SECONDS, Math.max(MIN_POLLING_INTERVAL_SECONDS, rounded));
}

function normalizeThemeMode(value: unknown): ThemeMode {
  if (value === "light" || value === "dark" || value === "system") {
    return value;
  }
  return "system";
}

function normalizeCollapsedSidebarMode(value: unknown): CollapsedSidebarMode {
  return value === "hidden" ? "hidden" : "icons";
}

function normalizePersistenceEnabled(value: unknown): boolean {
  return value === true;
}

function normalizeLogRetention(value: unknown): number {
  const numeric = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(numeric)) return DEFAULT_LOG_RETENTION_MINUTES;
  const rounded = Math.round(numeric);
  return Math.min(MAX_LOG_RETENTION_MINUTES, Math.max(MIN_LOG_RETENTION_MINUTES, rounded));
}

export function sanitizeSchemaSourceDir(value: unknown): string {
  if (typeof value !== "string") return "";

  let normalized = value.replace(/[\u0000-\u001F\u007F]/g, "").trim();
  if (
    normalized.length >= 2 &&
    ((normalized.startsWith('"') && normalized.endsWith('"')) ||
      (normalized.startsWith("'") && normalized.endsWith("'")))
  ) {
    normalized = normalized.slice(1, -1).trim();
  }
  if (normalized.length > MAX_SCHEMA_SOURCE_LENGTH) {
    normalized = normalized.slice(0, MAX_SCHEMA_SOURCE_LENGTH);
  }
  return normalized;
}

function initInfraSettings() {
  if (typeof localStorage === "undefined") return;
  try {
    const raw = localStorage.getItem(INFRA_SETTINGS_KEY);
    if (!raw) return;
    const parsed = JSON.parse(raw) as { enabledKinds?: unknown[]; frontendTargets?: unknown[] };
    if (Array.isArray(parsed.enabledKinds)) {
      infraEnabledKinds = parsed.enabledKinds.filter(isValidInfraKind);
    }
    if (Array.isArray(parsed.frontendTargets)) {
      legacyFrontendTargets = parsed.frontendTargets.filter(isValidFrontendTarget);
    }
  } catch {
    // ignore corrupt data
  }
}

function persistInfraSettings() {
  if (typeof localStorage === "undefined") return;
  localStorage.setItem(
    INFRA_SETTINGS_KEY,
    JSON.stringify({ enabledKinds: infraEnabledKinds }),
  );
}

function initProjectSettings() {
  if (typeof localStorage === "undefined") return;
  try {
    const raw = localStorage.getItem(PROJECT_SETTINGS_KEY);
    if (!raw) return;
    const parsed = JSON.parse(raw) as { schemaSourceDir?: unknown };
    schemaSourceDir = sanitizeSchemaSourceDir(parsed.schemaSourceDir);
  } catch {
    // ignore corrupt data
  }
}

function persistProjectSettings() {
  if (typeof localStorage === "undefined") return;
  localStorage.setItem(PROJECT_SETTINGS_KEY, JSON.stringify({ schemaSourceDir }));
}

async function loadUserServices() {
  if (typeof window === "undefined") return;
  servicesLoadController?.abort();
  const controller = new AbortController();
  servicesLoadController = controller;
  const accountId = activeAccountId;
  const generation = accountGeneration;
  try {
    // Browser-era registrations also belong to the default account.
    if (legacyFrontendTargets.length > 0) {
      const existing = await fetchUserServices({ signal: controller.signal, accountId: DEFAULT_ACCOUNT.id });
      const known = new Set(existing.map((svc) => svc.url));
      const migrated: UserService[] = legacyFrontendTargets
        .map((target): UserService => ({ name: target.name, url: `http://${target.host}:${target.port}`, scope: "account", accountId: DEFAULT_ACCOUNT.id }))
        .filter((service) => !known.has(service.url));
      if (controller.signal.aborted) return;
      if (migrated.length > 0) await saveUserServices({ services: [...existing, ...migrated], accountId: DEFAULT_ACCOUNT.id });
      legacyFrontendTargets = [];
      persistInfraSettings();
    }
    const loaded = await fetchUserServices({ signal: controller.signal, accountId });
    if (generation === accountGeneration && !controller.signal.aborted) userServices = loaded;
  } catch {
    // Older servers have no services endpoint; keep the list empty.
  } finally {
    if (generation === accountGeneration && servicesLoadController === controller) {
      userServicesLoaded = true;
      servicesLoadController = null;
    }
  }
}

function initAccountSettings() {
  if (typeof localStorage === "undefined") return;
  try {
    const raw = localStorage.getItem(ACCOUNTS_KEY);
    if (!raw) return;
    const parsed = JSON.parse(raw) as { activeAccountId?: string; knownAccounts?: unknown[] };
    if (Array.isArray(parsed.knownAccounts)) {
      const valid = parsed.knownAccounts.filter(isValidKnownAccount);
      if (valid.length > 0) {
        if (!valid.some((a) => a.id === "000000000000")) {
          valid.unshift({ ...DEFAULT_ACCOUNT });
        }
        knownAccounts = valid;
      }
    }
    if (
      typeof parsed.activeAccountId === "string" &&
      /^\d{12}$/.test(parsed.activeAccountId)
    ) {
      // Server-discovered accounts can be selected without being saved locally.
      activeAccountId = parsed.activeAccountId;
      setApiAccount(activeAccountId);
    }
  } catch {
    // ignore corrupt data
  }
}

function persistAccountSettings() {
  if (typeof localStorage === "undefined") return;
  localStorage.setItem(
    ACCOUNTS_KEY,
    JSON.stringify({ activeAccountId, knownAccounts }),
  );
}

function isValidKnownAccount(v: unknown): v is KnownAccount {
  return (
    typeof v === "object" &&
    v !== null &&
    "id" in v &&
    typeof (v as KnownAccount).id === "string" &&
    "label" in v &&
    typeof (v as KnownAccount).label === "string"
  );
}

const VALID_INFRA_KINDS = new Set<InfraProbeKind>([
	"docker",
	"postgresql",
	"redis",
	"mysql",
	"mongodb",
	"http",
]);

function isValidInfraKind(v: unknown): v is InfraProbeKind {
  return typeof v === "string" && VALID_INFRA_KINDS.has(v as InfraProbeKind);
}

function isValidFrontendTarget(v: unknown): v is LegacyFrontendTarget {
  return (
    typeof v === "object" &&
    v !== null &&
    typeof (v as LegacyFrontendTarget).name === "string" &&
    typeof (v as LegacyFrontendTarget).host === "string" &&
    typeof (v as LegacyFrontendTarget).port === "number"
  );
}
