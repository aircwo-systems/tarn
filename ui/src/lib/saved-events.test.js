import { describe, expect, test } from "bun:test";
import {
  loadSavedEvents,
  saveTestEvent,
  deleteSavedEvent,
} from "./saved-events";

function withStorage(check) {
  const original = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  const values = new Map();
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: {
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => {
        values.set(key, String(value));
      },
      removeItem: (key) => {
        values.delete(key);
      },
      clear: () => {
        values.clear();
      },
    },
  });

  try {
    check();
  } finally {
    if (original) Object.defineProperty(globalThis, "localStorage", original);
    else delete globalThis.localStorage;
  }
}

describe("saved test events", () => {
  test("saves and loads test events isolated by account and function", () => {
    withStorage(() => {
      const acct1 = "111111111111";
      const acct2 = "222222222222";
      const fn1 = "orderProcessor";
      const fn2 = "authHandler";

      saveTestEvent(acct1, fn1, {
        name: "Standard Order",
        payload: '{"orderId":"123"}',
      });

      const acct1Events = loadSavedEvents(acct1, fn1);
      expect(acct1Events.length).toBe(1);
      expect(acct1Events[0].name).toBe("Standard Order");
      expect(acct1Events[0].payload).toBe('{"orderId":"123"}');

      // Other function in same account has no events
      expect(loadSavedEvents(acct1, fn2).length).toBe(0);

      // Other account with same function name has no events
      expect(loadSavedEvents(acct2, fn1).length).toBe(0);
    });
  });

  test("deletes saved event by id", () => {
    withStorage(() => {
      const acct = "111111111111";
      const fn = "orderProcessor";

      const ev1 = saveTestEvent(acct, fn, { name: "Event 1", payload: "{}" });
      const ev2 = saveTestEvent(acct, fn, { name: "Event 2", payload: "{}" });

      expect(loadSavedEvents(acct, fn).length).toBe(2);

      deleteSavedEvent(acct, fn, ev1.id);
      const remaining = loadSavedEvents(acct, fn);
      expect(remaining.length).toBe(1);
      expect(remaining[0].id).toBe(ev2.id);
    });
  });
});
