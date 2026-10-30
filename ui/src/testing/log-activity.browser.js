import { mount, tick, unmount } from "svelte";
import LogsSection from "#lib/components/sections/logs-section.svelte";

// Run on a blank dev-server page to isolate the native viewer from dashboard polling.
const names = ["/aws/lambda/checkout", "/ecs/payments", "/tarn/api", "/tarn/system"];
const events = Array.from({ length: 480 }, (_, index) => ({
  timestamp: new Date(
    Date.UTC(2026, 9, 13, 12) +
      Math.round((index / 479 + 0.11 * Math.sin((index / 479) * Math.PI * 2)) * 898125),
  ).toISOString(),
  level:
    index >= 256 && index < 320 && index % 4 < 2 ? "ERROR" : index % 13 === 0 ? "WARN" : "INFO",
  source: index % 4 === 2 ? "api" : index % 4 === 3 ? "system" : "output",
  streamName: `${names[index % 4]}/request-worker`,
  message:
    [
      "Order accepted",
      "Payment authorised",
      "POST /checkout 200 42ms",
      "Worker heartbeat received",
    ][index % 4] + ` · order=${10420 + Math.floor(index / 4)}`,
}));
const settle = async () => {
  await tick();
  await new Promise((resolve) => setTimeout(resolve, 100));
};
const assert = (condition, message) => {
  if (!condition) throw new Error(message);
};

export async function mountLogActivityPreview(target = document.body) {
  const originalFetch = window.fetch;
  window.fetch = (input, init) => {
    const url = new URL(String(input), location.origin);
    if (url.pathname.endsWith("/logs/groups"))
      return Promise.resolve(
        Response.json(
          names.map((name) => ({
            name,
            eventCount: 120,
            streamCount: 1,
            createdAt: events[0].timestamp,
          })),
        ),
      );
    if (url.pathname.includes("/logs/events")) {
      let matching = events;
      const groupPath = url.pathname.split("/logs/events/")[1];
      const selected = groupPath
        ? [decodeURIComponent(groupPath)]
        : url.searchParams.get("groups")?.split(",");
      if (selected)
        matching = matching.filter((event) =>
          selected.some((name) => event.streamName.startsWith(`${name}/`)),
        );
      const levels = url.searchParams.get("level")?.split(",");
      if (levels) matching = matching.filter((event) => levels.includes(event.level));
      const pattern = url.searchParams.get("pattern");
      if (pattern)
        matching = matching.filter((event) =>
          event.message.toLowerCase().includes(pattern.toLowerCase()),
        );
      const total = matching.length;
      const descending = url.searchParams.get("order") !== "asc";
      const cursor = url.searchParams.get("cursor");
      if (cursor)
        matching = matching.filter((event) =>
          descending ? event.timestamp < cursor : event.timestamp > cursor,
        );
      const ordered = descending ? [...matching].reverse() : matching;
      const limit = Number(url.searchParams.get("limit") ?? 200);
      const page = ordered
        .slice(0, limit)
        .map((event) =>
          groupPath
            ? {
                ...event,
                streamName: event.streamName.slice(decodeURIComponent(groupPath).length + 1),
              }
            : event,
        );
      return Promise.resolve(
        Response.json({
          events: page,
          total,
          nextCursor: ordered.length > limit ? page.at(-1).timestamp : "",
        }),
      );
    }
    return originalFetch(input, init);
  };
  const instance = mount(LogsSection, { target, props: { initialGroup: "__all__" } });
  await settle();
  return async () => {
    await unmount(instance);
    window.fetch = originalFetch;
  };
}

export async function checkLogActivity() {
  const host = document.createElement("div");
  host.style.cssText =
    "position:fixed;inset:20px;display:flex;flex-direction:column;background:var(--bg-stage)";
  document.body.append(host);
  const cleanup = await mountLogActivityPreview(host);
  try {
    const scroller = host.querySelector(".log-scroller");
    assert(scroller, "Native log reader absent");
    assert(
      host.querySelector(".log-pin") && host.querySelector('button[title="Tail new events"]'),
      "Native pin or tail controls absent",
    );
    assert(
      host.querySelector(".log-activity h2").textContent === "Activity",
      "Overview is not the default",
    );
    scroller.scrollTop = 0;
    scroller.dispatchEvent(new Event("scroll"));
    await settle();
    const first = host.querySelector(".viewport-window").style.left;
    scroller.scrollTop = 2400;
    scroller.dispatchEvent(new Event("scroll"));
    await settle();
    const highlight = host.querySelector(".viewport-window");
    assert(highlight.style.left !== first, "Timeline did not follow native table scrolling");
    assert(getComputedStyle(highlight).borderBottomWidth === "0px", "Timeline underline remains");
    assert(
      getComputedStyle(host.querySelector(".viewport-key")).borderBottomWidth === "0px",
      "Legend underline remains",
    );
    const width = host.querySelector(".log-activity").clientWidth;
    host.querySelector("#log-row-105").click();
    await settle();
    assert(
      host.querySelector(".log-scroller") === scroller && scroller.scrollTop === 2400,
      "Opening native details moved table",
    );
    assert(
      host.querySelector(".rc-detail") && host.querySelector(".event-marker"),
      "Native details or timeline selection absent",
    );
    assert(
      host.querySelector(".log-activity").clientWidth === width,
      "Details narrowed the timeline",
    );
    host.querySelector('button[aria-label="Close detail panel"]').click();
    await settle();
    const height = scroller.clientHeight;
    host.querySelector(".compare-toggle").click();
    await settle();
    assert(host.querySelectorAll(".lane").length === 4, "Comparison does not show loaded groups");
    assert(
      scroller.scrollTop === 2400 && scroller.clientHeight === height,
      "Comparison moved or resized the native table",
    );
    const bucket = host.querySelector(".lane-slot:not(:disabled)");
    bucket.click();
    let previous = -1,
      stable = 0;
    for (let count = 0; count < 30 && stable < 3; count++) {
      await settle();
      stable = Math.abs(scroller.scrollTop - previous) < 0.1 ? stable + 1 : 0;
      previous = scroller.scrollTop;
    }
    assert(
      stable >= 3 && scroller.scrollTop !== 2400,
      "Comparison click did not navigate the native table",
    );
    host.querySelector(".compare-toggle").click();
    await settle();
    assert(host.querySelector(".pulse-chart"), "Overview did not return");
    return {
      passed: true,
      nativeTable: true,
      nativeControls: true,
      scrollTracking: true,
      noUnderline: true,
      nativeDetailsPreservePosition: true,
      timelineStaysFullWidth: true,
      comparisonPreservesHeight: true,
      comparisonNavigates: true,
    };
  } finally {
    await cleanup();
    host.remove();
  }
}
