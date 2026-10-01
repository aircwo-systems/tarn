import { mount, tick, unmount } from "svelte";
import Page from "../routes/+page.svelte";
import EventBridgeSection from "$lib/components/sections/eventbridge-section.svelte";
import {
  getAccountSettings,
  getDashboardFilters,
  refresh,
  setDashboardTagFilter,
} from "$lib/state.svelte";

// Run these exports in a blank HTML page served by the dashboard dev server.
// API calls are intercepted; no rule is changed on a live backend.
const date = "2026-10-01T00:00:00Z";
const fn = (name, team) => ({
  name,
  arn: `arn:aws:lambda:us-east-1:111111111111:function:${name}`,
  runtime: "nodejs22.x",
  state: "Active",
  timeoutSec: 3,
  memoryMB: 128,
  codeSize: 0,
  messagesProcessed: 0,
  invocations: 0,
  version: "$LATEST",
  lastModified: date,
  layers: 0,
  tags: { team },
  tagCount: 1,
});
const overview = (rules = []) => ({
  status: "ok",
  timestamp: date,
  services: [],
  config: {
    accountId: getAccountSettings().activeAccountId,
    region: "us-east-1",
    endpoint: "",
    dataDir: "",
    uiEnabled: true,
  },
  counts: {
    gateways: 0,
    functions: 2,
    queues: 0,
    topics: 0,
    subscriptions: 0,
    secrets: 0,
    buckets: 0,
    logGroups: 0,
    eventSourceMappings: 0,
    eventBridgeRules: rules.length,
  },
  gateways: [],
  functions: [fn("audit-dev", "dev"), fn("audit-prod", "prod")],
  queues: [],
  topics: [],
  subscriptions: [],
  secrets: [],
  buckets: [],
  eventSourceMappings: [],
  eventBridgeRules: rules,
  infrastructure: [],
  recentTraces: [],
});
const assert = (condition, message) => {
  if (!condition) throw new Error(message);
};
const settle = async () => {
  await tick();
  await new Promise((resolve) => setTimeout(resolve, 30));
};
const waitFor = async (condition, message) => {
  const deadline = Date.now() + 3000;
  while (!condition()) {
    if (Date.now() > deadline) throw new Error(message);
    await settle();
  }
};

async function withDashboard(component, data, respond, check, hash) {
  const originalFetch = window.fetch;
  const originalHash = location.hash;
  const originalFilter = getDashboardFilters().tagFilter;
  const host = document.createElement("div");
  host.style.cssText = "height:100vh;overflow:auto";
  document.body.append(host);
  let instance;
  window.fetch = (input, init) => {
    const url = new URL(String(input), location.origin);
    if (url.pathname.endsWith("/overview")) return Promise.resolve(Response.json(data));
    if (new Headers(init?.headers).get("x-amz-target")) return Promise.resolve(respond(init));
    if (url.pathname.includes("/_tarn/"))
      return Promise.resolve(Response.json({ events: [], total: 0, variables: {} }));
    if (url.pathname.includes("/_s3/"))
      return Promise.resolve(
        new Response("<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>"),
      );
    return originalFetch(input, init);
  };
  try {
    setDashboardTagFilter("");
    history.replaceState(null, "", hash);
    await refresh();
    instance = mount(component, { target: host });
    await settle();
    return await check(host);
  } finally {
    if (instance) await unmount(instance);
    setDashboardTagFilter(originalFilter);
    history.replaceState(null, "", originalHash || "#");
    window.fetch = originalFetch;
    host.remove();
  }
}

export async function checkResourceFilters() {
  return withDashboard(
    Page,
    overview(),
    () => Response.json({}),
    async (host) => {
      const main = () => host.querySelector("main").textContent;
      const sidebar = () =>
        [...host.querySelectorAll("aside nav button")].find((button) =>
          button.textContent.trim().startsWith("Functions"),
        );
      for (const [query, count] of [
        ["lambda", 2],
        ["FUNCTIONS", 2],
        ["lambda team=dev", 1],
        ["team:prod", 1],
        ["sqs", 0],
        ["", 2],
      ]) {
        setDashboardTagFilter(query);
        await settle();
        assert(
          main().includes(`${count} function`),
          `Filter ${JSON.stringify(query)} disagrees with the resource list: expected ${count} functions`,
        );
        if (count)
          assert(
            sidebar().textContent.includes(String(count)),
            `Sidebar count disagrees for ${JSON.stringify(query)}`,
          );
        if (count === 1)
          assert(
            main().includes(query.includes("dev") ? "audit-dev" : "audit-prod"),
            "Tag matching selected the wrong resource",
          );
      }
      return {
        passed: [
          "type aliases match counts and lists",
          "type plus tag filters",
          "tag-only filters",
          "other resource types excluded",
          "clearing restores resources",
        ],
      };
    },
    "#functions",
  );
}

