<script lang="ts">
  import {
    SpinnerGapIcon,
    DownloadSimpleIcon,
    PlayIcon,
    StopIcon,
    CaretRightIcon,
    CaretDownIcon,
    CheckSquareIcon,
    SquareIcon,
    MagnifyingGlassIcon,
    CheckCircleIcon,
    WarningIcon,
  } from "phosphor-svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import SectionHeader from "./section-header.svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcButton from "$lib/components/rack/rc-button.svelte";
  import RcTonePill, { type Tone } from "$lib/components/rack/rc-tone-pill.svelte";
  import { formatJSONForViewer } from "$lib/json-format";
  import type {
    GatewaySummary,
    RouteDetail,
    ChaosRound,
    ScanSourceResponse,
    ScanMatch,
    ProbeBody,
  } from "$lib/types";
  import {
    normalizeInvokeUrl,
    buildPostmanCollection,
    downloadJSON,
    slugify,
  } from "$lib/postman";
  import {
    getUISettings,
    setSchemaSourceDir,
    sanitizeSchemaSourceDir,
  } from "$lib/state.svelte";

  let {
    gateways,
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    gateways: GatewaySummary[];
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();
  const uiSettings = getUISettings();

  // Only gateways with routeDetails can be chaos-probed
  const probeableGateways = $derived(
    gateways.filter((gw) => gw.routeDetails?.length),
  );

  // Composite key: `${gatewayId}::${routeKey}`
  function rid(gatewayId: string, routeKey: string) {
    return `${gatewayId}::${routeKey}`;
  }

  // Selection — start with all selectable routes ticked
  let selected = $state<Set<string>>(new Set<string>());
  $effect.pre(() => {
    if (selected.size === 0 && probeableGateways.length > 0) {
      selected = new Set(
        probeableGateways.flatMap((gw) =>
          (gw.routeDetails ?? []).map((d) => rid(gw.apiId, d.routeKey)),
        ),
      );
    }
  });

  let probing = $state(false);
  let currentKey = $state<string | null>(null);
  let results = $state<Map<string, ChaosRound>>(new Map());
  let expanded = $state<Set<string>>(new Set());
  let controller = $state<AbortController | null>(null);

  const totalSelected = $derived(selected.size);
  const totalRouteable = $derived(
    probeableGateways.reduce((n, gw) => n + (gw.routeDetails?.length ?? 0), 0),
  );

  function toggleRoute(id: string) {
    const s = new Set(selected);
    if (s.has(id)) s.delete(id);
    else s.add(id);
    selected = s;
  }

  function toggleGateway(gw: GatewaySummary) {
    const ids = (gw.routeDetails ?? []).map((d) => rid(gw.apiId, d.routeKey));
    const allOn = ids.every((id) => selected.has(id));
    const s = new Set(selected);
    ids.forEach((id) => (allOn ? s.delete(id) : s.add(id)));
    selected = s;
  }

  function setAll(on: boolean) {
    selected = on
      ? new Set(
          probeableGateways.flatMap((gw) =>
            (gw.routeDetails ?? []).map((d) => rid(gw.apiId, d.routeKey)),
          ),
        )
      : new Set();
  }

  function toggleExpand(id: string) {
    const e = new Set(expanded);
    if (e.has(id)) e.delete(id);
    else e.add(id);
    expanded = e;
  }

  function requiredHeaders(detail: RouteDetail): Record<string, string> {
    const out: Record<string, string> = {};
    if (detail.methodRequestParams) {
      for (const k of Object.keys(detail.methodRequestParams)) {
        const m = k.match(/^method\.request\.header\.(.+)$/i);
        if (m?.[1]) out[m[1]] = "";
      }
    }
    return out;
  }

  async function run() {
    if (!totalSelected || probing) return;
    probing = true;
    results = new Map();
    expanded = new Set();
    currentKey = null;
    controller = new AbortController();

    try {
      for (const gw of probeableGateways) {
        if (controller.signal.aborted) break;

        const gwRoutes = (gw.routeDetails ?? []).filter((d) =>
          selected.has(rid(gw.apiId, d.routeKey)),
        );
        if (!gwRoutes.length) continue;

        const invokeBase = normalizeInvokeUrl(gw.invokeUrl);
        const routes = gwRoutes.map((d) => {
          const pb = routeProbeBodies(d);
          return {
            routeKey: d.routeKey,
            method: d.method ?? "GET",
            path: d.path ?? "/",
            seedBody: d.bodyExample ?? undefined,
            requiredHeaders: requiredHeaders(d),
            ...(pb ? { probeBodies: pb } : {}),
          };
        });

        let resp: Response;
        try {
          resp = await fetch("/_tarn/admin/chaos", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ invokeBase, routes }),
            signal: controller.signal,
          });
        } catch {
          continue;
        }
        if (!resp.ok || !resp.body) continue;

        const reader = resp.body.getReader();
        const decoder = new TextDecoder();
        let buf = "";

        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          if (controller.signal.aborted) {
            reader.cancel();
            break;
          }
          buf += decoder.decode(value, { stream: true });
          const lines = buf.split("\n");
          buf = lines.pop() ?? "";
          for (const line of lines) {
            if (!line.trim()) continue;
            try {
              const round: ChaosRound = JSON.parse(line);
              const key = rid(gw.apiId, round.routeKey);
              results = new Map(results).set(key, round);
              currentKey = key;
              // Auto-expand routes with multiple rounds or when stuck
              if ((round.examples?.length ?? 0) > 1 || round.needsInput) {
                expanded = new Set(expanded).add(key);
              }
            } catch {
              /* skip malformed */
            }
          }
        }
      }
    } catch (e) {
      if (!(e instanceof Error && e.name === "AbortError")) throw e;
    } finally {
      probing = false;
      currentKey = null;
      controller = null;
    }
  }

  function stop() {
    controller?.abort();
  }

  function downloadGateway(gw: GatewaySummary) {
    const chaosMap = new Map<string, ChaosRound>();
    for (const [key, round] of results) {
      const sep = key.indexOf("::");
      if (sep !== -1 && key.slice(0, sep) === gw.apiId) {
        chaosMap.set(key.slice(sep + 2), round);
      }
    }
    downloadJSON(
      `${slugify(gw.name)}.postman_collection.json`,
      buildPostmanCollection(gw, chaosMap.size ? chaosMap : undefined),
    );
  }

  function downloadAll() {
    for (const gw of probeableGateways) {
      downloadGateway(gw);
    }
  }

  const successCount = $derived(
    [...results.values()].filter(
      (r) => (r.statusCode ?? 0) >= 200 && (r.statusCode ?? 0) < 300,
    ).length,
  );
  const totalExamples = $derived(
    [...results.values()].reduce((n, r) => n + (r.examples?.length ?? 1), 0),
  );

  function statusTone(code: number | undefined): "ok" | "warn" | "err" | "dim" {
    if (!code) return "dim";
    if (code < 300) return "ok";
    if (code < 500) return "warn";
    return "err";
  }

  function methodTone(m: string): Tone {
    const u = m.toUpperCase();
    if (u === "GET") return "green";
    if (u === "POST" || u === "PUT" || u === "PATCH") return "amber";
    if (u === "DELETE") return "red";
    return "neutral";
  }

  function roundLabel(i: number, total: number, label?: string) {
    if (label) return label;
    return total === 1 ? "probe" : `round ${i + 1}/${total}`;
  }

  function truncate(s: string, max = 120) {
    const oneline = s.replace(/\s+/g, " ").trim();
    return oneline.length > max ? oneline.slice(0, max) + "…" : oneline;
  }

  // Per-example view state
  let exShowReq = $state<Set<string>>(new Set());

  function exKey(routeId: string, idx: number) {
    return `${routeId}:${idx}`;
  }
  function toggleExReq(routeId: string, idx: number) {
    const k = exKey(routeId, idx);
    const s = new Set(exShowReq);
    if (s.has(k)) s.delete(k);
    else s.add(k);
    exShowReq = s;
  }
  function reqHeaderEntries(ex: { requestHeaders?: Record<string, string> }) {
    return Object.entries(ex.requestHeaders ?? {}).filter(([, v]) => v !== "");
  }

  // ── Source config (schema-driven probing) ────────────────────────────────
  let sourceDir = $state(uiSettings.schemaSourceDir);
  let scanning = $state(false);
  let scanResult = $state<ScanSourceResponse | null>(null);
  let scanError = $state("");

  $effect(() => {
    sourceDir = uiSettings.schemaSourceDir;
  });

  // Map: functionName → ScanMatch (from scan result)
  const matchMap = $derived(
    new Map<string, ScanMatch>(
      (scanResult?.matches ?? []).map((m) => [m.functionName, m]),
    ),
  );

  // Derive which function backs a given route by looking at integrationTarget.
  function routeFunctionName(detail: RouteDetail): string | null {
    if (!detail.integrationTarget) return null;
    // integrationTarget formats: "arn:...:function:FunctionName" or just "FunctionName"
    const parts = detail.integrationTarget.split(":");
    if (parts.length >= 7 && parts[5] === "function") return parts[6];
    return detail.integrationTarget;
  }

  function routeProbeBodies(detail: RouteDetail): ProbeBody[] | undefined {
    const fn = routeFunctionName(detail);
    if (!fn) return undefined;
    // Find the matching ScanMatch (exact, then fuzzy)
    let match: ScanMatch | undefined;
    if (matchMap.has(fn)) {
      match = matchMap.get(fn);
    } else {
      for (const [k, v] of matchMap) {
        if (fn.includes(k) || k.includes(fn)) {
          match = v;
          break;
        }
      }
    }
    if (!match) return undefined;
    // Prefer method-specific probes to avoid sending POST bodies to PATCH routes etc.
    const method = (detail.method ?? "GET").toUpperCase();
    if (match.probesByMethod?.[method]?.length) {
      return match.probesByMethod[method];
    }
    return match.probeBodies;
  }

  async function scanSource() {
    const normalizedSourceDir = sanitizeSchemaSourceDir(sourceDir);
    if (!normalizedSourceDir || scanning) return;

    sourceDir = normalizedSourceDir;
    setSchemaSourceDir(normalizedSourceDir);
    scanning = true;
    scanError = "";
    scanResult = null;

    const functionNames = probeableGateways.flatMap((gw) =>
      (gw.routeDetails ?? [])
        .map((d) => routeFunctionName(d))
        .filter((n): n is string => n !== null),
    );
    const unique = [...new Set(functionNames)];

    try {
      const resp = await fetch("/_tarn/admin/chaos/source", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          baseDir: normalizedSourceDir,
          functionNames: unique,
        }),
      });
      if (!resp.ok) {
        const err = await resp.json().catch(() => ({ error: resp.statusText }));
        scanError = err.error ?? "Scan failed";
        return;
      }
      scanResult = (await resp.json()) as ScanSourceResponse;
    } catch (e) {
      scanError = e instanceof Error ? e.message : "Scan failed";
    } finally {
      scanning = false;
    }
  }

  // Override inputs state — routeId → { fieldName: value }
  let overrideInputs = $state<Map<string, Record<string, string>>>(new Map());
  let reProbing = $state<Set<string>>(new Set());

  function getOverride(id: string, field: string) {
    return overrideInputs.get(id)?.[field] ?? "";
  }
  function setOverride(id: string, field: string, value: string) {
    const m = new Map(overrideInputs);
    m.set(id, { ...(m.get(id) ?? {}), [field]: value });
    overrideInputs = m;
  }

  async function reprobeRoute(
    gw: GatewaySummary,
    detail: RouteDetail,
    id: string,
  ) {
    const overrides = overrideInputs.get(id) ?? {};
    if (!Object.keys(overrides).some((k) => overrides[k].trim())) return;

    // Carry the last known body state so re-probe doesn't re-discover already-filled fields.
    const lastExample = results.get(id)?.examples?.at(-1);
    let baseBody: Record<string, unknown> = {};
    if (lastExample?.requestBody) {
      try {
        baseBody = JSON.parse(lastExample.requestBody);
      } catch {
        /* ignore */
      }
    }
    // Merge: base (previously discovered) + user overrides
    const fieldOverrides = { ...baseBody, ...overrides };

    const rp = new Set(reProbing);
    rp.add(id);
    reProbing = rp;

    try {
      const invokeBase = normalizeInvokeUrl(gw.invokeUrl);
      let resp: Response;
      try {
        resp = await fetch("/_tarn/admin/chaos", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            invokeBase,
            routes: [
              {
                routeKey: detail.routeKey,
                method: detail.method ?? "GET",
                path: detail.path ?? "/",
                seedBody: detail.bodyExample ?? undefined,
                requiredHeaders: requiredHeaders(detail),
                fieldOverrides,
              },
            ],
          }),
        });
      } catch {
        return;
      }
      if (!resp.ok || !resp.body) return;

      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      let buf = "";
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        const lines = buf.split("\n");
        buf = lines.pop() ?? "";
        for (const line of lines) {
          if (!line.trim()) continue;
          try {
            const round: ChaosRound = JSON.parse(line);
            const newResults = new Map(results);
            newResults.set(id, round);
            results = newResults;
            if ((round.examples?.length ?? 0) > 0) {
              expanded = new Set(expanded).add(id);
            }
          } catch {
            /* skip */
          }
        }
      }
    } finally {
      const rp = new Set(reProbing);
      rp.delete(id);
      reProbing = rp;
    }
  }

  function updateSourceDir(value: string) {
    const normalized = sanitizeSchemaSourceDir(value);
    sourceDir = normalized;
    setSchemaSourceDir(normalized);
    if (scanError) {
      scanError = "";
    }
  }
