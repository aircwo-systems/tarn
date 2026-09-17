import { fetchOverview, fetchUserServices, pruneOldLogs, saveUserServices, setApiAccount } from "$lib/api";
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

let pollingIntervalSeconds = $state(DEFAULT_POLLING_INTERVAL_SECONDS);
let themeMode = $state<ThemeMode>("system");
let resolvedTheme = $state<"light" | "dark">("dark");
let persistenceEnabled = $state(DEFAULT_PERSISTENCE_ENABLED);
let dashboardTagFilter = $state("");
let settingsInitialized = false;
let schemaSourceDir = $state("");
let logRetentionMinutes = $state(DEFAULT_LOG_RETENTION_MINUTES);

let infraEnabledKinds = $state<InfraProbeKind[]>([...DEFAULT_INFRA_ENABLED_KINDS]);
let userServices = $state<UserService[]>([]);
let userServicesLoaded = $state(false);
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

  refreshPromise = refreshDashboard();
  return refreshPromise;
}

async function refreshDashboard() {
  try {
    do {
      refreshQueued = false;
      if (!loading) error = "";

      try {
        data = await fetchOverview();
        lastRefresh = new Date().toLocaleTimeString();
        error = "";
      } catch (err) {
        error = err instanceof Error ? err.message : "Failed to load dashboard data";
      } finally {
        loading = false;
      }
    } while (refreshQueued);
  } finally {
    refreshPromise = null;
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

export function removeKnownAccount(id: string) {
  if (id === "000000000000") return;
  knownAccounts = knownAccounts.filter((a) => a.id !== id);
  if (activeAccountId === id) {
    activeAccountId = "000000000000";
    setApiAccount("000000000000");
    refresh();
  }
  persistAccountSettings();
}

export function setInfraEnabledKinds(kinds: InfraProbeKind[]) {
  infraEnabledKinds = kinds;
  persistInfraSettings();
}

/** Replaces the registered services on the backend. Throws with the server's validation message. */
export async function setUserServices(services: UserService[]): Promise<void> {
  userServices = await saveUserServices(services);
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

export function matchesTagFilter(tags: Record<string, string> | undefined, query: string): boolean {
  const normalizedTokens = query
    .trim()
    .toLowerCase()
    .split(/\s+/)
    .map((token) => token.trim())
    .filter(Boolean);
  if (normalizedTokens.length === 0) return true;
  if (!tags || Object.keys(tags).length === 0) return false;

  return normalizedTokens.every((token) => matchesSingleTagFilter(tags, token));
}

function matchesSingleTagFilter(
  tags: Record<string, string>,
  normalized: string,
): boolean {
  if (!normalized) return true;

  const pairSeparator = normalized.includes(":") ? ":" : normalized.includes("=") ? "=" : "";
  if (pairSeparator) {
    const [rawKey, ...rest] = normalized.split(pairSeparator);
    const keyQuery = rawKey.trim();
    const valueQuery = rest.join(pairSeparator).trim();

    for (const [key, value] of Object.entries(tags)) {
      const keyLower = key.toLowerCase();
      const valueLower = value.toLowerCase();
      if (keyQuery && !keyLower.includes(keyQuery)) {
        continue;
      }
      if (valueQuery && !valueLower.includes(valueQuery)) {
        continue;
      }
      return true;
    }
    return false;
  }

  for (const [key, value] of Object.entries(tags)) {
    const keyLower = key.toLowerCase();
    const valueLower = value.toLowerCase();
    if (
      keyLower.includes(normalized) ||
      valueLower.includes(normalized) ||
      `${keyLower}:${valueLower}`.includes(normalized)
    ) {
      return true;
    }
  }
  return false;
}

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
      persistenceEnabled,
      logRetentionMinutes,
    }),
  );
  document.cookie = `${SETTINGS_COOKIE}=${payload}; Path=/; Max-Age=31536000; SameSite=Lax`;
}

function readSettingsFromCookie(): {
  pollingIntervalSeconds?: number;
  themeMode?: ThemeMode;
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
  try {
    userServices = await fetchUserServices();
    // One-time move of services saved in the browser before the backend owned them.
    if (legacyFrontendTargets.length > 0) {
      const known = new Set(userServices.map((svc) => svc.url));
      const migrated = legacyFrontendTargets
        .map((t) => ({ name: t.name, url: `http://${t.host}:${t.port}` }))
        .filter((svc) => !known.has(svc.url));
      legacyFrontendTargets = [];
      if (migrated.length > 0) await setUserServices([...userServices, ...migrated]);
      persistInfraSettings();
    }
  } catch {
    // older servers have no services endpoint; keep the list empty
  } finally {
    userServicesLoaded = true;
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
      knownAccounts.some((a) => a.id === parsed.activeAccountId)
    ) {
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
