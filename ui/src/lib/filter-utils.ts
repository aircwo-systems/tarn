export type DirectPrototypeFilter = {
  kind:
    | "gateway"
    | "eventbridge"
    | "topic"
    | "queue"
    | "dynamodb"
    | "function"
    | "secret"
    | "userpool"
    | "bucket"
    | "extension"
    | "infra";
  infraKind?: string;
} | null;

export function parseFilterTokens(value: string): string[] {
  return value
    .trim()
    .split(/\s+/)
    .map((t) => t.trim())
    .filter(Boolean);
}

export function mergeFilterTokens(current: string[], draft: string): string[] {
  const next = [...current];
  for (const token of parseFilterTokens(draft)) {
    if (!next.includes(token)) next.push(token);
  }
  return next;
}

export function resolveDirectPrototypeFilter(query: string): DirectPrototypeFilter {
  for (const token of parseFilterTokens(query)) {
    const n = token.toLowerCase();
    switch (n) {
      case "gateway":
      case "gateways":
      case "api":
      case "apigateway":
      case "api-gateway":
        return { kind: "gateway" };
      case "eventbridge":
      case "event-bridge":
      case "schedule":
      case "schedules":
      case "rule":
      case "rules":
        return { kind: "eventbridge" };
      case "topic":
      case "topics":
      case "sns":
        return { kind: "topic" };
      case "queue":
      case "queues":
      case "sqs":
        return { kind: "queue" };
      case "dynamodb":
      case "dynamo":
      case "ddb":
      case "table":
      case "tables":
      case "stream":
      case "streams":
        return { kind: "dynamodb" };
      case "lambda":
      case "lambdas":
      case "function":
      case "functions":
        return { kind: "function" };
      case "secret":
      case "secrets":
        return { kind: "secret" };
      case "cognito":
      case "userpool":
      case "userpools":
      case "user-pool":
        return { kind: "userpool" };
      case "bucket":
      case "buckets":
      case "s3":
      case "storage":
        return { kind: "bucket" };
      case "extension":
      case "extensions":
      case "cache":
        return { kind: "extension" };
      case "external":
      case "externals":
      case "infra":
      case "infrastructure":
        return { kind: "infra" };
      case "postgres":
      case "postgresql":
        return { kind: "infra", infraKind: "postgresql" };
      case "mysql":
        return { kind: "infra", infraKind: "mysql" };
      case "redis":
        return { kind: "infra", infraKind: "redis" };
      case "mongo":
      case "mongodb":
        return { kind: "infra", infraKind: "mongodb" };
      case "docker":
        return { kind: "infra", infraKind: "docker" };
      case "http":
      case "https":
        return { kind: "infra", infraKind: "http" };
    }
  }
  return null;
}

export function extractTagOnlyFilterQuery(query: string): string {
  return parseFilterTokens(query)
    .filter((token) => !resolveDirectPrototypeFilter(token))
    .join(" ");
}

export type ResourceFilterKind =
  | NonNullable<DirectPrototypeFilter>["kind"]
  | "ecs"
  | "trigger"
  | "stepfunctions";

export function matchesResourceType(kind: ResourceFilterKind, query: string): boolean {
  const direct = resolveDirectPrototypeFilter(query);
  return !direct || direct.kind === kind;
}

export function matchesResourceFilter(
  kind: ResourceFilterKind,
  query: string,
  tags?: Record<string, string>,
): boolean {
  return (
    matchesResourceType(kind, query) && matchesTagFilter(tags, extractTagOnlyFilterQuery(query))
  );
}

export function matchesInfrastructureFilter(normalizedKind: string, query: string): boolean {
  const direct = resolveDirectPrototypeFilter(query);
  return (
    !direct ||
    (direct.kind === "infra" && (!direct.infraKind || normalizedKind === direct.infraKind))
  );
}

export function matchesTagFilter(tags: Record<string, string> | undefined, query: string): boolean {
  const normalizedTokens = query
    .trim()
    .toLowerCase()
    .split(/\s+/)
    .map((token) => token.trim())
    .filter(Boolean);
  if (normalizedTokens.length === 0) return true;
  if (!tags || Object.keys(tags).length === 0) return false;

  return normalizedTokens.every((token) => matchesSingleTagFilter(tags, token));
}

function matchesSingleTagFilter(tags: Record<string, string>, normalized: string): boolean {
  if (!normalized) return true;

  const pairSeparator = normalized.includes(":") ? ":" : normalized.includes("=") ? "=" : "";
  if (pairSeparator) {
    const [rawKey, ...rest] = normalized.split(pairSeparator);
    const keyQuery = rawKey.trim();
    const valueQuery = rest.join(pairSeparator).trim();

    for (const [key, value] of Object.entries(tags)) {
      const keyLower = key.toLowerCase();
      const valueLower = value.toLowerCase();
      if (keyQuery && !keyLower.includes(keyQuery)) {
        continue;
      }
      if (valueQuery && !valueLower.includes(valueQuery)) {
        continue;
      }
      return true;
    }
    return false;
  }

  for (const [key, value] of Object.entries(tags)) {
    const keyLower = key.toLowerCase();
    const valueLower = value.toLowerCase();
    if (
      keyLower.includes(normalized) ||
      valueLower.includes(normalized) ||
      `${keyLower}:${valueLower}`.includes(normalized)
    ) {
      return true;
    }
  }
  return false;
}
