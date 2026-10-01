import { refresh, stopPolling } from "$lib/state.svelte";

// Run in a dashboard dev-server tab:
// await (await import('/src/testing/chaos-selection.browser.js')).checkChaosSelection()
export async function checkChaosSelection() {
  const originalHash = location.hash;
  const originalFetch = window.fetch;
  let routeKeys = [];
  const overview = () => ({
    status: "ok",
    timestamp: "2026-10-01T00:00:00Z",
    services: [],
    config: { accountId: "111111111111", region: "us-east-1", uiEnabled: true },
    counts: {
      gateways: routeKeys.length ? 1 : 0,
      functions: 0,
      queues: 0,
      topics: 0,
      subscriptions: 0,
      secrets: 0,
      buckets: 0,
      logGroups: 0,
      eventSourceMappings: 0,
    },
    gateways: routeKeys.length
      ? [
          {
            apiId: "audit-chaos-api",
            name: "audit-chaos-gateway",
            protocolType: "HTTP",
            version: "v2",
            arn: "arn:aws:apigateway:us-east-1::/apis/audit-chaos-api",
            apiEndpoint: "http://audit.invalid",
            defaultStage: "$default",
            invokeUrl: "http://audit.invalid",
            routes: routeKeys.length,
            integrations: 1,
            stages: 1,
            tagCount: 0,
            routeKeys,
            routeDetails: routeKeys.map((path) => ({
              routeKey: `GET ${path}`,
              method: "GET",
              path,
            })),
          },
        ]
      : [],
    functions: [],
    queues: [],
    topics: [],
    subscriptions: [],
    secrets: [],
    buckets: [],
    logGroups: [],
    eventSourceMappings: [],
    infrastructure: [],
    recentTraces: [],
  });
  const main = () => document.querySelector("main");
  const button = (label) =>
    [...main().querySelectorAll("button")].find((element) => element.textContent.trim() === label);
  const settle = () => new Promise((resolve) => setTimeout(resolve, 30));
  const waitFor = async (predicate, message) => {
    const deadline = Date.now() + 3000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error(message);
      await settle();
    }
  };
  const assertSelection = (count, message) => {
    const probe = button(count ? `Probe (${count})` : "Probe");
    const checked = main().querySelectorAll('.route-row button[aria-label="Deselect"]');
    if (!probe || probe.disabled !== (count === 0) || checked.length !== count) {
      throw new Error(message);
    }
  };
  const loadRoutes = async (routes) => {
    routeKeys = routes;
    await refresh();
    await settle();
  };

  stopPolling();
  window.fetch = async (input, init) => {
    const url = new URL(String(input), location.origin);
    if (!url.pathname.includes("/_tarn/")) return originalFetch(input, init);
    if (url.pathname.endsWith("/overview")) return Response.json(overview());
    // No probe or backend mutation is allowed by this fixture.
    throw new Error(`Unexpected Chaos request: ${url.pathname}`);
  };

  try {
    location.hash = "#functions";
    await waitFor(() => main()?.textContent.includes("Functions"), "Chaos did not unmount");
    await loadRoutes([]);
    location.hash = "#chaos";
    await waitFor(() => main()?.textContent.includes("Chaos probe"), "Chaos did not mount");
    assertSelection(0, "Empty initial overview selected routes");

    await loadRoutes(["/audit-first"]);
    assertSelection(1, "Routes arriving after mount were not initially selected");
    button("Clear").click();
    await settle();
    assertSelection(0, "Clear immediately reselected every route");
    await loadRoutes(["/audit-first"]);
    assertSelection(0, "An ordinary refresh reselected cleared routes");

    button("Select all").click();
    await settle();
    assertSelection(1, "Select all did not select the route");
    main().querySelector('.route-row button[aria-label="Deselect"]').click();
    await settle();
    assertSelection(0, "Deselecting the last route immediately selected it again");

    button("Select all").click();
    await settle();
    await loadRoutes(["/audit-first", "/audit-second"]);
    assertSelection(1, "A new route changed the existing selection");
    button("Select all").click();
    await settle();
    assertSelection(2, "Select all did not include the new route");
    await loadRoutes(["/audit-second"]);
    assertSelection(1, "A removed route remained selected");
    await loadRoutes([]);
    assertSelection(0, "Removing all routes left a phantom selection");
    await loadRoutes(["/audit-second"]);
    assertSelection(0, "Reappearing routes reset the user's empty selection");
    return {
      passed: [
        "initial selection after delayed data",
        "Clear stays empty across polls",
        "last route can be deselected",
        "Select all includes new routes",
        "removed routes are pruned",
      ],
    };
  } finally {
    window.fetch = originalFetch;
    location.hash = originalHash;
    await refresh();
  }
}
