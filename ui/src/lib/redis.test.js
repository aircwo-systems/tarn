import { describe, expect, test } from "bun:test";
import {
  formatRedisTTL,
  formatUptime,
  groupRedisKeysByPrefix,
  isRedisProbeKind,
  redisProbeId,
  tryPrettyJson,
} from "./redis";

describe("redis helpers", () => {
  test("formats TTL values cleanly", () => {
    expect(formatRedisTTL(-1)).toBe("No expiry");
    expect(formatRedisTTL(-2)).toBe("Expired");
    expect(formatRedisTTL(4500)).toBe("5s");
    expect(formatRedisTTL(125000)).toBe("2m 5s");
    expect(formatRedisTTL(7200000)).toBe("2h");
    expect(formatRedisTTL(90000000)).toBe("1d 1h");
  });

  test("groups keys by colon prefix with root last", () => {
    const groups = groupRedisKeysByPrefix([
      { key: "user:1", type: "string", ttlMs: -1 },
      { key: "standalone", type: "string", ttlMs: -1 },
      { key: "cache:home", type: "hash", ttlMs: 5000 },
      { key: "user:2", type: "string", ttlMs: -1 },
    ]);
    expect(groups.map((g) => [g.prefix, g.keys.length])).toEqual([
      ["cache", 1],
      ["user", 2],
      ["(root)", 1],
    ]);
  });

  test("pretty prints valid JSON objects/arrays and ignores plain strings", () => {
    expect(tryPrettyJson(`{"a":1}`).isJson).toBe(true);
    expect(tryPrettyJson(`hello`)).toEqual({ formatted: "hello", isJson: false });
  });

  test("identifies redis probe kinds and builds matching probe IDs", () => {
    expect(isRedisProbeKind("redis")).toBe(true);
    expect(isRedisProbeKind("rediss")).toBe(true);
    expect(isRedisProbeKind("postgresql")).toBe(false);
    expect(redisProbeId({ kind: "redis", host: "localhost", port: 6379 })).toBe("redis-localhost-6379");
    expect(formatUptime(3660)).toBe("1h 1m");
  });
});
