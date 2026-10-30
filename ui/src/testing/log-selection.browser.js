import { mount, tick, unmount } from "svelte";
import LogsSection from "#lib/components/sections/logs-section.svelte";

// In the dashboard preview: await (await import('/src/testing/log-selection.browser.js')).checkLogSelection()
export async function checkLogSelection({ closeWithBack = false } = {}) {
  const host = document.createElement("div");
  host.style.cssText =
    "position:fixed;inset:20px;z-index:10000;display:flex;flex-direction:column;background:var(--bg-stage)";
  document.body.append(host);
  const events = Array.from({ length: 200 }, (_, i) => ({
    timestamp: new Date(Date.UTC(2026, 9, 13, 12, 0, 200 - i)).toISOString(),
    level: "INFO",
    source: "output",
    streamName: "test-stream",
    message: `Selection anchor line ${i}`,
  }));
  const originalFetch = window.fetch;
  window.fetch = (input, init) => {
    const url = new URL(String(input), location.origin);
    if (url.pathname.includes("/logs/groups"))
      return Promise.resolve(
        Response.json([{ name: "selection-check", eventCount: 200, streamCount: 1 }]),
      );
    if (url.pathname.includes("/logs/events/"))
      return Promise.resolve(Response.json({ events, total: 200, nextCursor: "" }));
    return originalFetch(input, init);
  };
  let instance;
  const settle = async () => {
    await tick();
    await new Promise((r) => setTimeout(r, 100));
  };
  try {
    instance = mount(LogsSection, { target: host, props: { initialGroup: "selection-check" } });
    await settle();
    const before = host.querySelector(".log-scroller");
    if (!before) throw new Error("Log stream did not render");
    before.scrollTop = 2400;
    before.dispatchEvent(new Event("scroll"));
    await settle();
    const row = host.querySelector("#log-row-105");
    if (!row) throw new Error("Scrolled log row did not render");
    const top = before.scrollTop;
    const width = before.clientWidth;
    row.click();
    await settle();
    const after = host.querySelector(".log-scroller");
    const result = { before: top, after: after?.scrollTop, sameElement: before === after };
    if (!after || Math.abs(after.scrollTop - top) > 1)
      throw new Error(`Selection moved stream: ${JSON.stringify(result)}`);
    const close = host.querySelector('button[aria-label="Close detail panel"]');
    if (!close) throw new Error("Detail panel did not open");
    if (after.clientWidth >= width) throw new Error("Detail panel did not resize the reader");
    const back = host.querySelector('button[aria-label^="Back to log"]');
    if (closeWithBack && !back) throw new Error("Back control did not render");
    (closeWithBack ? back : close).click();
    await settle();
    const closed = host.querySelector(".log-scroller");
    if (!closed) throw new Error("Back opened log groups instead of the log list");
    if (closed !== before || Math.abs(closed.scrollTop - top) > 1)
      throw new Error("Closing detail moved stream");
    if (host.querySelector(".rc-detail")) throw new Error("Detail panel remained open");
    if (closeWithBack) {
      const backToGroups = host.querySelector('button[aria-label="Back to log groups"]');
      if (!backToGroups) throw new Error("List does not offer navigation back to groups");
      backToGroups.click();
      await settle();
      if (host.querySelector(".log-scroller"))
        throw new Error("Second back did not return to groups");
      if (!host.textContent.includes("Log groups")) throw new Error("Group list did not render");
    }
    return { ...result, passed: true };
  } finally {
    if (instance) await unmount(instance);
    window.fetch = originalFetch;
    host.remove();
  }
}
