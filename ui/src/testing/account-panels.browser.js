import { getAccountSettings, refresh, stopPolling, switchAccount } from "$lib/state.svelte";

// Run in a dashboard dev-server tab:
// await (await import('/src/testing/account-panels.browser.js')).checkAccountPanels()
// All backend requests are intercepted and use synthetic values.
export async function checkAccountPanels() {
  const accountA = "111111111111";
  const accountB = "222222222222";
  const originalAccount = getAccountSettings().activeAccountId;
  const originalHash = location.hash;
  const savedAccounts = localStorage.getItem("tarn-accounts");
  const originalFetch = window.fetch;
  const requests = [];
  const date = "2026-10-01T00:00:00Z";

  const overview = (accountId) => ({
    status: "ok",
    timestamp: date,
    services: [],
    config: { accountId, region: "us-east-1", endpoint: "", dataDir: "", uiEnabled: true },
    counts: {
      gateways: 0,
      functions: 1,
      queues: 0,
      topics: 0,
      subscriptions: 0,
      secrets: 1,
      buckets: 0,
      logGroups: 1,
      eventSourceMappings: 0,
      eventBridgeRules: 0,
    },
    gateways: [],
    queues: [],
    topics: [],
    subscriptions: [],
    buckets: [],
    eventSourceMappings: [],
    infrastructure: [],
    recentTraces: [],
    functions: [
      {
        name: "audit-shared-function",
        arn: `arn:aws:lambda:us-east-1:${accountId}:function:audit-shared-function`,
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
        tags: {},
        tagCount: 0,
        environmentKeys: ["AUDIT_VALUE"],
      },
    ],
    secrets: [
      {
        name: "audit-shared-secret",
        arn: `arn:aws:secretsmanager:us-east-1:${accountId}:secret:audit-shared-secret`,
        versionId: "synthetic",
        tags: {},
        tagCount: 0,
        createdDate: date,
        lastChangedDate: date,
      },
    ],
  });

  const visibleText = () => document.querySelector("main")?.textContent ?? "";
  const button = (label) =>
    [...document.querySelectorAll("main button")].find(
      (element) => element.textContent.trim() === label,
    );
  const waitFor = async (predicate, message) => {
    const deadline = Date.now() + 3000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error(message);
      await new Promise((resolve) => setTimeout(resolve, 20));
    }
  };
  const selectAccount = async (id) => {
    switchAccount(id);
    await refresh();
    await new Promise((resolve) => setTimeout(resolve, 20));
  };
  const showTab = async (tab, title) => {
    location.hash = `#${tab}`;
    await waitFor(() => visibleText().includes(title), `${tab} did not mount`);
  };
  const reveal = async (value) => {
    await waitFor(() => button("Reveal"), "Reveal button is missing");
    button("Reveal").click();
    await waitFor(() => visibleText().includes(value), `Missing synthetic value ${value}`);
  };
  const assertHidden = (value) => {
    if (visibleText().includes(value) || !button("Reveal")) {
      throw new Error("A revealed value survived an account switch");
    }
  };

  stopPolling();
  window.fetch = async (input, init) => {
    const url = new URL(String(input), location.origin);
    if (!url.pathname.includes("/_tarn/")) return originalFetch(input, init);
    const account =
      new Headers(init?.headers).get("Authorization")?.match(/Credential=(\d+)\//)?.[1] ??
      "000000000000";
    requests.push({ path: url.pathname, account });
    if (url.pathname.endsWith("/overview")) return Response.json(overview(account));
    if (url.pathname.endsWith("/logs/groups"))
      return Response.json([
        {
          name: `AUDIT_LOG_${account}`,
          createdAt: date,
          eventCount: 0,
          streamCount: 0,
        },
      ]);
    if (url.pathname.endsWith("/environment"))
      return Response.json({
        name: "audit-shared-function",
        variables: { AUDIT_VALUE: `AUDIT_ENV_${account}` },
      });
    if (url.pathname.endsWith("/value"))
      return Response.json({
        name: "audit-shared-secret",
        value: `AUDIT_SECRET_${account}`,
        valueType: "string",
      });
    return Response.json({ events: [], total: 0 });
  };

  try {
    await showTab("logs", "Log groups");
    await selectAccount(accountA);
    // Entering from another tab isolates this check from whichever account was initially selected.
    await showTab("secrets", "Secrets");
    await showTab("logs", "Log groups");
    await waitFor(
      () => visibleText().includes(`AUDIT_LOG_${accountA}`),
      "Account A log groups missing",
    );
    await selectAccount(accountB);
    await waitFor(
      () => visibleText().includes(`AUDIT_LOG_${accountB}`),
      "Log groups stayed in account A after switching to B",
    );
    if (visibleText().includes(`AUDIT_LOG_${accountA}`))
      throw new Error("Old log groups remain visible");

    await showTab("functions", "Functions");
    await selectAccount(accountA);
    await reveal(`AUDIT_ENV_${accountA}`);
    await selectAccount(accountB);
    assertHidden(`AUDIT_ENV_${accountA}`);
    await reveal(`AUDIT_ENV_${accountB}`);
    const environmentRequests = requests.filter((r) => r.path.endsWith("/environment")).length;
    await refresh();
    await new Promise((resolve) => setTimeout(resolve, 20));
    if (
      !visibleText().includes(`AUDIT_ENV_${accountB}`) ||
      requests.filter((r) => r.path.endsWith("/environment")).length !== environmentRequests
    ) {
      throw new Error("An ordinary overview refresh reset the environment panel");
    }

    await showTab("secrets", "Secrets");
    await selectAccount(accountA);
    await reveal(`AUDIT_SECRET_${accountA}`);
    await selectAccount(accountB);
    assertHidden(`AUDIT_SECRET_${accountA}`);
    await reveal(`AUDIT_SECRET_${accountB}`);
    return {
      passed: [
        "logs reload on account switch",
        "environment values reset",
        "secret values reset",
        "ordinary polls preserve panel state",
      ],
      requests: requests.filter((r) => /\/logs\/groups$|\/environment$|\/value$/.test(r.path)),
    };
  } finally {
    await selectAccount(originalAccount);
    if (savedAccounts === null) localStorage.removeItem("tarn-accounts");
    else localStorage.setItem("tarn-accounts", savedAccounts);
    window.fetch = originalFetch;
    location.hash = originalHash;
  }
}
