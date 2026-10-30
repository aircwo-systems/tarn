import { afterEach, expect, spyOn, test } from "bun:test";
import {
  fetchAWSOwnerState,
  saveAWSConnection,
  unlockAWSOwnerFromLocation,
  ownerUnlockError,
} from "./aws-connections";

let fetchSpy;
const originalWindow = globalThis.window;
afterEach(() => {
  fetchSpy?.mockRestore();
  globalThis.window = originalWindow;
});

test("unlock removes the capability before making a request and never falls back to storage", async () => {
  let replaced = "";
  globalThis.window = {
    location: { hash: "#settings?aws-unlock=synthetic-capability", pathname: "/", search: "" },
    history: {
      state: null,
      replaceState(_state, _title, url) {
        replaced = url;
        this.state = _state;
        window.location.hash = "#settings?section=aws";
      },
    },
  };
  fetchSpy = spyOn(globalThis, "fetch").mockImplementation(async (_url, init) => {
    expect(replaced).toBe("/#settings?section=aws");
    expect(init.headers.get("X-Tarn-Owner-Request")).toBe("1");
    expect(init.credentials).toBe("same-origin");
    expect(JSON.parse(init.body)).toEqual({ token: "synthetic-capability" });
    return Response.json({ error: "This unlock link has expired." }, { status: 403 });
  });
  await unlockAWSOwnerFromLocation();
  expect(ownerUnlockError()).toBe("This unlock link has expired.");
  expect(window.location.hash).not.toContain("synthetic-capability");
});

test("connection writes use the owner CSRF token independently of account signing", async () => {
  globalThis.window = undefined;
  const metadata = {
    id: "connection-id",
    name: "Development",
    region: "eu-west-2",
    kind: "temporary",
    accessKeySuffix: "7890",
    identity: { accountId: "123456789012", arn: "arn:aws:iam::123456789012:user/test" },
    createdAt: "2026-10-17T12:00:00Z",
    updatedAt: "2026-10-17T12:00:00Z",
    lastVerifiedAt: "2026-10-17T12:00:00Z",
    lastTestedAt: "2026-10-17T12:00:00Z",
    status: "verified",
  };
  fetchSpy = spyOn(globalThis, "fetch").mockImplementation(async (url, init) => {
    if (String(url).endsWith("/owner/status"))
      return Response.json({
        available: true,
        authenticated: true,
        csrfToken: "owner-csrf",
        manualCredentialsAvailable: true,
      });
    expect(init.headers.get("X-Tarn-CSRF")).toBe("owner-csrf");
    expect(init.headers.has("Authorization")).toBe(false);
    expect(init.cache).toBe("no-store");
    return Response.json(metadata, { status: 201 });
  });
  expect(await fetchAWSOwnerState()).toEqual({
    kind: "unlocked",
    manualCredentialsAvailable: true,
  });
  const saved = await saveAWSConnection(null, { name: "Development", region: "eu-west-2" });
  expect(saved.identity.accountId).toBe("123456789012");
  expect(saved).not.toHaveProperty("credentials");
});

test("malformed owner responses cannot establish an unlocked state", async () => {
  globalThis.window = undefined;
  fetchSpy = spyOn(globalThis, "fetch").mockResolvedValue(
    Response.json({ available: true, authenticated: true }),
  );
  await expect(fetchAWSOwnerState()).rejects.toThrow("invalid AWS connection metadata");
});
