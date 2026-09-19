<script lang="ts">
  import { untrack } from "svelte";
  import { PaperPlaneTiltIcon } from "phosphor-svelte";
  import RcTonePill, { type Tone } from "$lib/components/rack/rc-tone-pill.svelte";
  import { tryOperation, type TryOperationResponse } from "$lib/api";
  import { formatJSONForViewer } from "$lib/json-format";

  type Param = { name: string; in: string; required?: boolean };

  let {
    apiId,
    method,
    path,
    params = [],
    hasBody = false,
    example,
  }: {
    apiId: string;
    /** HTTP method, or "ANY" to let the user pick */
    method: string;
    path: string;
    params?: Param[];
    hasBody?: boolean;
    example?: unknown;
  } = $props();

  const ANY_METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"];

  // Seeded once from props; the user edits them from there.
  let chosenMethod = $state(untrack(() => (method === "ANY" ? "GET" : method)));
  let values = $state<Record<string, string>>({});
  let body = $state(
    untrack(() => (example === undefined ? (hasBody ? "{}" : "") : JSON.stringify(example, null, 2))),
  );
  let sending = $state(false);
  let error = $state<string | null>(null);
  let result = $state<TryOperationResponse | null>(null);
  let ctrl: AbortController | undefined;

  const sendsBody = $derived(hasBody && !["GET", "HEAD", "DELETE"].includes(chosenMethod));
  const missing = $derived(params.filter((p) => p.required && !values[key(p)]?.trim()));

  function key(p: Param): string {
    return `${p.in}:${p.name}`;
  }

  function resolvedPath(): string {
    return path.replace(/\{([A-Za-z0-9_]+)\}/g, (_, name: string) =>
      encodeURIComponent(values[`path:${name}`] ?? ""),
    );
  }

  function collect(location: string): Record<string, string> {
    const out: Record<string, string> = {};
    for (const p of params) {
      const v = values[key(p)];
      if (p.in === location && v?.trim()) out[p.name] = v;
    }
    return out;
  }

  async function send() {
    ctrl?.abort();
    ctrl = new AbortController();
    sending = true;
    error = null;
    try {
      result = await tryOperation(
        apiId,
        {
          method: chosenMethod,
          path: resolvedPath(),
          query: collect("query"),
          headers: collect("header"),
          body: sendsBody ? body : undefined,
        },
        ctrl.signal,
      );
    } catch (err) {
      if (ctrl.signal.aborted) return;
      result = null;
      error = err instanceof Error ? err.message : String(err);
    } finally {
      sending = false;
    }
  }

  function statusTone(code: number): Tone {
    if (code >= 500) return "red";
    if (code >= 400) return "amber";
    return "green";
  }

  const resultView = $derived(result ? formatJSONForViewer(result.body) : null);
</script>

