import type { SavedTestEvent } from "$lib/types";

function storageKey(accountId: string, functionName: string): string {
  const acct = accountId?.trim() || "000000000000";
  const fn = functionName?.trim() || "";
  return `tarn:saved-events:v1:${acct}:${fn}`;
}

export function loadSavedEvents(accountId: string, functionName: string): SavedTestEvent[] {
  if (typeof localStorage === "undefined") return [];
  const key = storageKey(accountId, functionName);
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(
      (item): item is SavedTestEvent =>
        item &&
        typeof item.id === "string" &&
        typeof item.name === "string" &&
        typeof item.payload === "string",
    );
  } catch {
    return [];
  }
}

export function saveTestEvent(
  accountId: string,
  functionName: string,
  event: { name: string; payload: string; source?: string },
): SavedTestEvent {
  const existing = loadSavedEvents(accountId, functionName);
  const id = `event-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
  const newEvent: SavedTestEvent = {
    id,
    name: event.name.trim() || "Untitled Event",
    payload: event.payload,
    createdAt: new Date().toISOString(),
    source: event.source,
  };

  const updated = [newEvent, ...existing];
  if (typeof localStorage !== "undefined") {
    try {
      localStorage.setItem(storageKey(accountId, functionName), JSON.stringify(updated));
    } catch {
      // Best-effort storage
    }
  }
  return newEvent;
}

export function deleteSavedEvent(
  accountId: string,
  functionName: string,
  eventId: string,
): void {
  const existing = loadSavedEvents(accountId, functionName);
  const updated = existing.filter((e) => e.id !== eventId);
  if (typeof localStorage !== "undefined") {
    try {
      localStorage.setItem(storageKey(accountId, functionName), JSON.stringify(updated));
    } catch {
      // Best-effort storage
    }
  }
}
