import type {
  EventBridgeFireResult,
  EventBridgeRaceResult,
  OverviewResponse,
  QueueMessagesResponse,
  QueueMessageSummary,
  SecretValueResult,
  LogGroupSummary,
  LogEventsResponse,
} from "$lib/types";

const configuredBase = normalizeBase(
  typeof import.meta !== "undefined" && import.meta.env?.VITE_OPENSTACK_API_BASE
    ? String(import.meta.env.VITE_OPENSTACK_API_BASE)
    : "",
);
const awsProtocolBase = normalizeAWSProtocolBase(configuredBase);

function endpoint(path: string): string {
  return `${configuredBase}${path}`;
}

function awsEndpoint(path: string): string {
  return `${awsProtocolBase}${path}`;
}

export async function fetchOverview(signal?: AbortSignal): Promise<OverviewResponse> {
  const overviewPath = "/_openstack/admin/overview";
  const healthPath = "/_openstack/health";
  const overviewURL = endpoint(overviewPath);
  const response = await fetch(overviewURL, {
    method: "GET",
    headers: {
      Accept: "application/json",
    },
    signal,
  });

  if (response.status === 404) {
    const healthURL = endpoint(healthPath);
    const health = await fetch(healthURL, {
      method: "GET",
      headers: {
        Accept: "application/json",
      },
      signal,
    }).catch(() => null);

    if (health?.ok) {
      throw new Error(
        "Connected to OpenStack, but this instance does not expose /_openstack/admin/overview yet. Rebuild and restart OpenStack from the latest code.",
      );
    }
  }

  if (!response.ok) {
    let message = `HTTP ${response.status}`;
    try {
      const body = (await response.json()) as { error?: string };
      if (body?.error) {
        message = body.error;
      }
    } catch {
      // Ignore non-JSON error bodies.
    }
    throw new Error(message);
  }

  const overview = (await response.json()) as OverviewResponse;
  await hydrateQueueMessages(overview, signal);
  return overview;
}

export async function fetchQueueMessages(
  queueName: string,
  signal?: AbortSignal,
): Promise<QueueMessageSummary[]> {
  const messagesPath = `/_openstack/admin/queues/${encodeURIComponent(queueName)}/messages?limit=20`;
  const response = await fetch(endpoint(messagesPath), {
    method: "GET",
    headers: {
      Accept: "application/json",
    },
    signal,
  });

  if (!response.ok) {
    throw new Error(`Failed to load queue messages: HTTP ${response.status}`);
  }

  const payload = (await response.json()) as QueueMessagesResponse;
  if (!Array.isArray(payload?.messages)) {
    return [];
  }
  return payload.messages;
}

export async function fetchSecretValue(
  secretName: string,
  signal?: AbortSignal,
): Promise<SecretValueResult> {
  const secretPath = `/_openstack/admin/secrets/${encodeURIComponent(secretName)}/value`;
  const response = await fetch(endpoint(secretPath), {
    method: "GET",
    headers: {
      Accept: "application/json",
    },
    signal,
  });

  if (!response.ok) {
    let message = `Failed to load secret value: HTTP ${response.status}`;
    try {
      const body = (await response.json()) as { error?: string };
      if (body?.error) {
        message = body.error;
      }
    } catch {
      // Ignore non-JSON error bodies.
    }
    throw new Error(message);
  }

  const payload = (await response.json()) as SecretValueResult;
  return {
    name: payload.name ?? secretName,
    value: payload.value ?? "",
    valueType: payload.valueType ?? "string",
  };
}

export async function fetchLogGroups(signal?: AbortSignal): Promise<LogGroupSummary[]> {
  const response = await fetch(endpoint("/_openstack/admin/logs/groups"), {
    method: "GET",
    headers: { Accept: "application/json" },
    signal,
  });
  if (!response.ok) {
    throw new Error(`Failed to load log groups: HTTP ${response.status}`);
  }
  const data = await response.json();
  return Array.isArray(data) ? data : [];
}

export interface FetchLogEventsParams {
  limit?: number;
  offset?: number; // Deprecated
  level?: string;
  pattern?: string;
  stream?: string;
  cursor?: string;
}