export async function checkEventBridgePatternEdits() {
  const eventPattern = '{"source":["audit.orders"]}';
  const rule = {
    name: "audit-rule",
    arn: "arn:aws:events:us-east-1:111111111111:rule/audit-rule",
    scheduleExpression: "",
    eventPattern,
    description: "original",
    state: "ENABLED",
    targets: [],
  };
  const data = overview([rule]);
  const saves = [];
  const respond = (init) => {
    const body = JSON.parse(init.body);
    saves.push(body);
    if (!body.ScheduleExpression && !body.EventPattern)
      return Response.json(
        {
          __type: "ValidationException",
          message: "Either ScheduleExpression or EventPattern is required",
        },
        { status: 400 },
      );
    Object.assign(rule, {
      eventPattern: body.EventPattern ?? "",
      scheduleExpression: body.ScheduleExpression ?? "",
      description: body.Description,
      state: body.State,
    });
    return Response.json({ RuleArn: rule.arn });
  };
  return withDashboard(
    EventBridgeSection,
    data,
    respond,
    async (host) => {
      const button = (text) =>
        [...host.querySelectorAll("button")].find((el) => el.textContent.trim() === text);
      const set = async (element, value) => {
        element.value = value;
        element.dispatchEvent(new Event("input", { bubbles: true }));
        await settle();
      };
      await set(host.querySelector('input[placeholder="What this rule is for"]'), "edited");
      host.querySelector('input[type="checkbox"]').click();
      await settle();
      button("Save changes").click();
      await waitFor(() => saves.length === 1, "Rule edit did not submit");
      assert(
        saves[0].EventPattern === eventPattern,
        "Editing a pattern rule omitted its existing event pattern",
      );
      assert(saves[0].State === "DISABLED", "Pattern edit lost the enabled setting");
      await waitFor(
        () => !button("Save changes") && !button("Saving…"),
        "Successful save did not reset the form",
      );
      assert(!host.querySelector(".eventbridge > .error"), "Pattern edit was rejected");
      assert(
        host.querySelector("textarea").value === eventPattern,
        "Saved pattern was not exposed in settings",
      );
      const updatedPattern = '{"source":["audit.payments"]}';
      await set(host.querySelector("textarea"), updatedPattern);
      button("Discard").click();
      await settle();
      assert(
        host.querySelector("textarea").value === eventPattern,
        "Discard did not restore the saved pattern",
      );
      await set(host.querySelector("textarea"), updatedPattern);
      button("Save changes").click();
      await waitFor(() => saves.length === 2, "Pattern edit did not submit");
      assert(saves[1].EventPattern === updatedPattern, "Edited event pattern was not sent");
      await waitFor(
        () => !button("Save changes") && !button("Saving…"),
        "Pattern-only save did not reset the form",
      );
      return {
        passed: [
          "existing pattern loaded",
          "description edit preserves pattern",
          "successful save resets form",
          "enabled setting preserved",
          "discard restores pattern",
          "pattern-only edits save",
        ],
      };
    },
    "#eventbridge?rule=audit-rule",
  );
}

