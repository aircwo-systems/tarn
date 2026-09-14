<script lang="ts">
  import { CheckIcon, CopyIcon, DownloadSimpleIcon } from "phosphor-svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcStat from "$lib/components/rack/rc-stat.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import RcTonePill, { type Tone } from "$lib/components/rack/rc-tone-pill.svelte";
  import type { GatewaySummary, RouteDetail } from "$lib/types";
  import {
    slugify,
    normalizeTemplate,
    parseTarget,
    buildPostmanCollection,
    buildPostmanEnvironment,
    downloadJSON,
  } from "$lib/postman";

  let {
    gateway,
  }: {
    gateway: GatewaySummary;
  } = $props();

  const numberFormatter = new Intl.NumberFormat("en-GB");

  let urlCopied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | undefined;

  function copyInvokeUrl() {
    navigator.clipboard.writeText(gateway.invokeUrl).then(() => {
      urlCopied = true;
      clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (urlCopied = false), 1500);
    });
  }

  function routeParts(routeKey: string): { method: string; path: string } {
    if (routeKey === "$default") return { method: "$default", path: "/" };
    const firstSpace = routeKey.indexOf(" ");
    if (firstSpace === -1) return { method: "ANY", path: routeKey };
    return {
      method: routeKey.slice(0, firstSpace),
      path: routeKey.slice(firstSpace + 1) || "/",
    };
  }

  function methodTone(method: string): Tone {
    const m = method.toUpperCase();
    if (m === "GET") return "green";
    if (m === "POST" || m === "PUT" || m === "PATCH") return "amber";
    if (m === "DELETE") return "red";
    return "neutral";
  }

  function integrationLabel(type: string): string {
    switch (type) {
      case "AWS_PROXY":
        return "Lambda Proxy";
      case "AWS":
        return "AWS";
      case "HTTP_PROXY":
        return "HTTP Proxy";
      case "HTTP":
        return "HTTP";
      case "MOCK":
        return "Mock";
      default:
        return type;
    }
  }

  function hasTemplates(detail: RouteDetail): boolean {
    return !!(
      detail.requestTemplates && Object.keys(detail.requestTemplates).length > 0
    );
  }

  function hasParams(detail: RouteDetail): boolean {
    return !!(
      detail.requestParameters &&
      Object.keys(detail.requestParameters).length > 0
    );
  }

  function downloadCollection() {
    downloadJSON(
      `${slugify(gateway.name)}.postman_collection.json`,
      buildPostmanCollection(gateway),
    );
  }

  function downloadEnvironment() {
    downloadJSON(
      `${slugify(gateway.name)}.postman_environment.json`,
      buildPostmanEnvironment(gateway),
    );
  }

  const config = $derived([
    { label: "API ID", value: gateway.apiId, mono: true },
    { label: "Endpoint", value: gateway.apiEndpoint || gateway.invokeUrl, mono: true },
    { label: "Stage", value: gateway.defaultStage || "--", mono: true, dim: !gateway.defaultStage },
    ...(gateway.description ? [{ label: "Description", value: gateway.description }] : []),
    { label: "ARN", value: gateway.arn, mono: true, dim: true },
  ]);

  const tags = $derived(Object.entries(gateway.tags ?? {}));
</script>

