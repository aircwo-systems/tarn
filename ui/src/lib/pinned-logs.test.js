import { describe, expect, test } from "bun:test";
import { nextPinnedIndex, pinContextRows, pinEventKey, pinEventSnapshot, pinnedLogId, pinnedLogRow, pinsForView, readPinnedLogs, savePinnedLogs } from "./pinned-logs";

describe("pinned log identity", () => {
  test("finds the same event in aggregated and group views", () => {
    const group = "/aws/lambda/orders";
    const event = {
      timestamp: "2026-09-25T12:00:00.000Z",
      message: "request failed",
      level: "ERROR",
      streamName: "stream-1",
    };
    const groupKey = `${event.timestamp}|${event.streamName}||${event.message}#1`;
    const aggregateEvent = { ...event, streamName: `${group}/${event.streamName}` };
    const aggregateKey = `${event.timestamp}|${aggregateEvent.streamName}||${event.message}#1`;

    expect(pinnedLogId(group, pinEventKey(group, aggregateEvent, aggregateKey)))
      .toBe(pinnedLogId(group, groupKey));
    expect(pinEventSnapshot(group, aggregateEvent)).toEqual(event);
  });

  test("saves the event snapshot and ignores malformed stored entries", () => {
    const original = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
    const values = new Map();
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: {
        getItem: (key) => values.get(key) ?? null,
        setItem: (key, value) => values.set(key, value),
      },
    });
    try {
      const pin = {
        id: "orders\nkey",
        group: "orders",
        event: { timestamp: "2026-09-25T12:00:00.000Z", message: "error", level: "ERROR", streamName: "stream-1" },
      };
      savePinnedLogs([pin]);
      expect(readPinnedLogs()).toEqual([pin]);
      values.set("tarn:pinned-logs:v1", JSON.stringify([pin, { group: "orders" }]));
      expect(readPinnedLogs()).toEqual([pin]);
    } finally {
      if (original) Object.defineProperty(globalThis, "localStorage", original);
      else delete globalThis.localStorage;
    }
  });

  test("filters pins by group and cycles in the selected time order", () => {
    const event = (timestamp) => ({ timestamp, message: "failed", level: "ERROR", streamName: "stream-1" });
    const pins = [
      { id: "a\nnew", group: "a", event: event("2026-09-25T12:02:00Z") },
      { id: "b\nother", group: "b", event: event("2026-09-25T12:01:00Z") },
      { id: "a\nold", group: "a", event: event("2026-09-25T12:00:00Z") },
    ];
    const groupPins = pinsForView(pins, ["a"], "asc");
    expect(groupPins.map((pin) => pin.id)).toEqual(["a\nold", "a\nnew"]);
    expect(nextPinnedIndex(groupPins, "a\nold")).toBe(1);
    expect(nextPinnedIndex(groupPins, "a\nnew")).toBe(0);
    expect(pinsForView(pins, null, "desc").map((pin) => pin.id)).toEqual(["a\nnew", "b\nother", "a\nold"]);
  });

  test("restores the aggregate key so a saved duplicate row stays selected", () => {
    const group = "/aws/lambda/orders";
    const event = { timestamp: "2026-09-25T12:00:00Z", message: "failed", level: "ERROR", streamName: "stream-1" };
    const key = `${event.timestamp}|${event.streamName}||${event.message}#1`;
    const pin = { id: pinnedLogId(group, key), group, event };
    const row = pinnedLogRow(pin, true);
    expect(row.event.streamName).toBe(`${group}/stream-1`);
    expect(pinEventKey(group, row.event, row.key)).toBe(key);
  });

  test("places a pin between surrounding logs in either sort order", () => {
    const group = "/aws/lambda/orders";
    const event = (time, message) => ({ timestamp: `2026-09-25T12:00:${time}Z`, message, level: "INFO", streamName: "stream-1" });
    const pin = { id: pinnedLogId(group, "saved-key"), group, event: event("02", "pinned") };
    const older = [event("01", "before")];
    const newer = [event("03", "after")];

    const ascending = pinContextRows(pin, older, newer, false, "asc");
    expect(ascending.events.map((row) => row.message)).toEqual(["before", "pinned", "after"]);
    expect(ascending.selectedIndex).toBe(1);
    expect(ascending.selectedKey).toBe("saved-key");

    const descending = pinContextRows(pin, older, newer, true, "desc");
    expect(descending.events.map((row) => row.message)).toEqual(["after", "pinned", "before"]);
    expect(descending.events[descending.selectedIndex].streamName).toBe(`${group}/stream-1`);
  });
});