export async function checkUntaggedResourceFilters() {
  const data = overview([
    {
      name: "audit-schedule",
      arn: "arn:aws:events:us-east-1:111111111111:rule/audit-schedule",
      scheduleExpression: "rate(5 minutes)",
      state: "ENABLED",
      targets: [],
    },
  ]);
  data.buckets = [{ name: "audit-bucket", objects: 0, totalSize: 0, createdDate: date }];
  data.dynamodbTables = [
    {
      name: "audit-table",
      arn: "arn:aws:dynamodb:us-east-1:111111111111:table/audit-table",
      status: "ACTIVE",
      createdDate: date,
      itemCount: 0,
      keySchema: "id (HASH)",
      localIndexes: 0,
      globalIndexes: 0,
      streamEnabled: false,
    },
  ];
  data.counts.buckets = 1;
  data.counts.dynamodbTables = 1;
  return withDashboard(
    Page,
    data,
    () => Response.json({}),
    async (host) => {
      const main = () => host.querySelector("main").textContent;
      for (const [tab, token, noun, name] of [
        ["storage", "s3", "bucket", "audit-bucket"],
        ["dynamodb", "ddb", "table", "audit-table"],
        ["eventbridge", "rules", "rule", "audit-schedule"],
      ]) {
        setDashboardTagFilter(token);
        location.hash = `#${tab}`;
        await waitFor(
          () => main().includes(`1 ${noun}`) && main().includes(name),
          `${tab} did not include its own resource type`,
        );
        setDashboardTagFilter("lambda");
        await waitFor(
          () => main().includes(`0 ${noun}`) && !main().includes(name),
          `${tab} displayed resources excluded by its sidebar count`,
        );
        setDashboardTagFilter(token);
        await waitFor(() => main().includes(name), `${tab} did not restore its resources`);
      }
      return {
        passed: [
          "storage type filtering",
          "DynamoDB type filtering",
          "EventBridge type filtering",
          "unrelated types excluded and restored",
        ],
      };
    },
    "#storage",
  );
}

export async function checkEventBridgeRuleValidation() {
  const data = overview();
  const saves = [];
  const respond = (init) => {
    const body = JSON.parse(init.body);
    saves.push(body);
    const arn = `arn:aws:events:us-east-1:111111111111:rule/${body.Name}`;
    data.eventBridgeRules.push({
      name: body.Name,
      arn,
      scheduleExpression: body.ScheduleExpression,
      eventPattern: body.EventPattern,
      description: body.Description,
      state: body.State,
      targets: [],
    });
    return Response.json({ RuleArn: arn });
  };
  return withDashboard(
    EventBridgeSection,
    data,
    respond,
    async (host) => {
      const button = (text) =>
        [...host.querySelectorAll("button")].find((el) => el.textContent.trim() === text);
      const set = async (element, value) => {
        element.value = value;
        element.dispatchEvent(new Event("input", { bubbles: true }));
        await settle();
      };
      button("Create your first rule").click();
      await settle();
      await set(host.querySelector('input[placeholder="nightly-cleanup"]'), "audit-pattern-create");
      await set(host.querySelector("#rule-schedule"), "");
      assert(button("Create rule").disabled, "A rule without a schedule or pattern was allowed");
      assert(
        host.textContent.includes("Enter a schedule or an event pattern"),
        "Missing definition did not show a validation message",
      );
      for (const invalid of ["{", "[]"]) {
        await set(host.querySelector("textarea"), invalid);
        assert(button("Create rule").disabled, "An invalid event pattern was allowed");
      }
      const eventPattern = '{"source":["audit.created"]}';
      await set(host.querySelector("textarea"), eventPattern);
      assert(!button("Create rule").disabled, "A valid pattern rule was blocked");
      await set(host.querySelector("#rule-schedule"), "rate(5 minutes)");
      assert(
        button("Create rule").disabled && host.textContent.includes("not both"),
        "A rule with both definition types was allowed",
      );
      await set(host.querySelector("#rule-schedule"), "");
      button("Create rule").click();
      await waitFor(() => saves.length === 1, "Pattern rule creation did not submit");
      assert(
        saves[0].EventPattern === eventPattern && saves[0].ScheduleExpression === "",
        "Pattern creation sent the wrong definition",
      );
      await waitFor(
        () => !host.querySelector('input[placeholder="nightly-cleanup"]'),
        "Pattern creation did not close the form",
      );
      button("New rule").click();
      await settle();
      await set(
        host.querySelector('input[placeholder="nightly-cleanup"]'),
        "audit-scheduled-create",
      );
      button("Create rule").click();
      await waitFor(() => saves.length === 2, "Scheduled rule creation did not submit");
      assert(
        saves[1].ScheduleExpression === "rate(5 minutes)" && saves[1].EventPattern === "",
        "Scheduled creation inherited a pattern",
      );
      return {
        passed: [
          "empty definitions blocked",
          "invalid JSON and arrays blocked",
          "both definitions blocked",
          "pattern creation",
          "scheduled creation",
        ],
      };
    },
    "#eventbridge",
  );
}