<div class="try">
  <div class="try-head">
    <p class="block-label">Try it</p>
    {#if method === "ANY"}
      <select bind:value={chosenMethod} aria-label="Method">
        {#each ANY_METHODS as m (m)}<option value={m}>{m}</option>{/each}
      </select>
    {/if}
    <button
      type="button"
      class="send"
      onclick={send}
      disabled={sending || missing.length > 0}
      title={missing.length ? `Fill in ${missing.map((p) => p.name).join(", ")}` : "Send request"}
    >
      <PaperPlaneTiltIcon size={12} />{sending ? "Sending…" : "Send"}
    </button>
  </div>

  {#if params.length > 0}
    <div class="inputs">
      {#each params as p (key(p))}
        <label class="input-row">
          <span class="input-name">{p.name}</span>
          <span class="input-in">{p.in}</span>
          <input
            type="text"
            bind:value={values[key(p)]}
            placeholder={p.required ? "required" : "optional"}
            spellcheck="false"
            autocomplete="off"
          />
        </label>
      {/each}
    </div>
  {/if}

  {#if sendsBody}
    <textarea bind:value={body} rows={Math.min(14, Math.max(4, body.split("\n").length))} spellcheck="false" aria-label="Request body"></textarea>
  {/if}

  {#if error}
    <p class="error">{error}</p>
  {/if}

  {#if result}
    <div class="result">
      <div class="result-head">
        <RcTonePill tone={statusTone(result.status)}>{result.status}</RcTonePill>
        <span class="result-meta">{result.durationMs} ms</span>
        <span class="result-url" title={result.url}>{result.url}</span>
      </div>
      <p class="block-label">
        Response body{result.headers["Content-Type"] ? ` · ${result.headers["Content-Type"]}` : ""}
      </p>
      {#if result.body}
        <!-- highlightJSON output is HTML-escaped -->
        <pre class="code">{#if resultView}{@html resultView.formattedHtml}{:else}{result.body}{/if}</pre>
        {#if result.truncated}<p class="note">Body truncated at 1 MB.</p>{/if}
      {:else}
        <p class="note">Empty body.</p>
      {/if}
      <details class="headers">
        <summary>Response headers · {Object.keys(result.headers).length}</summary>
        <div class="header-rows">
          {#each Object.entries(result.headers).sort(([a], [b]) => a.localeCompare(b)) as [k, v] (k)}
            <div class="header-row"><span>{k}</span><span>{v}</span></div>
          {/each}
        </div>
      </details>
    </div>
  {/if}
</div>

<style>
  .try { display: flex; flex-direction: column; gap: 8px; margin-top: 6px; padding-top: 10px; border-top: 1px dashed var(--border-subtle); }
  .try-head { display: flex; align-items: center; gap: 8px; }
  .try-head .block-label { flex: 1; }
  .block-label { font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }

  select, input, textarea {
    border: 1px solid var(--border-subtle); border-radius: 8px; background: var(--bg-stage); color: var(--text-primary);
    font: 11px var(--font-mono, ui-monospace, monospace); transition: border-color 120ms ease;
  }
  select:focus, input:focus, textarea:focus { outline: none; border-color: var(--border-focus); }
  select { height: 26px; padding: 0 6px; }

  .send {
    display: inline-flex; align-items: center; gap: 6px; height: 26px; padding: 0 11px; border-radius: 8px;
    border: 1px solid color-mix(in srgb, var(--accent-green) 45%, transparent);
    background: color-mix(in srgb, var(--accent-green) 10%, transparent);
    font-size: 11.5px; color: var(--text-primary);
    transition: background 120ms ease, transform 120ms ease, opacity 120ms ease;
  }
  .send:hover:not(:disabled) { background: color-mix(in srgb, var(--accent-green) 18%, transparent); }
  .send:active:not(:disabled) { transform: scale(0.96); }
  .send:disabled { opacity: 0.45; }

  .inputs { display: flex; flex-direction: column; gap: 4px; }
  .input-row { display: grid; grid-template-columns: minmax(0, 9rem) 3.5rem minmax(0, 1fr); align-items: center; gap: 8px; }
  .input-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-primary); }
  .input-in { font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  input { height: 26px; padding: 0 8px; min-width: 0; }
  textarea { padding: 8px 10px; line-height: 1.6; resize: vertical; }

  .error { font-size: 11px; color: var(--accent-red); }
  .result { display: flex; flex-direction: column; gap: 6px; animation: resultIn 200ms var(--ease-snappy) both; }
  @keyframes resultIn { from { opacity: 0; transform: translateY(-2px); } }
  .result-head { display: flex; align-items: center; gap: 8px; min-width: 0; }
  .result-meta { flex-shrink: 0; font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); }
  .result-url { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .code {
    overflow: auto; max-height: 20rem; padding: 8px 10px; border: 1px solid var(--border-subtle); border-radius: 8px;
    background: var(--bg-stage); font: 11px/1.6 var(--font-mono, ui-monospace, monospace); color: var(--text-primary);
    white-space: pre-wrap; word-break: break-all;
  }
  .headers summary { cursor: pointer; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); transition: color 120ms ease; }
  .headers summary:hover { color: var(--text-secondary); }
  .header-rows { display: flex; flex-direction: column; gap: 3px; margin-top: 6px; }
  .header-row { display: flex; gap: 8px; font: 11px var(--font-mono, ui-monospace, monospace); }
  .header-row span:first-child { flex-shrink: 0; color: var(--text-tertiary); }
  .header-row span:last-child { color: var(--text-secondary); word-break: break-all; }
  .note { font-size: 11px; color: var(--text-tertiary); }

  @media (prefers-reduced-motion: reduce) { .result { animation: none; } }
</style>
