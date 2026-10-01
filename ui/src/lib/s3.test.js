import { afterEach, beforeEach, describe, expect, spyOn, test } from "bun:test";
import { setApiAccount } from "./api";
import * as s3 from "./s3";

let fetchSpy;

beforeEach(() => {
  setApiAccount("222222222222");
  fetchSpy = spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 503 }));
});

afterEach(() => {
  fetchSpy.mockRestore();
  setApiAccount("000000000000");
});

describe("S3 account routing", () => {
  const file = new File(["synthetic upload"], "file.txt", { type: "text/plain" });
  const operations = [
    ["list", "GET", () => s3.listObjects("audit bucket", "folder/")],
    ["metadata", "HEAD", () => s3.headObject("audit bucket", "folder/file.txt")],
    ["content", "GET", () => s3.getObject("audit bucket", "folder/file.txt")],
    ["upload", "PUT", () => s3.putObject("audit bucket", "folder/file.txt", file)],
    ["delete", "DELETE", () => s3.deleteObject("audit bucket", "folder/file.txt")],
  ];

  for (const [name, method, operation] of operations) {
    test(`${name} selects the active account`, async () => {
      // A failed HTTP response still exercises request construction without needing an XML DOM.
      await expect(operation()).rejects.toThrow("503");
      const [url, init] = fetchSpy.mock.calls[0];
      const headers = new Headers(init?.headers);
      expect(headers.get("Authorization")).toContain("Credential=222222222222/");
      expect(init?.method ?? "GET").toBe(method);
      expect(url).toContain("/_s3/audit%20bucket");
      if (method === "PUT") {
        expect(headers.get("content-type")).toBe(file.type);
        expect(init.body).toBe(file);
      }
    });
  }

  test("the default account does not send an account override", async () => {
    setApiAccount("000000000000");
    await expect(s3.headObject("audit-bucket", "file.txt")).rejects.toThrow("503");
    expect(new Headers(fetchSpy.mock.calls[0][1]?.headers).has("Authorization")).toBe(false);
  });

  test("object content preserves the response and supports cancellation", async () => {
    const response = new Response("synthetic content", {
      headers: { "content-type": "text/plain" },
    });
    fetchSpy.mockResolvedValue(response);
    const controller = new AbortController();
    expect(await s3.getObject("audit-bucket", "file.txt", controller.signal)).toBe(response);
    expect(fetchSpy.mock.calls[0][1].signal).toBe(controller.signal);
  });
});
