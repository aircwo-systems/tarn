export type DirectPrototypeFilter =
  | {
      kind:
        | "gateway"
        | "eventbridge"
        | "topic"
        | "queue"
        | "dynamodb"
        | "function"
        | "secret"
        | "bucket"
        | "extension"
        | "infra";
      infraKind?: string;
    }
  | null;

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
