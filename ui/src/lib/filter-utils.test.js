import { describe, expect, test } from "bun:test";
import {
  extractTagOnlyFilterQuery,
  matchesInfrastructureFilter,
  matchesResourceFilter,
  matchesResourceType,
} from "./filter-utils";

describe("shared resource filters", () => {
  const aliases = [
    ["function", ["lambda", "lambdas", "function", "functions"]],
    ["gateway", ["gateway", "gateways", "api", "apigateway", "api-gateway"]],
    ["queue", ["queue", "queues", "sqs"]],
    ["topic", ["topic", "topics", "sns"]],
    ["secret", ["secret", "secrets"]],
    ["userpool", ["cognito", "userpool", "userpools", "user-pool"]],
    ["bucket", ["bucket", "buckets", "s3", "storage"]],
    ["dynamodb", ["dynamodb", "dynamo", "ddb", "table", "tables", "stream", "streams"]],
    ["eventbridge", ["eventbridge", "event-bridge", "schedule", "schedules", "rule", "rules"]],
  ];
  for (const [kind, tokens] of aliases) {
    test(`${kind} type aliases select untagged resources and exclude other kinds`, () => {
      for (const token of tokens) {
        expect(matchesResourceFilter(kind, token.toUpperCase())).toBe(true);
        expect(
          matchesResourceFilter(kind === "function" ? "queue" : "function", token, { team: "dev" }),
        ).toBe(false);
      }
    });
  }
  test("type tokens are removed before matching all remaining tag terms", () => {
    expect(
      matchesResourceFilter("function", "lambda TEAM=DEV env:local", { team: "dev", env: "local" }),
    ).toBe(true);
    expect(
      matchesResourceFilter("function", "lambda team=dev env:local", {
        team: "prod",
        env: "local",
      }),
    ).toBe(false);
    expect(matchesResourceFilter("function", "lambda team=dev")).toBe(false);
    expect(matchesResourceFilter("function", "team:dev", { team: "development" })).toBe(true);
  });
  test("explicit tag values that look like resource types remain tag filters", () => {
    expect(extractTagOnlyFilterQuery("lambda service=sqs team:dev")).toBe("service=sqs team:dev");
    expect(matchesResourceFilter("function", "service=lambda", { service: "lambda" })).toBe(true);
    expect(matchesResourceFilter("queue", "service=lambda", { service: "lambda" })).toBe(true);
  });
  test("clearing a filter includes tagged and untagged resources", () => {
    expect(matchesResourceFilter("function", " ")).toBe(true);
    expect(matchesResourceFilter("queue", "", { team: "dev" })).toBe(true);
  });
  test("the first type token keeps the existing selection policy", () => {
    expect(matchesResourceFilter("function", "lambda sqs team:dev", { team: "dev" })).toBe(true);
    expect(matchesResourceFilter("queue", "lambda sqs team:dev", { team: "dev" })).toBe(false);
  });
  test("sections without tag filtering still exclude other resource types", () => {
    for (const kind of ["ecs", "trigger", "stepfunctions"]) {
      expect(matchesResourceType(kind, "lambda")).toBe(false);
      expect(matchesResourceType(kind, "team:dev")).toBe(true);
    }
  });
  test("infrastructure aliases narrow services consistently", () => {
    expect(matchesInfrastructureFilter("postgresql", "postgres")).toBe(true);
    expect(matchesInfrastructureFilter("redis", "postgres")).toBe(false);
    expect(matchesInfrastructureFilter("redis", "infra")).toBe(true);
    expect(matchesInfrastructureFilter("redis", "lambda")).toBe(false);
    expect(matchesInfrastructureFilter("redis", "team:dev")).toBe(true);
  });
});