export async function fetchLogEvents(
  groupName: string,
  params: FetchLogEventsParams = {},
  signal?: AbortSignal,
): Promise<LogEventsResponse> {
  const query = new URLSearchParams();
  if (params.limit) query.set("limit", String(params.limit));
  if (params.offset) query.set("offset", String(params.offset)); // Deprecated
  if (params.level) query.set("level", params.level);
  if (params.pattern) query.set("pattern", params.pattern);
  if (params.stream) query.set("stream", params.stream);
  if (params.cursor) query.set("cursor", params.cursor);

  const qs = query.toString();
  const url = endpoint(
    `/_openstack/admin/logs/events/${encodeURIComponent(groupName)}${qs ? "?" + qs : ""}`,
  );
  const response = await fetch(url, {
    method: "GET",
    headers: { Accept: "application/json" },
    signal,
  });
  if (!response.ok) {
    throw new Error(`Failed to load log events: HTTP ${response.status}`);
  }
  return (await response.json()) as LogEventsResponse;
}

export interface PutEventBridgeRuleInput {
  name: string;
  scheduleExpression: string;
  state?: "ENABLED" | "DISABLED";
  description?: string;
}

export interface EventBridgeTargetInput {
  id: string;
  arn: string;
  input?: string;
  inputPath?: string;
  inputTransformer?: {
    inputPathsMap?: Record<string, string>;
    inputTemplate: string;
  };
}

export interface EventBridgeBatchFailedEntry {
  targetId?: string;
  errorCode?: string;
  errorMessage?: string;
}

export interface EventBridgeBatchResult {
  failedEntryCount: number;
  failedEntries: EventBridgeBatchFailedEntry[];
}

export async function putEventBridgeRule(
  input: PutEventBridgeRuleInput,
): Promise<{ ruleArn: string }> {
  const payload = await eventBridgeCall<{ RuleArn?: string }>("PutRule", {
    Name: input.name,
    ScheduleExpression: input.scheduleExpression,
    State: input.state,
    Description: input.description ?? "",
    EventBusName: "default",
  });
  return { ruleArn: payload.RuleArn ?? "" };
}

export async function deleteEventBridgeRule(ruleName: string): Promise<void> {
  await eventBridgeCall("DeleteRule", {
    Name: ruleName,
    EventBusName: "default",
  });
}

export async function enableEventBridgeRule(ruleName: string): Promise<void> {
  await eventBridgeCall("EnableRule", {
    Name: ruleName,
    EventBusName: "default",
  });
}

export async function disableEventBridgeRule(ruleName: string): Promise<void> {
  await eventBridgeCall("DisableRule", {
    Name: ruleName,
    EventBusName: "default",
  });
}

export async function putEventBridgeTargets(
  ruleName: string,
  targets: EventBridgeTargetInput[],
): Promise<EventBridgeBatchResult> {
  const payload = await eventBridgeCall<{
    FailedEntryCount?: number;
    FailedEntries?: Array<{
      TargetId?: string;
      ErrorCode?: string;
      ErrorMessage?: string;
    }>;
  }>("PutTargets", {
    Rule: ruleName,
    EventBusName: "default",
    Targets: targets.map((target) => ({
      Id: target.id,
      Arn: target.arn,
      Input: target.input,
      InputPath: target.inputPath,
      InputTransformer: target.inputTransformer
        ? {
            InputPathsMap: target.inputTransformer.inputPathsMap,
            InputTemplate: target.inputTransformer.inputTemplate,
          }
        : undefined,
    })),
  });
  return {
    failedEntryCount: payload.FailedEntryCount ?? 0,
    failedEntries:
      payload.FailedEntries?.map((entry) => ({
        targetId: entry.TargetId,
        errorCode: entry.ErrorCode,
        errorMessage: entry.ErrorMessage,
      })) ?? [],
  };
}

export async function removeEventBridgeTargets(
  ruleName: string,
  ids: string[],
): Promise<EventBridgeBatchResult> {
  const payload = await eventBridgeCall<{
    FailedEntryCount?: number;
    FailedEntries?: Array<{
      TargetId?: string;
      ErrorCode?: string;
      ErrorMessage?: string;
    }>;
  }>("RemoveTargets", {
    Rule: ruleName,
    EventBusName: "default",
    Ids: ids,
  });

  return {
    failedEntryCount: payload.FailedEntryCount ?? 0,
    failedEntries:
      payload.FailedEntries?.map((entry) => ({
        targetId: entry.TargetId,
        errorCode: entry.ErrorCode,
        errorMessage: entry.ErrorMessage,
      })) ?? [],
  };
}

