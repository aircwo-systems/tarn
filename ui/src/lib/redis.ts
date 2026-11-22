import type { InfraProbe, RedisAuthOptions, RedisKeySummary } from "#lib/types";

export const REDIS_VIEWER_CHANNEL = "tarn-redis-viewer";
export const REDIS_VIEWER_TAB_ID =
  typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : `tab-${Math.random().toString(36).slice(2)}`;

export function notifyRedisMutation(targetId: string, db = 0) {
  try {
    const ch = new BroadcastChannel(REDIS_VIEWER_CHANNEL);
    ch.postMessage({ type: "keys-mutated", sender: REDIS_VIEWER_TAB_ID, targetId, db, at: Date.now() });
    ch.close();
  } catch {
    // BroadcastChannel may be unavailable in restricted contexts.
  }
}

export function redisProbeId(probe: Pick<InfraProbe, "kind" | "host" | "port">): string {
  return `${probe.kind}-${probe.host}-${probe.port}`;
}

export function isRedisProbeKind(kind: string | undefined): boolean {
  const k = (kind ?? "").toLowerCase();
  return k === "redis" || k === "rediss";
}

export function redisStatusTone(status?: string): "green" | "amber" | "red" | "neutral" {
  const s = (status ?? "").toLowerCase();
  if (s === "connected") return "green";
  if (s === "refused") return "amber";
  if (s === "unreachable" || s === "error") return "red";
  return "neutral";
}

export function redisTypeTone(type?: string): "green" | "amber" | "red" | "neutral" {
  switch ((type ?? "").toLowerCase()) {
    case "string":
      return "green";
    case "hash":
    case "zset":
    case "stream":
      return "amber";
    case "list":
    case "set":
      return "neutral";
    default:
      return "neutral";
  }
}

export function formatRedisTTL(ttlMs: number): string {
  if (ttlMs === -1) return "No expiry";
  if (ttlMs <= -2) return "Expired";
  const totalSec = Math.ceil(ttlMs / 1000);
  if (totalSec < 60) return `${totalSec}s`;
  const mins = Math.floor(totalSec / 60);
  const secs = totalSec % 60;
  if (mins < 60) return secs > 0 ? `${mins}m ${secs}s` : `${mins}m`;
  const hrs = Math.floor(mins / 60);
  const remMins = mins % 60;
  if (hrs < 24) return remMins > 0 ? `${hrs}h ${remMins}m` : `${hrs}h`;
  const days = Math.floor(hrs / 24);
  const remHrs = hrs % 24;
  return remHrs > 0 ? `${days}d ${remHrs}h` : `${days}d`;
}

export function formatUptime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return "--";
  if (seconds < 60) return `${seconds}s`;
  const mins = Math.floor(seconds / 60);
  if (mins < 60) return `${mins}m`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ${mins % 60}m`;
  const days = Math.floor(hrs / 24);
  return `${days}d ${hrs % 24}h`;
}

export function tryPrettyJson(raw: string): { formatted: string; isJson: boolean } {
  const trimmed = raw.trim();
  if (!trimmed || (!trimmed.startsWith("{") && !trimmed.startsWith("["))) {
    return { formatted: raw, isJson: false };
  }
  try {
    return { formatted: JSON.stringify(JSON.parse(trimmed), null, 2), isJson: true };
  } catch {
    return { formatted: raw, isJson: false };
  }
}

export interface RedisKeyGroup {
  prefix: string;
  keys: RedisKeySummary[];
}

export function groupRedisKeysByPrefix(keys: RedisKeySummary[], delimiter = ":"): RedisKeyGroup[] {
  const map = new Map<string, RedisKeySummary[]>();
  for (const k of keys) {
    const idx = k.key.indexOf(delimiter);
    const prefix = idx > 0 ? k.key.slice(0, idx) : "(root)";
    const list = map.get(prefix);
    if (list) list.push(k);
    else map.set(prefix, [k]);
  }
  return Array.from(map.entries())
    .sort(([a], [b]) => (a === "(root)" ? 1 : b === "(root)" ? -1 : a.localeCompare(b)))
    .map(([prefix, list]) => ({ prefix, keys: list }));
}

const AUTH_STORAGE_PREFIX = "tarn-redis-auth:";

export function loadRedisSessionAuth(targetId: string): Omit<RedisAuthOptions, "db"> {
  if (typeof sessionStorage === "undefined") return {};
  try {
    const raw = sessionStorage.getItem(AUTH_STORAGE_PREFIX + targetId);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Partial<RedisAuthOptions>;
    return {
      username: typeof parsed.username === "string" ? parsed.username : "",
      password: typeof parsed.password === "string" ? parsed.password : "",
      tls: parsed.tls === true,
    };
  } catch {
    return {};
  }
}

export function saveRedisSessionAuth(targetId: string, auth: Omit<RedisAuthOptions, "db">) {
  if (typeof sessionStorage === "undefined") return;
  try {
    if (!auth.username && !auth.password && !auth.tls) {
      sessionStorage.removeItem(AUTH_STORAGE_PREFIX + targetId);
      return;
    }
    sessionStorage.setItem(AUTH_STORAGE_PREFIX + targetId, JSON.stringify(auth));
  } catch {
    // Ignore storage errors in private contexts.
  }
}
