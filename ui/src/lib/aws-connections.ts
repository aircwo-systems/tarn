import { fetchInstanceResource } from "#lib/api";

export type AWSIdentity = { accountId: string; arn: string };
export type ManualAWSCredentials =
  | { kind: "long-lived"; accessKeyId: string; secretAccessKey: string }
  | { kind: "temporary"; accessKeyId: string; secretAccessKey: string; sessionToken: string };
export type AWSCredentials = ManualAWSCredentials | { kind: "profile"; profile: string };
export type AWSConnectionInput = {
  name: string;
  region: string;
  credentials?: AWSCredentials;
  acceptedIdentity?: AWSIdentity;
};
export type AWSOwnerState =
  | { kind: "unavailable"; reason: string }
  | { kind: "locked" }
  | { kind: "unlocked"; manualCredentialsAvailable: boolean };
type AWSConnectionMetadata = {
  id: string;
  name: string;
  region: string;
  identity: AWSIdentity;
  createdAt: string;
  updatedAt: string;
  lastVerifiedAt: string;
  lastTestedAt: string;
  status: "verified" | "rejected" | "expired" | "unavailable" | "untested" | "needs-login";
  message: string;
};
export type AWSConnection = AWSConnectionMetadata &
  (
    | { kind: "profile"; profile: string }
    | { kind: "long-lived" | "temporary"; accessKeySuffix: string }
  );

let csrfToken = "";
let unlockPromise: Promise<void> | null = null;
let unlockError = "";

export class AWSConnectionError extends Error {
  constructor(
    message: string,
    readonly identity?: AWSIdentity,
    readonly status = 0,
  ) {
    super(message);
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function textField(body: Record<string, unknown>, key: string): string {
  const value = body[key];
  if (typeof value !== "string") throw new Error("Tarn returned invalid AWS connection metadata.");
  return value;
}

function parseIdentity(value: unknown): AWSIdentity {
  if (!isRecord(value)) throw new Error("Tarn returned an invalid AWS identity.");
  return { accountId: textField(value, "accountId"), arn: textField(value, "arn") };
}

function parseConnection(value: unknown): AWSConnection {
  if (!isRecord(value)) throw new Error("Tarn returned invalid AWS connection metadata.");
  const kind = value.kind;
  const status = value.status;
  if (kind !== "long-lived" && kind !== "temporary" && kind !== "profile")
    throw new Error("Unsupported AWS credential kind.");
  if (
    status !== "verified" &&
    status !== "rejected" &&
    status !== "expired" &&
    status !== "unavailable" &&
    status !== "untested" &&
    status !== "needs-login"
  )
    throw new Error("Unsupported AWS connection status.");
  const metadata: AWSConnectionMetadata = {
    id: textField(value, "id"),
    name: textField(value, "name"),
    region: textField(value, "region"),
    status,
    identity: parseIdentity(value.identity),
    createdAt: textField(value, "createdAt"),
    updatedAt: textField(value, "updatedAt"),
    lastVerifiedAt: textField(value, "lastVerifiedAt"),
    lastTestedAt: textField(value, "lastTestedAt"),
    message: typeof value.message === "string" ? value.message : "",
  };
  return kind === "profile"
    ? { ...metadata, kind, profile: textField(value, "profile") }
    : { ...metadata, kind, accessKeySuffix: textField(value, "accessKeySuffix") };
}

async function request(path: string, method = "GET", body?: unknown): Promise<unknown> {
  const headers = new Headers({ Accept: "application/json" });
  if (method !== "GET") headers.set("X-Tarn-CSRF", csrfToken);
  if (body !== undefined) headers.set("Content-Type", "application/json");
  if (path === "/_tarn/owner/session" && method === "POST")
    headers.set("X-Tarn-Owner-Request", "1");
  const response = await fetchInstanceResource(path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    cache: "no-store",
  });
  if (response.status === 204) return null;
  const result: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    if (response.status === 401) csrfToken = "";
    const message =
      isRecord(result) && typeof result.error === "string"
        ? result.error
        : "Could not reach AWS connection management.";
    const identity =
      isRecord(result) && result.identity !== undefined
        ? parseIdentity(result.identity)
        : undefined;
    throw new AWSConnectionError(message, identity, response.status);
  }
  return result;
}

// Strip the capability synchronously, before routing remembers the fragment.
// It is exchanged once and is never written to browser storage or telemetry.
export function unlockAWSOwnerFromLocation(): Promise<void> {
  if (typeof window === "undefined") return Promise.resolve();
  const params = new URLSearchParams(window.location.hash.split("?").slice(1).join("?"));
  const token = params.get("aws-unlock");
  if (!token) return unlockPromise ?? Promise.resolve();
  window.history.replaceState(
    window.history.state,
    "",
    `${window.location.pathname}${window.location.search}#settings?section=aws`,
  );
  unlockError = "";
  unlockPromise = request("/_tarn/owner/session", "POST", { token }).then(
    () => {
      unlockError = "";
    },
    (error: unknown) => {
      unlockError = error instanceof Error ? error.message : "Could not unlock AWS connections.";
    },
  );
  return unlockPromise;
}

export function ownerUnlockError(): string {
  return unlockError;
}

export async function fetchAWSOwnerState(): Promise<AWSOwnerState> {
  await unlockAWSOwnerFromLocation();
  const result = await request("/_tarn/owner/status");
  if (!isRecord(result)) throw new Error("Tarn returned an invalid owner session.");
  csrfToken = "";
  if (result.available === false)
    return { kind: "unavailable", reason: textField(result, "reason") };
  if (result.available !== true || typeof result.authenticated !== "boolean")
    throw new Error("Tarn returned an invalid owner session.");
  if (!result.authenticated) return { kind: "locked" };
  csrfToken = textField(result, "csrfToken");
  if (typeof result.manualCredentialsAvailable !== "boolean")
    throw new Error("Tarn returned an invalid owner configuration.");
  return { kind: "unlocked", manualCredentialsAvailable: result.manualCredentialsAvailable };
}

const base = "/_tarn/admin/aws-connections";

export async function fetchAWSProfiles(): Promise<string[]> {
  const result = await request(`${base}/profiles`);
  if (!isRecord(result) || !Array.isArray(result.profiles))
    throw new Error("Tarn returned invalid AWS profile names.");
  return result.profiles.map((name: unknown) => {
    if (typeof name !== "string") throw new Error("Tarn returned an invalid AWS profile name.");
    return name;
  });
}

export async function fetchAWSConnections(): Promise<AWSConnection[]> {
  const result = await request(base);
  if (!isRecord(result) || !Array.isArray(result.connections))
    throw new Error("Tarn returned an invalid AWS connection list.");
  return result.connections.map(parseConnection);
}

export async function saveAWSConnection(
  id: string | null,
  input: AWSConnectionInput,
): Promise<AWSConnection> {
  return parseConnection(
    await request(id ? `${base}/${encodeURIComponent(id)}` : base, id ? "PATCH" : "POST", input),
  );
}

export async function testAWSConnection(id: string): Promise<AWSConnection> {
  return parseConnection(await request(`${base}/${encodeURIComponent(id)}/test`, "POST"));
}

export async function deleteAWSConnection(id: string): Promise<void> {
  await request(`${base}/${encodeURIComponent(id)}`, "DELETE");
}

export async function lockAWSConnections(): Promise<void> {
  await request("/_tarn/owner/session", "DELETE");
  csrfToken = "";
}