<div class="detail">
  <!-- Hero -->
  <header class="hero">
    <div class="identity">
      <div class="title-row">
        <h1 title={gateway.name}>{gateway.name}</h1>
        <RcTonePill tone="neutral">{gateway.protocolType}</RcTonePill>
        <RcTonePill tone="neutral">{gateway.version}</RcTonePill>
      </div>
      <p class="subline">
        {#if gateway.defaultStage}<span>stage: {gateway.defaultStage}</span><i></i>{/if}<span
          class="invoke"
          title={gateway.invokeUrl}>{gateway.invokeUrl}</span
        >
        <button type="button" class="copy-btn" onclick={copyInvokeUrl} aria-label="Copy invoke URL">
          {#if urlCopied}<CheckIcon size={11} class="ok" />{:else}<CopyIcon size={11} />{/if}
        </button>
      </p>
    </div>
    <div class="hero-actions">
      <button type="button" class="btn" onclick={downloadCollection}>
        <DownloadSimpleIcon size={12} />Collection
      </button>
      <button type="button" class="btn" onclick={downloadEnvironment}>
        <DownloadSimpleIcon size={12} />Env
      </button>
    </div>
  </header>

  <!-- Numbers -->
  <div class="stats">
    <RcStat
      label="Routes"
      value={numberFormatter.format(gateway.routeDetails?.length ?? gateway.routes)}
      sub="route handlers"
    />
    <RcStat
      label="Integrations"
      value={numberFormatter.format(gateway.integrations)}
      sub="backend targets"
      tone={gateway.integrations > 0 ? "green" : undefined}
    />
    <RcStat label="Stages" value={numberFormatter.format(gateway.stages)} sub="deployed" />
  </div>

  <RcPanel
    title="Routes & integrations"
    description="Methods, targets and request shapes on this gateway."
    index={0}
  >
    {#if gateway.routeDetails?.length}
      <div class="routes">
        {#each gateway.routeDetails as detail (detail.routeKey)}
          {@const target = detail.integrationTarget
            ? parseTarget(detail.integrationTarget)
            : null}
          <div class="route">
            <div class="route-head">
              <RcTonePill tone={methodTone(detail.method ?? "")}>{detail.method ?? "—"}</RcTonePill>
              <span class="route-path">{detail.path ?? detail.routeKey}</span>
              {#if detail.integrationType}
                <span class="route-integration">{integrationLabel(detail.integrationType)}</span>
              {/if}
            </div>
            {#if target}
              <div class="route-target">
                <span class="target-kind">{target.kind}</span>
                <span class="target-name">{target.name}</span>
              </div>
            {/if}
            {#if hasTemplates(detail)}
              {#each Object.entries(detail.requestTemplates!) as [contentType, template] (contentType)}
                <div class="route-block">
                  <p class="route-block-label">{contentType} · template</p>
                  <pre class="route-pre">{normalizeTemplate(template)}</pre>
                </div>
              {/each}
            {/if}
            {#if hasParams(detail)}
              <div class="route-block">
                <p class="route-block-label">parameters</p>
                <div class="route-params">
                  {#each Object.entries(detail.requestParameters!) as [key, value] (key)}
                    <div class="param"><span>{key}</span><span>{value}</span></div>
                  {/each}
                </div>
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {:else if gateway.routeKeys?.length}
      <div class="routes">
        {#each gateway.routeKeys as routeKey (routeKey)}
          {@const route = routeParts(routeKey)}
          <div class="route">
            <div class="route-head">
              <RcTonePill tone={methodTone(route.method)}>{route.method}</RcTonePill>
              <span class="route-path">{route.path}</span>
            </div>
          </div>
        {/each}
      </div>
    {:else}
      <p class="empty">No routes configured for this gateway.</p>
    {/if}
  </RcPanel>

  <RcPanel title="Configuration" index={1}>
    <RcKv items={config} />
    {#if tags.length > 0}
      <div class="tags">
        {#each tags as [k, v] (k)}
          <span class="tag"><span>{k}</span>{v}</span>
        {/each}
      </div>
    {/if}
  </RcPanel>
</div>

<style>
  .detail { display: flex; flex-direction: column; gap: 14px; min-width: 0; }

  .hero { display: flex; align-items: flex-end; justify-content: space-between; gap: 16px; flex-wrap: wrap; padding: 4px 2px 2px; }
  .identity { min-width: 0; }
  .title-row { display: flex; align-items: center; gap: 8px; min-width: 0; flex-wrap: wrap; }
  h1 {
    font: 600 19px var(--font-sans, ui-sans-serif, system-ui, sans-serif); letter-spacing: -0.02em; color: var(--text-primary);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .subline { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 4px; font-size: 11.5px; color: var(--text-tertiary); }
  .subline i { width: 3px; height: 3px; border-radius: 1px; background: var(--border-default); }
  .invoke { font-family: var(--font-mono, ui-monospace, monospace); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 32rem; }
  .copy-btn { display: inline-flex; color: var(--text-tertiary); transition: color 120ms ease; }
  .copy-btn:hover { color: var(--text-primary); }
  .copy-btn :global(.ok) { color: var(--accent-green); }
  .hero-actions { display: flex; gap: 6px; }

  .btn {
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 11px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11.5px; color: var(--text-secondary); text-decoration: none;
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .btn:hover { color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element-hover); }
  .btn:active { transform: scale(0.96); }
  .btn:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }

  .stats {
    display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px; padding: 14px 2px 10px;
    animation: statsIn 320ms var(--ease-snappy) both;
  }
  @keyframes statsIn { from { opacity: 0; transform: translateY(4px); } }
  @media (max-width: 1100px) { .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); } }

  .empty { font-size: 11.5px; color: var(--text-tertiary); }

  .routes { display: flex; flex-direction: column; gap: 10px; }
  .route { border: 1px solid var(--border-subtle); border-radius: 8px; background: var(--bg-app); overflow: hidden; }
  .route-head { display: flex; align-items: center; gap: 8px; padding: 8px 10px; min-width: 0; }
  .route-path { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 12px var(--font-mono, ui-monospace, monospace); color: var(--text-primary); }
  .route-integration { flex-shrink: 0; font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .route-target { display: flex; align-items: center; gap: 8px; border-top: 1px solid var(--border-subtle); padding: 6px 10px; min-width: 0; }
  .target-kind {
    flex-shrink: 0; border: 1px solid var(--border-subtle); border-radius: 6px; background: var(--bg-stage);
    padding: 1px 6px; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary);
  }
  .target-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); }
  .route-block { border-top: 1px solid var(--border-subtle); padding: 8px 10px; }
  .route-block-label { font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); margin-bottom: 6px; }
  .route-pre {
    overflow-x: auto; font: 11px/1.7 var(--font-mono, ui-monospace, monospace); color: var(--text-secondary);
    white-space: pre;
  }
  .route-params { display: flex; flex-direction: column; gap: 4px; }
  .param { display: flex; gap: 8px; font: 11px var(--font-mono, ui-monospace, monospace); }
  .param span:first-child { color: var(--text-tertiary); flex-shrink: 0; }
  .param span:last-child { color: var(--text-secondary); word-break: break-all; }

  .tags { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 14px; }
  .tag {
    display: inline-flex; gap: 6px; height: 22px; align-items: center; padding: 0 8px; border-radius: 8px;
    background: var(--bg-element); font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-primary);
  }
  .tag span { color: var(--text-tertiary); }

  @media (prefers-reduced-motion: reduce) { .stats { animation: none; } }
</style>
