import type { LogEvent } from "./types";

const storageKey = (accountId: string) => `tarn:pinned-logs:v2:${accountId}`;

export interface PinnedLog {
  accountId: string;
  id: string;
  group: string;
  event: LogEvent;
}

export function pinnedLogId(group: string, eventKey: string, accountId: string): string {
  return `${accountId}\n${group}\n${eventKey}`;
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

export function pinsForView(
  pins: PinnedLog[],
  groups: string[] | null,
  order: "asc" | "desc",
): PinnedLog[] {
  const direction = order === "desc" ? -1 : 1;
  return pins
    .filter((pin) => groups === null || groups.includes(pin.group))
    .sort(
      (a, b) =>
        direction * (Date.parse(a.event.timestamp) - Date.parse(b.event.timestamp)) ||
        a.id.localeCompare(b.id),
    );
}

export function pinnedLogRow(pin: PinnedLog, showGroup: boolean): { event: LogEvent; key: string } {
  const key = pin.id.slice(`${pin.accountId}\n${pin.group}\n`.length);
  if (!showGroup) return { event: pin.event, key };
  const event = { ...pin.event, streamName: `${pin.group}/${pin.event.streamName}` };
  return {
    event,
    key: key.replace(
      `${event.timestamp}|${pin.event.streamName}|`,
      `${event.timestamp}|${event.streamName}|`,
    ),
  };
}

export function pinContextRows(
  pin: PinnedLog,
  older: LogEvent[],
  newer: LogEvent[],
  showGroup: boolean,
  order: "asc" | "desc",
): { events: LogEvent[]; selectedIndex: number; selectedKey: string } {
  const { event: selected, key: selectedKey } = pinnedLogRow(pin, showGroup);
  const direction = order === "desc" ? -1 : 1;
  const events = [...older, selected, ...newer].sort(
    (a, b) => direction * (Date.parse(a.timestamp) - Date.parse(b.timestamp)),
  );
  return { events, selectedIndex: events.indexOf(selected), selectedKey };
}

export function nextPinnedIndex(pins: PinnedLog[], currentId: string): number {
  if (pins.length === 0) return -1;
  return (pins.findIndex((pin) => pin.id === currentId) + 1) % pins.length;
}

export function readPinnedLogs(accountId: string): PinnedLog[] {
  if (typeof localStorage === "undefined") return [];
  try {
    // v1 pins have no account identity. Preserve that storage without attributing it.
    const value: unknown = JSON.parse(localStorage.getItem(storageKey(accountId)) ?? "[]");
    if (!Array.isArray(value)) return [];
    return value.filter(isPinnedLog).filter((pin) => pin.accountId === accountId);
  } catch {
    return [];
  }
}

export function savePinnedLogs(pins: PinnedLog[], accountId: string): void {
  localStorage.setItem(storageKey(accountId), JSON.stringify(pins));
}

function isPinnedLog(value: unknown): value is PinnedLog {
  if (typeof value !== "object" || value === null) return false;
  if (!("accountId" in value) || typeof value.accountId !== "string") return false;
  if (!("id" in value) || typeof value.id !== "string") return false;
  if (!("group" in value) || typeof value.group !== "string") return false;
  if (!value.id.startsWith(`${value.accountId}\n${value.group}\n`)) return false;
  if (!("event" in value) || typeof value.event !== "object" || value.event === null) return false;
  const event = value.event;
  return (
    "timestamp" in event &&
    typeof event.timestamp === "string" &&
    "message" in event &&
    typeof event.message === "string" &&
    "level" in event &&
    typeof event.level === "string" &&
    "streamName" in event &&
    typeof event.streamName === "string"
  );
}
