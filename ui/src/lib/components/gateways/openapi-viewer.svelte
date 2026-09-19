<script lang="ts" module>
  // Minimal slice of OpenAPI 3.1 that Tarn generates.
  export type JSONSchema = {
    type?: string;
    properties?: Record<string, JSONSchema>;
    required?: string[];
    items?: JSONSchema;
  };
  type Parameter = { name: string; in: string; required?: boolean };
  type Operation = {
    operationId?: string;
    summary?: string;
    tags?: string[];
    parameters?: Parameter[];
    requestBody?: {
      required?: boolean;
      content?: Record<string, { schema?: JSONSchema; example?: unknown }>;
    };
    "x-tarn-integration"?: { type?: string; target?: string };
  };
  export type OpenAPIDoc = {
    openapi: string;
    info: { title: string; version: string; description?: string };
    servers?: Array<{ url: string; description?: string }>;
    paths: Record<string, Record<string, Operation>>;
  };

  const ANY_METHOD = "x-amazon-apigateway-any-method";
  const METHOD_ORDER = ["get", "post", "put", "patch", "delete", "head", "options", ANY_METHOD];
</script>

<script lang="ts">
  import { CaretRightIcon, CheckIcon, CopySimpleIcon, DownloadSimpleIcon } from "phosphor-svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcTonePill, { type Tone } from "$lib/components/rack/rc-tone-pill.svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import { fetchOpenAPI } from "$lib/api";
  import { formatJSONForViewer } from "$lib/json-format";
  import { downloadJSON, parseTarget, slugify } from "$lib/postman";
  import type { GatewaySummary } from "$lib/types";

  let { gateway, index = 0 }: { gateway: GatewaySummary; index?: number } = $props();

  let doc = $state<OpenAPIDoc | null>(null);
  let loading = $state(false);
  let error = $state<string | null>(null);
  let view = $state<"docs" | "json">("docs");
  let open = $state<Record<string, boolean>>({});
  let copied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | undefined;

  // Refetch when the gateway or its routes change.
  const fingerprint = $derived(
    `${gateway.apiId}|${(gateway.routeDetails ?? []).map((d) => d.routeKey).join(",")}`,
  );

  $effect(() => {
    const apiId = gateway.apiId;
    void fingerprint;
    const ctrl = new AbortController();
    loading = true;
    error = null;
    fetchOpenAPI(apiId, ctrl.signal)
      .then((d) => {
        doc = d as OpenAPIDoc;
      })
      .catch((err) => {
        if (ctrl.signal.aborted) return;
        error = err instanceof Error ? err.message : String(err);
      })
      .finally(() => {
        if (!ctrl.signal.aborted) loading = false;
      });
    return () => ctrl.abort();
  });

  const operations = $derived(
    doc
      ? Object.entries(doc.paths)
          .sort(([a], [b]) => a.localeCompare(b))
          .flatMap(([path, item]) =>
            Object.entries(item)
              .sort(([a], [b]) => METHOD_ORDER.indexOf(a) - METHOD_ORDER.indexOf(b))
              .map(([method, op]) => ({ key: `${method} ${path}`, method, path, op })),
          )
      : [],
  );

  const json = $derived(doc ? JSON.stringify(doc, null, 2) : "");
  const jsonView = $derived(json ? formatJSONForViewer(json) : null);

  function methodLabel(method: string): string {
    return method === ANY_METHOD ? "ANY" : method.toUpperCase();
  }

  function methodTone(method: string): Tone {
    const m = methodLabel(method);
    if (m === "GET") return "green";
    if (m === "POST" || m === "PUT" || m === "PATCH") return "amber";
    if (m === "DELETE") return "red";
    return "neutral";
  }

  function schemaType(s: JSONSchema | undefined): string {
    if (!s?.type) return "any";
    if (s.type === "array") return `${schemaType(s.items)}[]`;
    return s.type;
  }

  function copyJSON() {
    navigator.clipboard.writeText(json).then(() => {
      copied = true;
      clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (copied = false), 1500);
    });
  }

  function download() {
    if (doc) downloadJSON(`${slugify(gateway.name)}.openapi.json`, doc);
  }
</script>

