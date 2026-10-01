import { afterEach, expect, spyOn, test } from "bun:test";
import { putEventBridgeRule } from "./api";
import { ruleDefinitionError } from "./eventbridge-rule";

let fetchSpy;
afterEach(() => fetchSpy?.mockRestore());

test("saving a pattern rule sends the existing pattern with its edited settings", async () => {
  fetchSpy = spyOn(globalThis, "fetch").mockResolvedValue(
    Response.json({ RuleArn: "audit-rule-arn" }),
  );
  const eventPattern = '{"source":["audit.orders"]}';
  await putEventBridgeRule({
    name: "order-rule",
    scheduleExpression: "",
    eventPattern,
    description: "edited",
    state: "DISABLED",
  });
  expect(JSON.parse(fetchSpy.mock.calls[0][1].body)).toMatchObject({
    Name: "order-rule",
    EventPattern: eventPattern,
    ScheduleExpression: "",
    Description: "edited",
    State: "DISABLED",
  });
});

test("scheduled rules retain their schedule without requiring a pattern", async () => {
  fetchSpy = spyOn(globalThis, "fetch").mockResolvedValue(
    Response.json({ RuleArn: "audit-rule-arn" }),
  );
  await putEventBridgeRule({
    name: "scheduled",
    scheduleExpression: "rate(5 minutes)",
    eventPattern: "",
  });
  expect(JSON.parse(fetchSpy.mock.calls[0][1].body)).toMatchObject({
    ScheduleExpression: "rate(5 minutes)",
    EventPattern: "",
  });
});

test("rule definitions require exactly one schedule or pattern", () => {
  expect(ruleDefinitionError({ scheduleExpression: " ", eventPattern: " " })).toContain("Enter");
  expect(
    ruleDefinitionError({ scheduleExpression: "rate(5 minutes)", eventPattern: "{}" }),
  ).toContain("not both");
  expect(
    ruleDefinitionError({ scheduleExpression: "rate(5 minutes)", eventPattern: "" }),
  ).toBeNull();
  expect(ruleDefinitionError({ scheduleExpression: "", eventPattern: "{}" })).toBeNull();
  expect(
    ruleDefinitionError({ scheduleExpression: "", eventPattern: '{"source":["audit"]}' }),
  ).toBeNull();
});

test("malformed schedules and non-object patterns cannot be submitted", () => {
  expect(ruleDefinitionError({ scheduleExpression: "invalid", eventPattern: "" })).not.toBeNull();
  for (const eventPattern of ["{", "null", "[]", '"audit"', "42"]) {
    expect(ruleDefinitionError({ scheduleExpression: "", eventPattern })).not.toBeNull();
  }
});
