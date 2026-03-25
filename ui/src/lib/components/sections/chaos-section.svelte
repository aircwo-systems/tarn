<script lang="ts">
  import {
    LightningIcon,
    SpinnerGapIcon,
    DownloadSimpleIcon,
    PlayIcon,
    StopIcon,
    CaretRightIcon,
    CaretDownIcon,
    CheckSquareIcon,
    SquareIcon,
    FolderOpenIcon,
    MagnifyingGlassIcon,
    CheckCircleIcon,
    WarningIcon,
  } from "phosphor-svelte";
  import Badge from "$lib/components/ui/badge/badge.svelte";
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

  let { gateways }: { gateways: GatewaySummary[] } = $props();
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

  function statusColor(code: number | undefined) {
    if (!code) return "text-muted-foreground/70";
    if (code < 300) return "text-green-500";
    if (code < 400) return "text-yellow-500";
    if (code < 500) return "text-orange-500";
    return "text-destructive-500";
  }

  function methodVariant(
    m: string,
  ): "default" | "secondary" | "amber" | "outline" | "destructive" {
    const u = m.toUpperCase();
    if (u === "GET") return "default";
    if (u === "POST" || u === "PUT" || u === "PATCH") return "amber";
    if (u === "DELETE") return "destructive";
    if (u === "$DEFAULT") return "secondary";
    return "outline";
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
  let exFormatted = $state<Set<string>>(new Set());

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
  function toggleExFormat(routeId: string, idx: number) {
    const k = exKey(routeId, idx);
    const s = new Set(exFormatted);
    if (s.has(k)) s.delete(k);
    else s.add(k);
    exFormatted = s;
  }
  function tryFormat(s: string): string {
    try {
      return JSON.stringify(JSON.parse(s), null, 2);
    } catch {
      return s;
    }
  }
  function isJson(s: string): boolean {
    try {
      JSON.parse(s);
      return true;
    } catch {
      return false;
    }
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

<div class="space-y-4">
  <!-- Header -->
  <div class="flex items-center justify-between gap-4">
    <div>
      <h2 class="text-sm font-semibold text-foreground flex items-center gap-2">
        <LightningIcon size={14} class="text-primary" />
        Chaos Probe
      </h2>
      <p class="mt-0.5 text-[11px] text-muted-foreground/70">
        Exhaust all validation layers per route — captures every 400 and the
        final 200 as named Postman examples.
      </p>
    </div>

    <div class="flex items-center gap-2 shrink-0">
      {#if probing}
        <button
          type="button"
          onclick={stop}
          class="inline-flex items-center gap-1.5 rounded-md border border-red-500/40 bg-red-500/10 px-3 py-1.5 text-xs text-destructive-400 hover:bg-red-500/20 transition-colors"
        >
          <StopIcon size={12} />
          Stop
        </button>
      {:else}
        <button
          type="button"
          onclick={() => setAll(true)}
          class="text-[11px] text-muted-foreground/70 hover:text-foreground transition-colors"
        >
          Select all
        </button>
        <button
          type="button"
          onclick={() => setAll(false)}
          class="text-[11px] text-muted-foreground/70 hover:text-foreground transition-colors"
        >
          Clear
        </button>
        <button
          type="button"
          onclick={run}
          disabled={!totalSelected}
          class="inline-flex items-center gap-1.5 rounded-md border border-primary/50 bg-primary/10 px-3 py-1.5 text-xs text-primary hover:bg-primary/20 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
        >
          <PlayIcon size={12} />
          Probe {totalSelected > 0 ? `(${totalSelected})` : ""}
        </button>
      {/if}
    </div>
  </div>

  <!-- Source config panel -->
  <div class="rounded-lg border border-border bg-card overflow-hidden">
    <div
      class="flex items-center gap-2 border-b border-border px-3 py-2 bg-popover/40"
    >
      <FolderOpenIcon size={13} class="text-muted-foreground/70 shrink-0" />
      <span class="text-[11px] font-semibold text-foreground"
        >Schema Source</span
      >
      <span class="ml-1 text-[10px] text-muted-foreground/70"
        >— provide a local Lambda repo directory to generate schema-driven
        probes</span
      >
    </div>
    <div class="px-3 py-2.5 space-y-2">
      <div class="flex items-center gap-2">
        <input
          id="chaos-schema-source"
          type="text"
          placeholder="/path/to/lambda-repos"
          value={sourceDir}
          oninput={(e) =>
            updateSourceDir((e.currentTarget as HTMLInputElement).value)}
          onkeydown={(e) => e.key === "Enter" && scanSource()}
          class="flex-1 min-w-0 rounded-lg border border-border bg-background px-2.5 py-1.5 font-mono text-[11px] text-foreground placeholder:text-muted-foreground/70/40 focus:border-primary/60 focus:outline-none"
        />
        <button
          type="button"
          onclick={scanSource}
          disabled={!sourceDir.trim() || scanning}
          class="inline-flex items-center gap-1.5 rounded-lg border border-border bg-card px-3 py-1.5 text-[11px] text-foreground hover:bg-popover transition-colors disabled:opacity-40 disabled:cursor-not-allowed shrink-0"
        >
          {#if scanning}
            <SpinnerGapIcon size={11} class="animate-spin" />
            Scanning…
          {:else}
            <MagnifyingGlassIcon size={11} />
            Scan
          {/if}
        </button>
      </div>

      {#if scanError}
        <div
          class="flex items-center gap-1.5 font-mono text-[10px] text-destructive-400"
        >
          <WarningIcon size={11} class="shrink-0" />
          {scanError}
        </div>
      {/if}

      {#if scanResult}
        {@const matchCount = scanResult.matches.length}
        {@const withSchema = scanResult.matches.filter(
          (m) => m.schemasTs,
        ).length}
        {@const unmatched = scanResult.unmatched.length}
        <div class="flex items-center gap-3 font-mono text-[10px]">
          <span class="flex items-center gap-1 text-primary">
            <CheckCircleIcon size={11} />
            {matchCount} matched
          </span>
          {#if withSchema > 0}
            <span class="text-primary/80">{withSchema} with schema</span>
          {/if}
          {#if unmatched > 0}
            <span class="text-muted-foreground/70">{unmatched} unmatched</span>
          {/if}
        </div>

        {#if scanResult.matches.length > 0}
          <div class="space-y-1 max-h-40 overflow-y-auto">
            {#each scanResult.matches as m (m.functionName)}
              <div
                class="flex items-start gap-2 rounded-lg border border-border/50 bg-background px-2.5 py-1.5 text-[10px]"
              >
                <div class="flex-1 min-w-0">
                  <div class="flex items-center gap-2">
                    <span class="font-mono text-foreground truncate"
                      >{m.functionName}</span
                    >
                    <span
                      class="font-mono text-[9px] text-muted-foreground/70/60"
                    >
                      {Math.round(m.score * 100)}% match
                    </span>
                  </div>
                  <div class="flex items-center gap-2 mt-0.5">
                    <span
                      class="font-mono text-[9px] text-muted-foreground/70 truncate"
                      >{m.dir}</span
                    >
                  </div>
                </div>
                <div class="shrink-0 flex gap-1.5">
                  {#if m.schemasTs}
                    <span
                      class="rounded-lg border border-primary/50 bg-primary/10 px-1 py-0.5 font-mono text-[9px] text-primary"
                    >
                      schema
                    </span>
                  {/if}
                  {#if m.eventFiles?.length}
                    <span
                      class="rounded-lg border border-border bg-card px-1 py-0.5 font-mono text-[9px] text-muted-foreground/70"
                    >
                      {m.eventFiles.length} event{m.eventFiles.length !== 1
                        ? "s"
                        : ""}
                    </span>
                  {/if}
                  {#if m.probesByMethod}
                    {#each Object.entries(m.probesByMethod) as [meth, bodies] (meth)}
                      <span
                        class="rounded-lg border border-border bg-card px-1 py-0.5 font-mono text-[9px] text-muted-foreground/70"
                      >
                        {meth}
                        {bodies.length}p
                      </span>
                    {/each}
                  {:else if m.probeBodies?.length}
                    <span
                      class="rounded-lg border border-border bg-card px-1 py-0.5 font-mono text-[9px] text-muted-foreground/70"
                    >
                      {m.probeBodies.length} probe{m.probeBodies.length !== 1
                        ? "s"
                        : ""}
                    </span>
                  {/if}
                </div>
              </div>
            {/each}
          </div>
        {/if}

        {#if scanResult.unmatched.length > 0}
          <div class="font-mono text-[10px] text-muted-foreground/70/60">
            Unmatched: {scanResult.unmatched.join(", ")}
          </div>
        {/if}
      {/if}
    </div>
  </div>

  {#if probeableGateways.length === 0}
    <div class="rounded-lg border border-border bg-card px-4 py-8 text-center">
      <p class="text-sm text-muted-foreground/70">
        No gateways with route details available.
      </p>
      <p class="mt-1 text-[11px] text-muted-foreground/70">
        Refresh the dashboard after deploying your API Gateway.
      </p>
    </div>
  {:else}
    <div class="space-y-3">
      {#each probeableGateways as gw (gw.apiId)}
        {@const gwRoutes = gw.routeDetails ?? []}
        {@const gwIds = gwRoutes.map((d) => rid(gw.apiId, d.routeKey))}
        {@const allChecked = gwIds.every((id) => selected.has(id))}
        {@const gwDone =
          results.size > 0 && gwIds.every((id) => results.has(id))}

        <div class="rounded-lg border border-border bg-card overflow-hidden">
          <!-- Gateway header -->
          <div
            class="flex items-center justify-between gap-3 border-b border-border px-3 py-2 bg-popover/40"
          >
            <div class="flex items-center gap-2 min-w-0">
              <button
                type="button"
                onclick={() => toggleGateway(gw)}
                class="shrink-0 text-muted-foreground hover:text-foreground transition-colors"
                aria-label={allChecked
                  ? "Deselect all routes"
                  : "Select all routes"}
              >
                {#if allChecked}
                  <CheckSquareIcon size={14} class="text-primary" />
                {:else}
                  <SquareIcon size={14} />
                {/if}
              </button>
              <span class="text-xs font-semibold text-foreground truncate"
                >{gw.name}</span
              >
              <Badge variant="secondary" class="shrink-0 text-[10px]"
                >{gw.protocolType}</Badge
              >
              <Badge
                variant="outline"
                class="shrink-0 text-[10px] px-1 py-0 font-mono"
                >{gw.version}</Badge
              >
              <span
                class="hidden sm:block font-mono text-[10px] text-muted-foreground/70 truncate"
              >
                {normalizeInvokeUrl(gw.invokeUrl)}
              </span>
            </div>
            {#if gwDone}
              <button
                type="button"
                onclick={() => downloadGateway(gw)}
                class="inline-flex shrink-0 items-center gap-1 rounded-lg border border-primary/50 bg-primary/10 px-2 py-1 text-[11px] text-primary hover:bg-primary/20 transition-colors"
              >
                <DownloadSimpleIcon size={11} />
                Collection
              </button>
            {/if}
          </div>

          <!-- Route rows -->
          <div class="divide-y divide-border/50">
            {#each gwRoutes as detail (detail.routeKey)}
              {@const id = rid(gw.apiId, detail.routeKey)}
              {@const round = results.get(id)}
              {@const isProbing = probing && currentKey === id}
              {@const isExpanded = expanded.has(id)}

              <div>
                <!-- Route row -->
                <div class="flex items-center gap-2 px-3 py-2 text-[11px]">
                  <!-- Checkbox -->
                  <button
                    type="button"
                    onclick={() => toggleRoute(id)}
                    disabled={probing}
                    class="shrink-0 text-muted-foreground hover:text-foreground transition-colors disabled:opacity-40"
                    aria-label={selected.has(id) ? "Deselect" : "Select"}
                  >
                    {#if selected.has(id)}
                      <CheckSquareIcon size={13} class="text-primary" />
                    {:else}
                      <SquareIcon size={13} />
                    {/if}
                  </button>

                  <!-- Method -->
                  <Badge
                    variant={methodVariant(detail.method ?? "GET")}
                    class="shrink-0 text-[10px] font-mono"
                  >
                    {detail.method ?? "GET"}
                  </Badge>

                  <!-- Path -->
                  <span class="flex-1 truncate font-mono text-muted-foreground">
                    {detail.path ?? detail.routeKey}
                  </span>

                  <!-- Status -->
                  {#if isProbing}
                    <span class="flex items-center gap-1 text-primary shrink-0">
                      <SpinnerGapIcon size={10} class="animate-spin" />
                      probing
                    </span>
                  {:else if round}
                    {@const codes = round.examples?.map(
                      (e) => e.statusCode,
                    ) ?? [round.statusCode]}
                    <span
                      class="font-mono {statusColor(round.statusCode)} shrink-0"
                    >
                      {round.statusCode}
                    </span>
                    {#if codes.length > 1}
                      <span
                        class="font-mono text-muted-foreground/70/60 text-[10px] shrink-0"
                      >
                        {codes.join("→")}
                      </span>
                    {/if}
                    <span class="text-muted-foreground/70 shrink-0"
                      >{round.durationMs}ms</span
                    >
                    {#if round.examples?.length}
                      <button
                        type="button"
                        onclick={() => toggleExpand(id)}
                        class="flex items-center gap-0.5 text-muted-foreground/70 hover:text-foreground transition-colors shrink-0"
                        aria-label={isExpanded
                          ? "Collapse examples"
                          : "Expand examples"}
                      >
                        {#if isExpanded}
                          <CaretDownIcon size={11} />
                        {:else}
                          <CaretRightIcon size={11} />
                        {/if}
                        <span class="text-[10px]"
                          >{round.examples.length} example{round.examples
                            .length !== 1
                            ? "s"
                            : ""}</span
                        >
                      </button>
                    {/if}
                  {:else if probing && selected.has(id)}
                    <span class="text-muted-foreground/70/50 shrink-0"
                      >queued</span
                    >
                  {:else if selected.has(id)}
                    <span class="text-muted-foreground/70/40 shrink-0"
                      >pending</span
                    >
                  {/if}
                </div>

                <!-- Examples expansion -->
                {#if isExpanded && round?.examples?.length}
                  {@const total = round.examples.length}
                  <div
                    class="border-t border-border/40 bg-background divide-y divide-border/30"
                  >
                    {#each round.examples as ex, i (i)}
                      {@const ek = exKey(id, i)}
                      {@const showReq = exShowReq.has(ek)}
                      {@const formatted = exFormatted.has(ek)}
                      {@const reqHeaders = reqHeaderEntries(ex)}
                      {@const hasReqBody =
                        ex.requestBody !== undefined && ex.requestBody !== null}
                      {@const hasResBody = !!ex.body}
                      {@const canFormat = hasResBody && isJson(ex.body!)}

                      <div>
                        <!-- Example header row -->
                        <div
                          class="flex items-center gap-2 px-3 py-1.5 font-mono text-[10px]"
                        >
                          <span
                            class="text-muted-foreground/70/60 shrink-0 max-w-[7rem] truncate"
                            title={roundLabel(i, total, ex.label)}
                            >{roundLabel(i, total, ex.label)}</span
                          >
                          <span
                            class="{statusColor(
                              ex.statusCode,
                            )} font-semibold shrink-0">{ex.statusCode}</span
                          >
                          <span class="text-muted-foreground/70 shrink-0"
                            >{ex.durationMs}ms</span
                          >
                          <span class="flex-1"></span>
                          <button
                            type="button"
                            onclick={() => toggleExReq(id, i)}
                            class="rounded-lg px-1.5 py-0.5 text-[9px] transition-colors shrink-0
															{showReq
                              ? 'bg-popover text-foreground border border-border'
                              : 'text-muted-foreground/70/60 hover:text-foreground'}"
                            title="Show request inputs">req</button
                          >
                          {#if canFormat}
                            <button
                              type="button"
                              onclick={() => toggleExFormat(id, i)}
                              class="rounded-lg px-1.5 py-0.5 text-[9px] font-sans transition-colors shrink-0
																{formatted
                                ? 'bg-popover text-foreground border border-border'
                                : 'text-muted-foreground/70/60 hover:text-foreground'}"
                              title="Toggle JSON formatting"
                              >&#123;&#125;</button
                            >
                          {/if}
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
                                  <pre
                                    class="font-mono text-[10px] text-muted-foreground whitespace-pre-wrap break-all leading-relaxed">{tryFormat(
                                      ex.requestBody ?? "",
                                    )}</pre>
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
                            <pre
                              class="font-mono text-[10px] text-muted-foreground whitespace-pre-wrap break-all leading-relaxed max-h-56 overflow-y-auto rounded">{formatted
                                ? tryFormat(ex.body!)
                                : truncate(ex.body!, 800)}</pre>
                          </div>
                        {/if}
                      </div>
                    {/each}
                  </div>
                {/if}

                <!-- Stuck / needs-input panel -->
                {#if round?.needsInput && !reProbing.has(id)}
                  <div
                    class="border-t border-amber-500/20 bg-amber-500/5 px-3 py-2.5"
                  >
                    <p class="mb-2 font-mono text-[10px] text-amber-400">
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
                              class="w-full rounded-lg border border-border bg-background px-2 py-1 font-mono text-[10px] text-foreground placeholder:text-muted-foreground/70/40 focus:border-amber-500/60 focus:outline-none"
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
                    <button
                      type="button"
                      onclick={() => reprobeRoute(gw, detail, id)}
                      class="inline-flex items-center gap-1.5 rounded-lg border border-amber-500/40 bg-amber-500/10 px-2.5 py-1 text-[10px] text-amber-400 hover:bg-amber-500/20 transition-colors"
                    >
                      <PlayIcon size={10} />
                      Re-probe with overrides
                    </button>
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
        </div>
      {/each}
    </div>

    <!-- Progress bar while probing -->
    {#if probing}
      <div
        class="relative h-7 rounded-lg border border-border overflow-hidden bg-background"
      >
        <div
          class="absolute inset-y-0 left-0 bg-primary/15 transition-all duration-300"
          style="width: {totalSelected > 0
            ? Math.round((results.size / totalSelected) * 100)
            : 0}%"
        ></div>
        <div class="relative flex h-full items-center justify-between px-3">
          <span class="font-mono text-[10px] text-primary">
            {results.size} / {totalSelected} probed
          </span>
          <span class="font-mono text-[10px] text-muted-foreground/70">
            {totalExamples} example{totalExamples !== 1 ? "s" : ""} captured
          </span>
        </div>
      </div>
    {/if}

    <!-- Done: full-width download bar  |  [=== stats ===| ↓ Download All ] -->
    {#if !probing && results.size > 0}
      <div class="flex rounded-lg border border-border overflow-hidden text-xs">
        <!-- left: stats fill -->
        <div class="flex flex-1 items-center gap-4 bg-card px-4 py-2.5 min-w-0">
          <span class="font-mono text-muted-foreground/70 shrink-0">
            {results.size} route{results.size !== 1 ? "s" : ""}
          </span>
          <span class="text-border select-none">|</span>
          <span
            class="font-mono shrink-0 {successCount > 0
              ? 'text-green-500'
              : successCount === 0
                ? 'text-muted-foreground/70'
                : ''}"
          >
            {successCount} success
          </span>
          <span class="text-border select-none">|</span>
          <span class="font-mono text-muted-foreground/70 shrink-0">
            {totalExamples} example{totalExamples !== 1 ? "s" : ""}
          </span>
          <span class="text-border select-none hidden sm:inline">|</span>
          <span
            class="hidden sm:block font-mono text-[10px] text-muted-foreground/70/60 truncate"
          >
            {probeableGateways.map((g) => g.name).join(" · ")}
          </span>
        </div>
        <!-- right: action buttons -->
        <div
          class="flex shrink-0 divide-x divide-border border-l border-border"
        >
          <button
            type="button"
            onclick={run}
            class="flex items-center gap-1.5 px-3 py-2.5 text-muted-foreground/70 hover:bg-popover hover:text-foreground transition-colors"
            title="Re-run probes"
          >
            <PlayIcon size={11} />
            <span class="hidden sm:inline">Re-run</span>
          </button>
          <button
            type="button"
            onclick={downloadAll}
            class="flex items-center gap-1.5 px-4 py-2.5 bg-primary/10 text-primary hover:bg-primary/20 transition-colors font-medium"
          >
            <DownloadSimpleIcon size={12} />
            Download All
          </button>
        </div>
      </div>
    {/if}
  {/if}
</div>