{#snippet schemaRows(schema: JSONSchema | undefined, depth: number)}
  {#if schema?.type === "object" && schema.properties}
    {#each Object.entries(schema.properties) as [name, child] (name)}
      <div class="field" style:--depth={depth}>
        <span class="field-name">{name}</span>
        <span class="field-type">{schemaType(child)}</span>
        {#if schema.required?.includes(name)}<span class="field-req">required</span>{/if}
      </div>
      {@render schemaRows(child.type === "array" ? child.items : child, depth + 1)}
    {/each}
  {/if}
{/snippet}

<RcPanel title="API spec" description="Generated OpenAPI 3.1 for this gateway." {index}>
  {#snippet actions()}
    <div class="segmented" role="radiogroup" aria-label="Spec view">
      <span class="segmented-pill" style="transform: translateX({view === 'docs' ? 0 : 100}%)"></span>
      <button type="button" role="radio" aria-checked={view === "docs"} class:on={view === "docs"} onclick={() => (view = "docs")}>Docs</button>
      <button type="button" role="radio" aria-checked={view === "json"} class:on={view === "json"} onclick={() => (view = "json")}>JSON</button>
    </div>
    <button type="button" class="icon-btn" onclick={copyJSON} disabled={!doc} aria-label="Copy spec JSON" title="Copy JSON">
      {#if copied}<CheckIcon size={12} class="ok" />{:else}<CopySimpleIcon size={12} />{/if}
    </button>
    <button type="button" class="icon-btn" onclick={download} disabled={!doc} aria-label="Download spec" title="Download .openapi.json">
      <DownloadSimpleIcon size={12} />
    </button>
  {/snippet}

  {#if error}
    <p class="error">{error}</p>
  {:else if !doc}
    <p class="empty">{loading ? "Generating spec…" : "No spec available."}</p>
  {:else if view === "json"}
    {#if jsonView}
      <FormattedMessageViewer
        raw={json}
        formatted={jsonView.formatted}
        formattedHtml={jsonView.formattedHtml}
        formattedOpenByDefault={true}
        rawOpenByDefault={false}
        formattedContentClass="text-[11px] text-foreground"
        formattedMaxHeightClass="max-h-[36rem]"
      />
    {/if}
  {:else}
    <div class="meta">
      <span>OpenAPI {doc.openapi}</span>
      <i></i>
      <span>{doc.info.version}</span>
      {#each doc.servers ?? [] as server (server.url)}
        <i></i><span class="mono">{server.url}</span>
      {/each}
    </div>

    {#if operations.length === 0}
      <p class="empty">No operations — this gateway has no routes yet.</p>
    {:else}
      <div class="ops">
        {#each operations as { key, method, path, op } (key)}
          {@const isOpen = !!open[key]}
          {@const params = op.parameters ?? []}
          {@const media = op.requestBody?.content?.["application/json"]}
          {@const target = op["x-tarn-integration"]?.target ? parseTarget(op["x-tarn-integration"].target) : null}
          <div class="op" class:open={isOpen}>
            <button type="button" class="op-head" aria-expanded={isOpen} onclick={() => (open[key] = !isOpen)}>
              <CaretRightIcon size={10} class="caret" />
              <RcTonePill tone={methodTone(method)}>{methodLabel(method)}</RcTonePill>
              <span class="op-path">{path}</span>
              {#if target}<span class="op-target">{target.kind} {target.name}</span>{/if}
            </button>

            {#if isOpen}
              <div class="op-body">
                {#if op.operationId}
                  <div class="kv"><span>operationId</span><span class="mono">{op.operationId}</span></div>
                {/if}

                {#if params.length > 0}
                  <p class="block-label">Parameters</p>
                  <div class="fields">
                    {#each params as p (p.in + p.name)}
                      <div class="field">
                        <span class="field-name">{p.name}</span>
                        <span class="field-type">{p.in}</span>
                        {#if p.required}<span class="field-req">required</span>{/if}
                      </div>
                    {/each}
                  </div>
                {/if}

                {#if media}
                  <p class="block-label">Request body · application/json{op.requestBody?.required ? "" : " · optional"}</p>
                  {#if media.schema?.properties}
                    <div class="fields">{@render schemaRows(media.schema, 0)}</div>
                  {:else}
                    <p class="empty">{schemaType(media.schema)} — no example to infer fields from.</p>
                  {/if}
                  {#if media.example !== undefined}
                    {@const ex = JSON.stringify(media.example, null, 2)}
                    {@const exView = formatJSONForViewer(ex)}
                    <p class="block-label">Example</p>
                    {#if exView}
                      <FormattedMessageViewer
                        raw={ex}
                        formatted={exView.formatted}
                        formattedHtml={exView.formattedHtml}
                        formattedContentClass="text-[11px] text-foreground"
                        formattedMaxHeightClass="max-h-[18rem]"
                      />
                    {/if}
                  {/if}
                {/if}

                {#if params.length === 0 && !media}
                  <p class="empty">No parameters or request body.</p>
                {/if}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  {/if}
</RcPanel>

<style>
  .empty { font-size: 11.5px; color: var(--text-tertiary); }
  .error { font-size: 11.5px; color: var(--accent-red); }
  .mono { font-family: var(--font-mono, ui-monospace, monospace); }

  .segmented {
    position: relative; display: inline-grid; grid-template-columns: repeat(2, 56px); padding: 2px;
    border-radius: 8px; border: 1px solid var(--border-subtle); background: var(--bg-app);
  }
  .segmented-pill {
    position: absolute; top: 2px; bottom: 2px; left: 2px; width: 56px; border-radius: 6px;
    background: var(--bg-stage); border: 1px solid var(--border-default);
    transition: transform 280ms var(--ease-snappy);
  }
  .segmented button {
    position: relative; height: 22px; font-size: 11.5px; color: var(--text-tertiary); transition: color 140ms ease;
  }
  .segmented button:hover { color: var(--text-secondary); }
  .segmented button.on { color: var(--text-primary); }

  .icon-btn {
    display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 28px; border-radius: 8px;
    border: 1px solid var(--border-subtle); color: var(--text-secondary);
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .icon-btn:hover:not(:disabled) { color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element-hover); }
  .icon-btn:active:not(:disabled) { transform: scale(0.96); }
  .icon-btn:disabled { opacity: 0.4; }
  .icon-btn :global(.ok) { color: var(--accent-green); }

  .meta { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-bottom: 10px; font-size: 11px; color: var(--text-tertiary); }
  .meta i { width: 3px; height: 3px; border-radius: 1px; background: var(--border-default); }

  .ops { display: flex; flex-direction: column; gap: 8px; }
  .op { border: 1px solid var(--border-subtle); border-radius: 8px; background: var(--bg-app); overflow: hidden; transition: border-color 120ms ease; }
  .op.open { border-color: var(--border-default); }
  .op-head {
    display: flex; align-items: center; gap: 8px; width: 100%; padding: 7px 10px; min-width: 0; text-align: left;
    transition: background 120ms ease;
  }
  .op-head:hover { background: var(--bg-element-hover); }
  .op-head:focus-visible { outline: 1px solid var(--border-focus); outline-offset: -1px; }
  .op-head :global(.caret) { flex-shrink: 0; color: var(--text-tertiary); transition: transform 200ms var(--ease-snappy); }
  .op.open .op-head :global(.caret) { transform: rotate(90deg); }
  .op-path { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 12px var(--font-mono, ui-monospace, monospace); color: var(--text-primary); }
  .op-target { flex-shrink: 0; max-width: 40%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }

  .op-body { border-top: 1px solid var(--border-subtle); padding: 10px; display: flex; flex-direction: column; gap: 6px; animation: bodyIn 200ms var(--ease-snappy) both; }
  @keyframes bodyIn { from { opacity: 0; transform: translateY(-2px); } }
  .kv { display: flex; gap: 8px; font-size: 11px; }
  .kv span:first-child { color: var(--text-tertiary); }
  .kv span:last-child { color: var(--text-secondary); }
  .block-label { margin-top: 6px; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }

  .fields { display: flex; flex-direction: column; border: 1px solid var(--border-subtle); border-radius: 8px; overflow: hidden; }
  .field {
    display: flex; align-items: center; gap: 10px; padding: 5px 10px 5px calc(10px + var(--depth, 0) * 14px);
    font: 11px var(--font-mono, ui-monospace, monospace);
  }
  .field + .field { border-top: 1px solid var(--border-subtle); }
  .field-name { color: var(--text-primary); }
  .field-type { color: var(--text-tertiary); }
  .field-req { margin-left: auto; font-size: 10px; color: var(--accent-amber); }

  @media (prefers-reduced-motion: reduce) {
    .segmented-pill, .op-head :global(.caret) { transition: none; }
    .op-body { animation: none; }
  }
</style>