</script>

<div class="chaos">
  <!-- Header -->
  <SectionHeader
    title="Chaos probe"
    description="Exhaust validation layers per route and capture the full example set."
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet stats()}
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{totalSelected}</span>
        <span class="text-muted-foreground/70">selected</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{totalRouteable}</span>
        <span class="text-muted-foreground/70">routeable</span>
      </span>
    {/snippet}

    {#snippet actions()}
      {#if probing}
        <RcButton variant="danger" small onclick={stop}>
          <StopIcon size={12} />
          Stop
        </RcButton>
      {:else}
        <RcButton variant="ghost" small onclick={() => setAll(true)}>
          Select all
        </RcButton>
        <RcButton variant="ghost" small onclick={() => setAll(false)}>
          Clear
        </RcButton>
        <RcButton variant="primary" small onclick={run} disabled={!totalSelected}>
          <PlayIcon size={12} />
          Probe {totalSelected > 0 ? `(${totalSelected})` : ""}
        </RcButton>
      {/if}
    {/snippet}
  </SectionHeader>

  <!-- Source config panel -->
  <RcPanel
    title="Schema source"
    description="Provide a local Lambda repo directory to generate schema-driven probes."
    index={0}
  >
    <div class="source-row">
      <input
        id="chaos-schema-source"
        type="text"
        placeholder="/path/to/lambda-repos"
        value={sourceDir}
        oninput={(e) =>
          updateSourceDir((e.currentTarget as HTMLInputElement).value)}
        onkeydown={(e) => e.key === "Enter" && scanSource()}
        class="field"
        aria-label="Schema source directory"
      />
      <RcButton small onclick={scanSource} disabled={!sourceDir.trim() || scanning}>
        {#if scanning}
          <SpinnerGapIcon size={11} class="animate-spin" />
          Scanning…
        {:else}
          <MagnifyingGlassIcon size={11} />
          Scan
        {/if}
      </RcButton>
    </div>

    {#if scanError}
      <p class="error"><WarningIcon size={11} class="shrink-0" />{scanError}</p>
    {/if}

    {#if scanResult}
      {@const matchCount = scanResult.matches.length}
      {@const withSchema = scanResult.matches.filter(
        (m) => m.schemasTs,
      ).length}
      {@const unmatched = scanResult.unmatched.length}
      <div class="scan-stats">
        <span class="scan-ok">
          <CheckCircleIcon size={11} />
          {matchCount} matched
        </span>
        {#if withSchema > 0}
          <span class="scan-sub">{withSchema} with schema</span>
        {/if}
        {#if unmatched > 0}
          <span class="scan-sub">{unmatched} unmatched</span>
        {/if}
      </div>

      {#if scanResult.matches.length > 0}
        <div class="match-list">
          {#each scanResult.matches as m (m.functionName)}
            <div class="match">
              <div class="match-main">
                <div class="match-title">
                  <span class="match-name">{m.functionName}</span>
                  <span class="match-score">{Math.round(m.score * 100)}% match</span>
                </div>
                <span class="match-dir">{m.dir}</span>
              </div>
              <div class="match-tags">
                {#if m.schemasTs}
                  <RcTonePill tone="green">schema</RcTonePill>
                {/if}
                {#if m.eventFiles?.length}
                  <span class="mini-tag">
                    {m.eventFiles.length} event{m.eventFiles.length !== 1 ? "s" : ""}
                  </span>
                {/if}
                {#if m.probesByMethod}
                  {#each Object.entries(m.probesByMethod) as [meth, bodies] (meth)}
                    <span class="mini-tag">{meth} {bodies.length}p</span>
                  {/each}
                {:else if m.probeBodies?.length}
                  <span class="mini-tag">
                    {m.probeBodies.length} probe{m.probeBodies.length !== 1 ? "s" : ""}
                  </span>
                {/if}
              </div>
            </div>
          {/each}
        </div>
      {/if}

      {#if scanResult.unmatched.length > 0}
        <p class="scan-unmatched">Unmatched: {scanResult.unmatched.join(", ")}</p>
      {/if}
    {/if}
  </RcPanel>

  {#if probeableGateways.length === 0}
    <div class="blank">
      <h2>No probeable gateways</h2>
      <p>Refresh the dashboard after deploying your API Gateway.</p>
    </div>
  {:else}
    <div class="gateway-list">
      {#each probeableGateways as gw, gi (gw.apiId)}
        {@const gwRoutes = gw.routeDetails ?? []}
        {@const gwIds = gwRoutes.map((d) => rid(gw.apiId, d.routeKey))}
        {@const allChecked = gwIds.every((id) => selected.has(id))}
        {@const gwDone =
          results.size > 0 && gwIds.every((id) => results.has(id))}

        <RcPanel
          title={gw.name}
          description="{gw.protocolType} {gw.version} · {normalizeInvokeUrl(gw.invokeUrl)}"
          index={gi + 1}
        >
          {#snippet actions()}
            <button
              type="button"
              onclick={() => toggleGateway(gw)}
              class="check-btn"
              aria-label={allChecked ? "Deselect all routes" : "Select all routes"}
            >
              {#if allChecked}
                <CheckSquareIcon size={14} class="on" />
              {:else}
                <SquareIcon size={14} />
              {/if}
            </button>
            {#if gwDone}
              <RcButton small onclick={() => downloadGateway(gw)}>
                <DownloadSimpleIcon size={11} />
                Collection
              </RcButton>
            {/if}
          {/snippet}

          <!-- Route rows -->
          <div class="route-rows">
            {#each gwRoutes as detail (detail.routeKey)}
              {@const id = rid(gw.apiId, detail.routeKey)}
              {@const round = results.get(id)}
              {@const isProbing = probing && currentKey === id}
              {@const isExpanded = expanded.has(id)}

              <div>
                <!-- Route row -->
                <div class="route-row">
                  <!-- Checkbox -->
                  <button
                    type="button"
                    onclick={() => toggleRoute(id)}
                    disabled={probing}
                    class="check-btn"
                    aria-label={selected.has(id) ? "Deselect" : "Select"}
                  >
                    {#if selected.has(id)}
                      <CheckSquareIcon size={13} class="on" />
                    {:else}
                      <SquareIcon size={13} />
                    {/if}
                  </button>

                  <!-- Method -->
                  <RcTonePill tone={methodTone(detail.method ?? "GET")}>
                    {detail.method ?? "GET"}
                  </RcTonePill>

                  <!-- Path -->
                  <span class="route-path">
                    {detail.path ?? detail.routeKey}
                  </span>

                  <!-- Status -->
                  {#if isProbing}
                    <span class="probing">
                      <SpinnerGapIcon size={10} class="animate-spin" />
                      probing
                    </span>
                  {:else if round}
                    {@const codes = round.examples?.map(
                      (e) => e.statusCode,
                    ) ?? [round.statusCode]}
                    <span class="route-code" data-tone={statusTone(round.statusCode)}>
                      {round.statusCode}
                    </span>
                    {#if codes.length > 1}
                      <span class="route-codes">
                        {codes.join("→")}
                      </span>
                    {/if}
                    <span class="route-duration">{round.durationMs}ms</span>
                    {#if round.examples?.length}
                      <button
                        type="button"
                        onclick={() => toggleExpand(id)}
                        class="examples-btn"
                        aria-label={isExpanded
                          ? "Collapse examples"
                          : "Expand examples"}
                      >
                        {#if isExpanded}
                          <CaretDownIcon size={11} />
                        {:else}
                          <CaretRightIcon size={11} />
                        {/if}
                        <span
                          >{round.examples.length} example{round.examples
                            .length !== 1
                            ? "s"
                            : ""}</span
                        >
                      </button>
                    {/if}
                  {:else if probing && selected.has(id)}
                    <span class="route-queued">queued</span>
                  {:else if selected.has(id)}
                    <span class="route-pending">pending</span>
                  {/if}
                </div>

                <!-- Examples expansion -->
                {#if isExpanded && round?.examples?.length}
                  {@const total = round.examples.length}
                  <div class="examples">
                    {#each round.examples as ex, i (i)}
                      {@const ek = exKey(id, i)}
                      {@const showReq = exShowReq.has(ek)}
                      {@const reqHeaders = reqHeaderEntries(ex)}
                      {@const hasReqBody =
                        ex.requestBody !== undefined && ex.requestBody !== null}
                      {@const hasResBody = !!ex.body}
                      {@const formattedReqBody = hasReqBody
                        ? formatJSONForViewer(ex.requestBody ?? "")
                        : null}
                      {@const formattedResBody = hasResBody
                        ? formatJSONForViewer(ex.body!)
                        : null}

                      <div>
                        <!-- Example header row -->
                        <div class="example-head">
                          <span
                            class="example-label"
                            title={roundLabel(i, total, ex.label)}
                            >{roundLabel(i, total, ex.label)}</span
                          >
                          <span class="ex-code" data-tone={statusTone(ex.statusCode)}>{ex.statusCode}</span>
                          <span class="example-duration">{ex.durationMs}ms</span>
                          <span class="flex-1"></span>
                          <button
                            type="button"
                            onclick={() => toggleExReq(id, i)}
                            class="req-btn"
                            class:on={showReq}
                            title="Show request inputs">req</button
                          >
                        </div>

                        <!-- Request detail panel -->
                        {#if showReq}
                          <div
                            class="mx-3 mb-2 rounded-lg border border-border/50 bg-popover/40 overflow-hidden"
                          >
                            <div class="px-2.5 py-1.5 space-y-2">
                              <div>
                                <p
                                  class="mb-1 font-mono text-[9px] uppercase tracking-widest text-muted-foreground/70/50"
                                >
                                  headers sent
                                </p>
                                {#if reqHeaders.length}
                                  <div class="space-y-0.5">
                                    {#each reqHeaders as [k, v] (k)}
                                      <div
                                        class="flex gap-2 font-mono text-[10px]"
                                      >
                                        <span
                                          class="text-muted-foreground/70 shrink-0"
                                          >{k}</span
                                        >
                                        <span class="text-primary/80 break-all"
                                          >{v}</span
                                        >
                                      </div>
                                    {/each}
                                  </div>
                                {:else}
                                  <p
                                    class="font-mono text-[10px] text-muted-foreground/70/40 italic"
                                  >
                                    no custom headers
                                  </p>
                                {/if}
                              </div>
                              {#if hasReqBody}
                                <div>
                                  <p
                                    class="mb-1 font-mono text-[9px] uppercase tracking-widest text-muted-foreground/70/50"
                                  >
                                    body sent
                                  </p>
                                  {#if formattedReqBody}
                                    <FormattedMessageViewer
                                      raw={ex.requestBody ?? ""}
                                      formatted={formattedReqBody.formatted}
                                      formattedHtml={formattedReqBody.formattedHtml}
                                      formattedLabel="JSON"
                                      rawLabel="Raw Body"
                                      formattedOpenByDefault={true}
                                      rawOpenByDefault={false}
                                      formattedContentClass="text-[10px] text-muted-foreground"
                                      rawContentClass="text-[10px] text-muted-foreground"
                                      formattedMaxHeightClass="max-h-52"
                                      rawMaxHeightClass="max-h-40"
                                    />
                                  {:else}
                                    <pre
                                      class="font-mono text-[10px] text-muted-foreground whitespace-pre-wrap break-all leading-relaxed">{ex
                                        .requestBody}</pre>
                                  {/if}
                                </div>
                              {/if}
                            </div>
                          </div>
                        {/if}

                        <!-- Response body -->
                        {#if hasResBody}
                          <div class="px-3 pb-2.5">
                            <p
                              class="mb-1 font-mono text-[9px] uppercase tracking-widest text-muted-foreground/70/50"
                            >
                              response
                            </p>
                            {#if formattedResBody}
                              <FormattedMessageViewer
                                raw={ex.body!}
                                formatted={formattedResBody.formatted}
                                formattedHtml={formattedResBody.formattedHtml}
                                formattedLabel="JSON"
                                rawLabel="Raw Response"
                                formattedOpenByDefault={true}
                                rawOpenByDefault={false}
                                formattedContentClass="text-[10px] text-muted-foreground"
                                rawContentClass="text-[10px] text-muted-foreground"
                                formattedMaxHeightClass="max-h-56"
                                rawMaxHeightClass="max-h-40"
                              />
                            {:else}
                              <pre
                                class="font-mono text-[10px] text-muted-foreground whitespace-pre-wrap break-all leading-relaxed max-h-56 overflow-y-auto rounded">{truncate(
                                  ex.body!,
                                  800,
                                )}</pre>
                            {/if}
                          </div>
                        {/if}
                      </div>
                    {/each}
                  </div>
                {/if}

                <!-- Stuck / needs-input panel -->
                {#if round?.needsInput && !reProbing.has(id)}
                  <div class="stuck">
                    <p class="stuck-title">
                      Enum field{round.stuckFields &&
                      round.stuckFields.length !== 1
                        ? "s"
                        : ""} need a valid value to continue.
                    </p>
                    <div class="space-y-1.5 mb-2.5">
                      {#each round.stuckFields ?? [] as field (field)}
                        <div class="flex items-center gap-2">
                          <span
                            class="font-mono text-[10px] text-muted-foreground/70 w-28 shrink-0 truncate"
                            >{field}</span
                          >
                          <div class="flex-1 min-w-0 space-y-0.5">
                            <input
                              type="text"
                              placeholder="value"
                              value={getOverride(id, field)}
                              oninput={(e) =>
                                setOverride(
                                  id,
                                  field,
                                  (e.target as HTMLInputElement).value,
                                )}
                              class="field small"
                            />
                            {#if round.stuckOptions?.[field]?.length}
                              <p
                                class="font-mono text-[9px] text-muted-foreground/70/50"
                              >
                                options: {round.stuckOptions[field].join(", ")}
                              </p>
                            {/if}
                          </div>
                        </div>
                      {/each}
                    </div>
                    <RcButton small onclick={() => reprobeRoute(gw, detail, id)}>
                      <PlayIcon size={10} />
                      Re-probe with overrides
                    </RcButton>
                  </div>
                {:else if reProbing.has(id)}
                  <div
                    class="border-t border-border/30 bg-background px-3 py-2 flex items-center gap-1.5 font-mono text-[10px] text-primary"
                  >
                    <SpinnerGapIcon size={10} class="animate-spin" />
                    re-probing…
                  </div>
                {/if}
              </div>
            {/each}
          </div>
        </RcPanel>
      {/each}
    </div>

    <!-- Progress bar while probing -->
    {#if probing}
      <div class="progress">
        <div
          class="progress-fill"
          style="width: {totalSelected > 0
            ? Math.round((results.size / totalSelected) * 100)
            : 0}%"
        ></div>
        <div class="progress-labels">
          <span class="progress-done">
            {results.size} / {totalSelected} probed
          </span>
          <span class="progress-sub">
            {totalExamples} example{totalExamples !== 1 ? "s" : ""} captured
          </span>
        </div>
      </div>
    {/if}

    <!-- Done: full-width download bar  |  [=== stats ===| ↓ Download All ] -->
    {#if !probing && results.size > 0}
      <div class="donebar">
        <!-- left: stats fill -->
        <div class="donebar-stats">
          <span class="donebar-stat">
            {results.size} route{results.size !== 1 ? "s" : ""}
          </span>
          <span class="donebar-sep">|</span>
          <span class="donebar-stat {successCount > 0 ? 'ok' : 'dim'}">
            {successCount} success
          </span>
          <span class="donebar-sep">|</span>
          <span class="donebar-stat">
            {totalExamples} example{totalExamples !== 1 ? "s" : ""}
          </span>
          <span class="donebar-sep hidden sm:inline">|</span>
          <span class="donebar-names">
            {probeableGateways.map((g) => g.name).join(" · ")}
          </span>
        </div>
        <!-- right: action buttons -->
        <div class="donebar-actions">
          <button
            type="button"
            onclick={run}
            class="donebar-btn"
            title="Re-run probes"
          >
            <PlayIcon size={11} />
            <span class="hidden sm:inline">Re-run</span>
          </button>
          <RcButton small onclick={downloadAll}>
            <DownloadSimpleIcon size={12} />
            Download All
          </RcButton>
        </div>
      </div>
    {/if}
  {/if}
</div>

<style>
  .chaos { display: flex; flex-direction: column; gap: 14px; min-height: 100%; }
  .gateway-list { display: flex; flex-direction: column; gap: 14px; }

  .field {
    height: 30px; width: 100%; min-width: 0; padding: 0 10px; border-radius: 8px;
    border: 1px solid var(--border-subtle); background: var(--bg-app); color: var(--text-primary);
    font: 11.5px var(--font-mono, ui-monospace, monospace); outline: none;
    transition: border-color 120ms ease;
  }
  .field::placeholder { color: var(--text-tertiary); }
  .field:hover { border-color: var(--border-default); }
  .field:focus { border-color: var(--border-focus); }
  .field.small { height: 26px; font-size: 10px; }

  .source-row { display: flex; align-items: center; gap: 8px; }
  .error {
    display: flex; align-items: center; gap: 6px; margin-top: 8px;
    font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--accent-red);
  }
  .scan-stats { display: flex; align-items: center; gap: 12px; margin-top: 10px; font: 10.5px var(--font-mono, ui-monospace, monospace); }
  .scan-ok { display: inline-flex; align-items: center; gap: 4px; color: var(--accent-green); }
  .scan-sub { color: var(--text-tertiary); }
  .match-list { display: flex; flex-direction: column; gap: 4px; margin-top: 8px; max-height: 10rem; overflow-y: auto; }
  .match {
    display: flex; align-items: flex-start; gap: 8px; border: 1px solid var(--border-subtle);
    border-radius: 8px; background: var(--bg-app); padding: 6px 10px;
  }
  .match-main { flex: 1; min-width: 0; }
  .match-title { display: flex; align-items: center; gap: 8px; }
  .match-name { font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .match-score { font: 9px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .match-dir { font: 9px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .match-tags { flex-shrink: 0; display: flex; gap: 6px; }
  .mini-tag {
    border: 1px solid var(--border-subtle); border-radius: 6px; padding: 1px 5px;
    font: 9px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); white-space: nowrap;
  }
  .scan-unmatched { margin-top: 6px; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 4px; font-size: 12px; color: var(--text-secondary); }
  @keyframes fadeUp { from { opacity: 0; transform: translateY(6px); } }

  .check-btn {
    display: inline-flex; align-items: center; justify-content: center; flex-shrink: 0;
    color: var(--text-tertiary); transition: color 120ms ease;
  }
  .check-btn:hover:not(:disabled) { color: var(--text-primary); }
  .check-btn:disabled { opacity: 0.4; }
  .check-btn :global(.on) { color: var(--accent-green); }

  .route-rows { display: flex; flex-direction: column; }
  .route-row { display: flex; align-items: center; gap: 8px; padding: 7px 2px; font-size: 11px; border-top: 1px solid var(--border-subtle); }
  .route-row:first-child { border-top: 0; }
  .route-path { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); }
  .probing { display: inline-flex; align-items: center; gap: 4px; flex-shrink: 0; font-size: 10.5px; color: var(--accent-green); }
  .route-code { flex-shrink: 0; font-family: var(--font-mono, ui-monospace, monospace); font-weight: 600; font-variant-numeric: tabular-nums; color: var(--text-tertiary); }
  .route-code[data-tone="ok"] { color: var(--accent-green); }
  .route-code[data-tone="warn"] { color: var(--accent-amber); }
  .route-code[data-tone="err"] { color: var(--accent-red); }
  .route-codes { flex-shrink: 0; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .route-duration { flex-shrink: 0; font-size: 11px; color: var(--text-tertiary); }
  .examples-btn {
    display: inline-flex; align-items: center; gap: 2px; flex-shrink: 0;
    font-size: 10px; color: var(--text-tertiary); transition: color 120ms ease;
  }
  .examples-btn:hover { color: var(--text-primary); }
  .route-queued, .route-pending { flex-shrink: 0; font-size: 10.5px; color: var(--text-tertiary); opacity: 0.6; }

  .examples { border-top: 1px solid var(--border-subtle); background: var(--bg-app); border-radius: 0 0 8px 8px; }
  .example-head { display: flex; align-items: center; gap: 8px; padding: 6px 10px; font: 10px var(--font-mono, ui-monospace, monospace); }
  .example-label { flex-shrink: 0; max-width: 7rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-tertiary); }
  .ex-code { flex-shrink: 0; font-weight: 600; font-variant-numeric: tabular-nums; color: var(--text-tertiary); }
  .ex-code[data-tone="ok"] { color: var(--accent-green); }
  .ex-code[data-tone="warn"] { color: var(--accent-amber); }
  .ex-code[data-tone="err"] { color: var(--accent-red); }
  .example-duration { flex-shrink: 0; color: var(--text-tertiary); }
  .req-btn {
    flex-shrink: 0; border-radius: 6px; padding: 2px 6px; font-size: 9px; color: var(--text-tertiary);
    transition: color 120ms ease, background 120ms ease;
  }
  .req-btn:hover { color: var(--text-primary); }
  .req-btn.on { background: var(--bg-element); color: var(--text-primary); border: 1px solid var(--border-subtle); }

  .stuck {
    border-top: 1px solid color-mix(in srgb, var(--accent-amber) 25%, transparent);
    background: color-mix(in srgb, var(--accent-amber) 6%, transparent);
    padding: 10px 12px;
  }
  .stuck-title { margin-bottom: 8px; font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--accent-amber); }

  .progress {
    position: relative; height: 28px; border-radius: 8px; border: 1px solid var(--border-subtle);
    overflow: hidden; background: var(--bg-app);
  }
  .progress-fill {
    position: absolute; inset: 0 auto 0 0;
    background: color-mix(in srgb, var(--accent-green) 12%, transparent);
    transition: width 300ms ease;
  }
  .progress-labels { position: relative; display: flex; height: 100%; align-items: center; justify-content: space-between; padding: 0 12px; }
  .progress-done { font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--accent-green); }
  .progress-sub { font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }

  .donebar {
    display: flex; border-radius: 8px; border: 1px solid var(--border-subtle);
    overflow: hidden; font-size: 12px; background: var(--bg-stage);
  }
  .donebar-stats { display: flex; flex: 1; align-items: center; gap: 12px; padding: 8px 14px; min-width: 0; }
  .donebar-stat { font-family: var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); flex-shrink: 0; font-variant-numeric: tabular-nums; }
  .donebar-stat.ok { color: var(--accent-green); }
  .donebar-stat.dim { color: var(--text-tertiary); }
  .donebar-sep { color: var(--border-default); user-select: none; }
  .donebar-names { display: none; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  @media (min-width: 640px) { .donebar-names { display: block; } }
  .donebar-actions { display: flex; align-items: center; gap: 8px; flex-shrink: 0; padding: 6px 10px; border-left: 1px solid var(--border-subtle); }
  .donebar-btn {
    display: inline-flex; align-items: center; gap: 6px; font-size: 11.5px; color: var(--text-tertiary);
    transition: color 120ms ease;
  }
  .donebar-btn:hover { color: var(--text-primary); }

  @media (prefers-reduced-motion: reduce) {
    .blank { animation: none; }
    .progress-fill { transition: none; }
  }
</style>
