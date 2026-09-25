import type { LogEvent } from "./types";

const STORAGE_KEY = "tarn:pinned-logs:v1";

export interface PinnedLog {
  id: string;
  group: string;
  event: LogEvent;
}

export function pinnedLogId(group: string, eventKey: string): string {
  return `${group}\n${eventKey}`;
}

/** Aggregated results prefix stream names with the group; group results do not. */
export function pinEventKey(group: string, event: LogEvent, key: string): string {
  if (!event.streamName.startsWith(`${group}/`)) return key;
  return key.replace(
    `${event.timestamp}|${event.streamName}|`,
    `${event.timestamp}|${event.streamName.slice(group.length + 1)}|`,
  );
}

export function pinEventSnapshot(group: string, event: LogEvent): LogEvent {
  return {
    ...event,
    streamName: event.streamName.startsWith(`${group}/`)
      ? event.streamName.slice(group.length + 1)
      : event.streamName,
  };
}

export function pinsForView(pins: PinnedLog[], groups: string[] | null, order: "asc" | "desc"): PinnedLog[] {
  const direction = order === "desc" ? -1 : 1;
  return pins
    .filter((pin) => groups === null || groups.includes(pin.group))
    .sort((a, b) => direction * (Date.parse(a.event.timestamp) - Date.parse(b.event.timestamp)) || a.id.localeCompare(b.id));
}

export function pinnedLogRow(pin: PinnedLog, showGroup: boolean): { event: LogEvent; key: string } {
  const key = pin.id.slice(pin.group.length + 1);
  if (!showGroup) return { event: pin.event, key };
  const event = { ...pin.event, streamName: `${pin.group}/${pin.event.streamName}` };
  return {
    event,
    key: key.replace(`${event.timestamp}|${pin.event.streamName}|`, `${event.timestamp}|${event.streamName}|`),
  };
}

export function nextPinnedIndex(pins: PinnedLog[], currentId: string): number {
  if (pins.length === 0) return -1;
  return (pins.findIndex((pin) => pin.id === currentId) + 1) % pins.length;
}

export function readPinnedLogs(): PinnedLog[] {
  if (typeof localStorage === "undefined") return [];
  try {
    const value: unknown = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "[]");
    if (!Array.isArray(value)) return [];
    return value.filter(isPinnedLog);
  } catch {
    return [];
  }
}

export function savePinnedLogs(pins: PinnedLog[]): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(pins));
}

function isPinnedLog(value: unknown): value is PinnedLog {
  if (typeof value !== "object" || value === null) return false;
  if (!("id" in value) || typeof value.id !== "string") return false;
  if (!("group" in value) || typeof value.group !== "string") return false;
  if (!("event" in value) || typeof value.event !== "object" || value.event === null) return false;
  const event = value.event;
  return "timestamp" in event && typeof event.timestamp === "string"
    && "message" in event && typeof event.message === "string"
    && "level" in event && typeof event.level === "string"
    && "streamName" in event && typeof event.streamName === "string";
}
