import { mount, tick, unmount } from "svelte";
import { fromStore, writable } from "svelte/store";
import BucketDetail from "$lib/components/s3/bucket-detail.svelte";
import LogsSection from "$lib/components/sections/logs-section.svelte";
import FunctionInvocations from "$lib/components/functions/function-invocations.svelte";
import { invocationsFromTraces } from "$lib/function-links";

// Run each exported check in a blank HTML page served by the dashboard dev server.
// This avoids background dashboard requests. All API responses are synthetic.
const waitFor = async (predicate, message) => {
  const deadline = Date.now() + 5000;
  while (!predicate()) {
    if (Date.now() > deadline) throw new Error(message);
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
};
const settle = async () => {
  await tick();
  await new Promise((resolve) => setTimeout(resolve, 30));
};
const assert = (condition, message) => {
  if (!condition) throw new Error(message);
};

async function withComponent(component, props, respond, check) {
  const host = document.createElement("div");
  host.style.cssText =
    "position:fixed;inset:8px;z-index:10000;display:flex;flex-direction:column;background:var(--bg-surface);overflow:auto";
  document.body.append(host);
  const originalFetch = window.fetch;
  window.fetch = (input, init) => {
    const url = new URL(String(input), location.origin);
    if (url.pathname.startsWith("/_s3/") || url.pathname.startsWith("/_tarn/")) {
      return Promise.resolve(respond(url, init) ?? new Response(null, { status: 503 }));
    }
    return originalFetch(input, init);
  };
  let instance;
  try {
    instance = mount(component, { target: host, props });
    return await check(host);
  } finally {
    if (instance) await unmount(instance);
    window.fetch = originalFetch;
    host.remove();
  }
}

const bucket = {
  name: "audit-bucket",
  objects: 2,
  totalSize: 2,
  createdDate: "2026-10-01T00:00:00Z",
};
const listing = (key) =>
  new Response(
    `<ListBucketResult><IsTruncated>false</IsTruncated><CommonPrefixes><Prefix>folder/</Prefix></CommonPrefixes><Contents><Key>${key}</Key><Size>1</Size></Contents></ListBucketResult>`,
  );

export async function checkBucketListing() {
  const store = writable({ bucket, prefix: "" });
  const route = fromStore(store);
  const pending = [];
  let completed = false;
  const props = {
    get bucket() {
      return route.current.bucket;
    },
    get prefix() {
      return route.current.prefix;
    },
    selectedKey: null,
    onnavigate: (prefix) => store.update((value) => ({ ...value, prefix })),
  };
  const request = (url, init) =>
    new Promise((resolve) => {
      pending.push({ prefix: url.searchParams.get("prefix") ?? "", signal: init?.signal, resolve });
    });
  try {
    return await withComponent(BucketDetail, props, request, async (host) => {
      await waitFor(() => pending.length === 1, "Initial bucket listing did not start");
      pending[0].resolve(listing("initial-root.txt"));
      await waitFor(
        () => host.textContent.includes("initial-root.txt"),
        "Initial listing did not render",
      );
      const navigate = async (prefix) => {
        store.update((value) => ({ ...value, prefix }));
        await settle();
      };
      await navigate("folder/");
      await waitFor(() => pending.length === 2, "Folder listing did not start");
      await navigate("");
      await waitFor(() => pending.length === 3, "New root listing did not start");
      pending[2].resolve(listing("new-root.txt"));
      await waitFor(
        () => host.textContent.includes("new-root.txt"),
        "New root listing did not render",
      );
      pending[1].resolve(listing("folder/stale.txt"));
      await settle();
      assert(
        host.textContent.includes("new-root.txt") && !host.textContent.includes("stale.txt"),
        "A delayed folder response replaced the current root listing",
      );
      assert(pending[1].signal?.aborted, "Superseded folder request was not cancelled");

      await navigate("folder/");
      await navigate("");
      await waitFor(() => pending.length === 5, "Second navigation did not reload");
      pending[3].resolve(new Response(null, { status: 503 }));
      await settle();
      assert(!host.querySelector(".error"), "A stale folder error appeared in the current root");
      assert(
        host.querySelector(".spin"),
        "Stale cleanup stopped the current request's loading indicator",
      );
      pending[4].resolve(listing("latest-root.txt"));
      await waitFor(
        () => host.textContent.includes("latest-root.txt"),
        "Current request did not finish",
      );
      assert(!host.querySelector(".spin"), "Current request left the loading indicator running");

      store.update((value) => ({ ...value, bucket: { ...bucket } }));
      await settle();
      assert(pending.length === 5, "An unchanged overview poll reloaded the listing");
      host.querySelector('button[title="Refresh"]').click();
      await waitFor(() => pending.length === 6, "Manual refresh did not reload");
      pending[5].resolve(new Response(null, { status: 503 }));
      await waitFor(() => host.querySelector(".error"), "Current errors were hidden");
      await navigate("folder/");
      await waitFor(() => pending.length === 7, "Final request did not start");
      completed = true;
      return {
        passed: [
          "late success ignored",
          "late error ignored",
          "loading belongs to current request",
          "unchanged polls preserve listing",
          "manual refresh and current errors",
        ],
      };
    });
  } finally {
    const last = pending.at(-1);
    for (const request of pending) request.resolve(listing("cleanup.txt"));
    if (completed)
      assert(last?.signal?.aborted, "Unmount did not cancel the pending listing request");
  }
}

export async function checkLiveLogs(scope = "audit-a", order = "asc") {
  const events = Array.from({ length: 201 }, (_, i) => ({
    timestamp: new Date(Date.UTC(2026, 9, 1, 10, 0, i)).toISOString(),
    level: "INFO",
    message: `audit-event-${i}`,
    streamName: "audit-a/stream",
  }));
  const requests = [];
  const props = {
    initialGroup: scope,
    initialOrder: order,
    initialPattern: "audit",
    initialLevels: ["INFO"],
    initialStream: "audit-a/stream",
  };
  return withComponent(
    LogsSection,
    props,
    (url) => {
      if (url.pathname.endsWith("/logs/groups"))
        return Response.json([
          { name: "audit-a", eventCount: events.length },
          { name: "audit-b", eventCount: 0 },
        ]);
      if (url.pathname.includes("/logs/events")) {
        requests.push(url);
        const sorted = url.searchParams.get("order") === "asc" ? events : [...events].reverse();
        return Response.json({
          events: sorted.slice(0, Number(url.searchParams.get("limit"))),
          total: events.length,
          nextCursor: "synthetic-next",
        });
      }
    },
    async (host) => {
      await waitFor(() => host.textContent.includes("200 of 201"), "Initial log page did not load");
      assert(requests[0].searchParams.get("order") === order, "Initial page ignored display order");
      events.push({
        timestamp: "2026-10-01T10:03:21Z",
        level: "INFO",
        message: "audit-new-event",
        streamName: "audit-a/stream",
      });
      await settle();
      const initialRequests = requests.length;
      host.querySelector('button[title="Tail new events"]').click();
      await waitFor(() => requests.length > initialRequests, "Live tail did not poll");
      await settle();
      const poll = requests.at(-1);
      assert(
        poll.searchParams.get("order") === "desc",
        "Oldest-first live tail fetched the oldest page again",
      );
      assert(!poll.searchParams.has("cursor"), "Live tail reused the historical page cursor");
      assert(
        poll.searchParams.get("pattern") === "audit" &&
          poll.searchParams.get("level") === "INFO" &&
          poll.searchParams.get("stream") === "audit-a/stream",
        "Live tail lost its filters",
      );
      if (scope.includes(","))
        assert(poll.searchParams.get("groups") === scope, "Live tail lost the selected groups");
      const scroller = host.querySelector(".log-scroller");
      scroller.scrollTop = order === "asc" ? scroller.scrollHeight : 0;
      scroller.dispatchEvent(new Event("scroll"));
      await waitFor(
        () => host.textContent.includes("audit-new-event"),
        "New events were not displayed in the live buffer",
      );
      const times = [...host.querySelectorAll(".log-msg")].map((el) => el.textContent.trim());
      assert(
        order === "asc" ? times.at(-1) === "audit-new-event" : times[0] === "audit-new-event",
        "Live merge changed the requested display order",
      );
      return {
        scope,
        order,
        passed: [
          "newest events fetched",
          "new event rendered",
          "display order preserved",
          "filters preserved",
        ],
      };
    },
  );
}

export async function checkRepeatedInvocations() {
  const fn = {
    name: "audit-function",
    arn: "arn:aws:lambda:us-east-1:111111111111:function:audit-function",
  };
  const input = [
    {
      id: "audit-trace",
      startedAt: "2026-10-01T10:00:00Z",
      durationMs: 40,
      status: 200,
      spans: [
        { kind: "lambda", name: fn.name, durationMs: 10, status: "ok" },
        { kind: "lambda", name: fn.name, durationMs: 30, status: "error" },
      ],
    },
  ];
  const invocations = invocationsFromTraces(fn, input);
  assert(invocations.length === 2, "Repeated Lambda invocations in one trace were dropped");
  const opened = [];
  return withComponent(
    FunctionInvocations,
    { invocations, timeoutSec: 30, onopen: (id) => opened.push(id) },
    () => undefined,
    async (host) => {
      await settle();
      const bars = host.querySelectorAll("button.bar");
      const rows = host.querySelectorAll("button.row");
      assert(
        bars.length === 2 && rows.length === 2,
        "Repeated invocations did not render distinct rows and bars",
      );
      assert(
        host.querySelectorAll("button.row.err").length === 1,
        "The second invocation's failure was lost",
      );
      rows[0].dispatchEvent(new MouseEvent("mouseenter"));
      await tick();
      assert(
        host.querySelectorAll("button.row.hot").length === 1 &&
          host.querySelectorAll("button.bar.hot").length === 1,
        "Hovering one invocation highlighted every invocation in the trace",
      );
      rows[1].click();
      assert(opened[0] === "audit-trace", "Invocation did not open its parent trace");
      return {
        passed: [
          "two rows and bars",
          "second failure visible",
          "hover identity isolated",
          "parent trace opens",
        ],
      };
    },
  );
}
