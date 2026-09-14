import { fetchAllLogEvents } from "$lib/api";
import type { LogEvent } from "$lib/types";

/**
 * Lightweight, app-wide pulse of recent log activity. Lives at module level so
 * it survives tab switches (the logs section is re-mounted on every visit) and
 * can feed the sidebar indicator while the logs view is closed.
 */

export const PULSE_WINDOW_MS = 60_000;
export const PULSE_BUCKETS = 16;
const ERROR_WINDOW_MS = 5 * 60_000;
const POLL_MS = 4_000;
const SAMPLE_LIMIT = 200;

export interface LogPulse {
  /** Event counts per bucket, oldest → newest, spanning PULSE_WINDOW_MS. */
  bars: number[];
  /** Highest severity per bucket: 0 none/info, 1 warn, 2 error. */
  severity: number[];
  recentErrors: number;
  recentWarnings: number;
  lastEventAt: number;
  lastLevel: string;
}

let sample = $state<LogEvent[]>([]);
let now = $state(Date.now());
let handle: ReturnType<typeof setInterval> | null = null;
let controller: AbortController | null = null;
let consumers = 0;

async function poll() {
  // Never stack requests: a slow payload aborts rather than piling up.
  controller?.abort();
  const ctrl = new AbortController();
  controller = ctrl;
  try {
    const result = await fetchAllLogEvents({ limit: SAMPLE_LIMIT, order: "desc" }, ctrl.signal);
    sample = result.events ?? [];
  } catch {
    /* keep last sample; the pulse is advisory */
  } finally {
    if (controller === ctrl) controller = null;
    now = Date.now();
  }
}

/** Ref-counted start; returns a stop function. */
export function startLogPulse(): () => void {
  consumers++;
  if (!handle) {
    poll();
    handle = setInterval(poll, POLL_MS);
  }
  return () => {
    consumers = Math.max(0, consumers - 1);
    if (consumers === 0 && handle) {
      clearInterval(handle);
      handle = null;
      controller?.abort();
    }
  };
}

function severityOf(level: string): number {
  if (level === "ERROR") return 2;
  if (level === "WARN") return 1;
  return 0;
}

const pulse = $derived.by<LogPulse>(() => {
  const bars = new Array<number>(PULSE_BUCKETS).fill(0);
  const severity = new Array<number>(PULSE_BUCKETS).fill(0);
  const bucketMs = PULSE_WINDOW_MS / PULSE_BUCKETS;
  let recentErrors = 0;
  let recentWarnings = 0;
  let lastEventAt = 0;
  let lastLevel = "";

  for (const ev of sample) {
    const ts = Date.parse(ev.timestamp);
    if (Number.isNaN(ts)) continue;
    if (ts > lastEventAt) {
      lastEventAt = ts;
      lastLevel = ev.level;
    }
    const age = now - ts;
    if (age <= ERROR_WINDOW_MS) {
      if (ev.level === "ERROR") recentErrors++;
      else if (ev.level === "WARN") recentWarnings++;
    }
    if (age < 0 || age >= PULSE_WINDOW_MS) continue;
    const idx = PULSE_BUCKETS - 1 - Math.floor(age / bucketMs);
    bars[idx]++;
    severity[idx] = Math.max(severity[idx], severityOf(ev.level));
  }

  return { bars, severity, recentErrors, recentWarnings, lastEventAt, lastLevel };
});

export function getLogPulse(): LogPulse {
  return pulse;
}
