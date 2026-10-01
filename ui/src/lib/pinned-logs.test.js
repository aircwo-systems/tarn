import { describe, expect, test } from "bun:test";
import {
  nextPinnedIndex,
  pinContextRows,
  pinEventKey,
  pinEventSnapshot,
  pinnedLogId,
  pinnedLogRow,
  pinsForView,
  readPinnedLogs,
  savePinnedLogs,
} from "./pinned-logs";

const accountA = "111111111111";
const accountB = "222222222222";
const group = "/aws/lambda/shared-name";
const event = {
  timestamp: "2026-10-01T00:00:00Z",
  message: "synthetic account A log",
  level: "INFO",
  streamName: "stream-1",
};

function withStorage(check) {
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
    check(values);
  } finally {
    if (original) Object.defineProperty(globalThis, "localStorage", original);
    else delete globalThis.localStorage;
  }
}

describe("pinned log account isolation", () => {
  test("another account cannot read, overwrite, or unpin the saved event", () => {
    withStorage(() => {
      const pinA = { accountId: accountA, id: pinnedLogId(group, "key", accountA), group, event };
      savePinnedLogs([pinA], accountA);
      expect(readPinnedLogs(accountA)).toEqual([pinA]);
      expect(readPinnedLogs(accountB)).toEqual([]);
      const pinB = { ...pinA, accountId: accountB, id: pinnedLogId(group, "key", accountB) };
      savePinnedLogs([pinB], accountB);
      expect(readPinnedLogs(accountA)).toEqual([pinA]);
      savePinnedLogs([], accountB);
      expect(readPinnedLogs(accountA)).toEqual([pinA]);
      expect(readPinnedLogs(accountB)).toEqual([]);
    });
  });

  test("identical event keys in different accounts have different pin identities", () => {
    expect(pinnedLogId(group, "key", accountA)).not.toBe(pinnedLogId(group, "key", accountB));
  });

  test("unattributed legacy pins remain stored without being assigned to an account", () => {
    withStorage((values) => {
      const legacy = JSON.stringify([{ id: `${group}\nkey`, group, event }]);
      values.set("tarn:pinned-logs:v1", legacy);
      expect(readPinnedLogs(accountA)).toEqual([]);
      expect(readPinnedLogs("000000000000")).toEqual([]);
      savePinnedLogs([], accountA);
      expect(values.get("tarn:pinned-logs:v1")).toBe(legacy);
    });
  });

  test("rejects another account's pin or a mismatched identity in account storage", () => {
    withStorage((values) => {
      const pinA = { accountId: accountA, id: pinnedLogId(group, "key", accountA), group, event };
      const pinB = { ...pinA, accountId: accountB, id: pinnedLogId(group, "key", accountB) };
      values.set(
        `tarn:pinned-logs:v2:${accountA}`,
        JSON.stringify([pinA, pinB, { ...pinA, id: pinB.id }, { ...pinA, event: {} }]),
      );
      expect(readPinnedLogs(accountA)).toEqual([pinA]);
    });
  });
});

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

    expect(pinnedLogId(group, pinEventKey(group, aggregateEvent, aggregateKey), accountA)).toBe(
      pinnedLogId(group, groupKey, accountA),
    );
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
        accountId: accountA,
        id: pinnedLogId("orders", "key", accountA),
        group: "orders",
        event: {
          timestamp: "2026-09-25T12:00:00.000Z",
          message: "error",
          level: "ERROR",
          streamName: "stream-1",
        },
      };
      savePinnedLogs([pin], accountA);
      expect(readPinnedLogs(accountA)).toEqual([pin]);
      values.set(`tarn:pinned-logs:v2:${accountA}`, JSON.stringify([pin, { group: "orders" }]));
      expect(readPinnedLogs(accountA)).toEqual([pin]);
    } finally {
      if (original) Object.defineProperty(globalThis, "localStorage", original);
      else delete globalThis.localStorage;
    }
  });

  test("filters pins by group and cycles in the selected time order", () => {
    const event = (timestamp) => ({
      timestamp,
      message: "failed",
      level: "ERROR",
      streamName: "stream-1",
    });
    const pins = [
      {
        accountId: accountA,
        id: pinnedLogId("a", "new", accountA),
        group: "a",
        event: event("2026-09-25T12:02:00Z"),
      },
      {
        accountId: accountA,
        id: pinnedLogId("b", "other", accountA),
        group: "b",
        event: event("2026-09-25T12:01:00Z"),
      },
      {
        accountId: accountA,
        id: pinnedLogId("a", "old", accountA),
        group: "a",
        event: event("2026-09-25T12:00:00Z"),
      },
    ];
    const groupPins = pinsForView(pins, ["a"], "asc");
    expect(groupPins.map((pin) => pin.id)).toEqual([pins[2].id, pins[0].id]);
    expect(nextPinnedIndex(groupPins, pins[2].id)).toBe(1);
    expect(nextPinnedIndex(groupPins, pins[0].id)).toBe(0);
    expect(pinsForView(pins, null, "desc").map((pin) => pin.id)).toEqual([
      pins[0].id,
      pins[1].id,
      pins[2].id,
    ]);
  });

  test("restores the aggregate key so a saved duplicate row stays selected", () => {
    const group = "/aws/lambda/orders";
    const event = {
      timestamp: "2026-09-25T12:00:00Z",
      message: "failed",
      level: "ERROR",
      streamName: "stream-1",
    };
    const key = `${event.timestamp}|${event.streamName}||${event.message}#1`;
    const pin = { accountId: accountA, id: pinnedLogId(group, key, accountA), group, event };
    const row = pinnedLogRow(pin, true);
    expect(row.event.streamName).toBe(`${group}/stream-1`);
    expect(pinEventKey(group, row.event, row.key)).toBe(key);
  });

  test("places a pin between surrounding logs in either sort order", () => {
    const group = "/aws/lambda/orders";
    const event = (time, message) => ({
      timestamp: `2026-09-25T12:00:${time}Z`,
      message,
      level: "INFO",
      streamName: "stream-1",
    });
    const pin = {
      accountId: accountA,
      id: pinnedLogId(group, "saved-key", accountA),
      group,
      event: event("02", "pinned"),
    };
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