export async function fireEventBridgeRule(
  ruleName: string,
): Promise<EventBridgeFireResult> {
  const response = await fetch(endpoint("/_openstack/admin/eventbridge/fire"), {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify({ ruleName }),
  });
  if (!response.ok) {
    throw new Error(await extractJSONError(response, `HTTP ${response.status}`));
  }
  return (await response.json()) as EventBridgeFireResult;
}

export async function runEventBridgeRace(
  ruleName: string,
  runs: number,
  concurrency: number,
): Promise<EventBridgeRaceResult> {
  const response = await fetch(endpoint("/_openstack/admin/eventbridge/race"), {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify({ ruleName, runs, concurrency }),
  });
  if (!response.ok) {
    throw new Error(await extractJSONError(response, `HTTP ${response.status}`));
  }
  return (await response.json()) as EventBridgeRaceResult;
}

async function hydrateQueueMessages(
  overview: OverviewResponse,
  signal?: AbortSignal,
): Promise<void> {
  const queuesToHydrate = overview.queues.filter((queue) => {
    const totalMessages = queue.approxVisible + queue.approxInFlight + queue.approxDelayed;
    const hasRecentMessages =
      Array.isArray(queue.recentMessages) && queue.recentMessages.length > 0;
    return totalMessages > 0 && !hasRecentMessages;
  });

  await Promise.all(
    queuesToHydrate.map(async (queue) => {
      try {
        queue.recentMessages = await fetchQueueMessages(queue.name, signal);
      } catch {
        // Keep overview data even if queue detail fetch fails.
      }
    }),
  );
}

function normalizeBase(raw: string): string {
  const input = raw.trim();
  if (!input) return "";

  try {
    const url = new URL(input);
    if (url.hostname === "0.0.0.0" || url.hostname === "::" || url.hostname === "[::]") {
      url.hostname = "127.0.0.1";
    }
    return url.toString().replace(/\/$/, "");
  } catch {
    return input.replace(/\/$/, "");
  }
}

function normalizeAWSProtocolBase(base: string): string {
  const input = base.trim();
  if (!input) return "";

  try {
    const url = new URL(input);
    if (url.hostname === "0.0.0.0" || url.hostname === "::" || url.hostname === "[::]") {
      url.hostname = "127.0.0.1";
    }
    url.pathname = "/";
    url.search = "";
    url.hash = "";
    return url.toString().replace(/\/$/, "");
  } catch {
    // Relative base paths are typically dashboard/admin prefixes.
    // AWS JSON protocol actions should target the API root.
    return "";
  }
}

async function eventBridgeCall<T = unknown>(action: string, body: unknown): Promise<T> {
  const headers = {
    "Content-Type": "application/x-amz-json-1.1",
    "X-Amz-Target": `AWSEvents.${action}`,
    Accept: "application/json",
  };
  const payload = JSON.stringify(body ?? {});
  const candidateURLs = [endpoint("/_openstack/events")];
  if (awsProtocolBase) {
    const protocolScopedPath = awsEndpoint("/_openstack/events");
    if (!candidateURLs.includes(protocolScopedPath)) {
      candidateURLs.push(protocolScopedPath);
    }
    const protocolRoot = awsEndpoint("/");
    if (!candidateURLs.includes(protocolRoot)) {
      candidateURLs.push(protocolRoot);
    }
  }

  let response: Response | null = null;
  for (let i = 0; i < candidateURLs.length; i++) {
    response = await fetch(candidateURLs[i], {
      method: "POST",
      headers,
      body: payload,
    });
    if (response.status !== 405 || i === candidateURLs.length - 1) {
      break;
    }
  }

  if (!response) {
    throw new Error(`AWSEvents.${action} failed: no response`);
  }

  if (!response.ok) {
    const fallback = `AWSEvents.${action} failed: HTTP ${response.status}`;
    throw new Error(await extractAMZError(response, fallback));
  }

  return (await response.json()) as T;
}

async function extractAMZError(response: Response, fallback: string): Promise<string> {
  try {
    const body = (await response.json()) as {
      __type?: string;
      message?: string;
      error?: string;
    };
    const code = body.__type?.trim() ?? "";
    const message = body.message?.trim() || body.error?.trim() || fallback;
    return code ? `${code}: ${message}` : message;
  } catch {
    return fallback;
  }
}

async function extractJSONError(response: Response, fallback: string): Promise<string> {
  try {
    const body = (await response.json()) as {
      error?: string;
      message?: string;
    };
    return body.error ?? body.message ?? fallback;
  } catch {
    return fallback;
  }
}
