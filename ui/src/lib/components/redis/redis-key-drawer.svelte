<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import {
    XIcon,
    TrashIcon,
    FloppyDiskIcon,
    WarningCircleIcon,
    PlusIcon,
    CopyIcon,
    CheckIcon,
    CodeIcon,
  } from "phosphor-svelte";
  import { getRedisKey, setRedisKey, deleteRedisKeys } from "#lib/api";
  import { formatRedisTTL, notifyRedisMutation, tryPrettyJson } from "#lib/redis";
  import { formatBytes } from "#lib/utils";
  import type {
    RedisAuthOptions,
    RedisHashEntry,
    RedisKeyDetail,
    RedisSetKeyRequest,
    RedisZSetMember,
  } from "#lib/types";

  let {
    targetId,
    auth,
    initialKey = null,
    onclose,
    onmutated,
  }: {
    targetId: string;
    auth: RedisAuthOptions;
    initialKey?: string | null;
    onclose: () => void;
    onmutated: () => void;
  } = $props();

  const isCreate = $derived(initialKey === null);
  type KeyType = RedisSetKeyRequest["type"];

  let loading = $state(false);
  let busy = $state(false);
  let error = $state("");
  let detail = $state<RedisKeyDetail | null>(null);
  let confirmDelete = $state(false);
  let copied = $state(false);

  let keyName = $state("");
  let keyType = $state<KeyType>("string");
  let ttlInput = $state(""); // blank = keep/no expiry, -1 = persist, >0 = seconds
  let stringValue = $state("");
  let hashRows = $state<RedisHashEntry[]>([{ field: "", value: "" }]);
  let listRows = $state<string[]>([""]);
  let setRows = $state<string[]>([""]);
  let zsetRows = $state<RedisZSetMember[]>([{ member: "", score: 0 }]);
  let streamId = $state("*");
  let streamRows = $state<RedisHashEntry[]>([{ field: "", value: "" }]);

  let controller: AbortController | null = null;
  let copyTimer: ReturnType<typeof setTimeout> | undefined;

  async function loadKey(key: string) {
    controller?.abort();
    const ac = new AbortController();
    controller = ac;
    loading = true;
    error = "";
    try {
      const res = await getRedisKey(targetId, { ...auth, key, limit: 200 }, ac.signal);
      if (ac.signal.aborted) return;
      detail = res;
      if (!res.found) {
        error = `Key "${key}" no longer exists.`;
        return;
      }
      keyName = res.key;
      const t = res.type.toLowerCase();
      if (t === "string" || t === "hash" || t === "list" || t === "set" || t === "zset" || t === "stream") {
        keyType = t;
      }
      ttlInput = res.ttlMs > 0 ? String(Math.ceil(res.ttlMs / 1000)) : "";
      if (t === "string") {
        const pretty = tryPrettyJson(res.stringValue ?? "");
        stringValue = pretty.formatted;
      } else if (t === "hash") {
        hashRows = res.hashEntries?.length ? res.hashEntries.map((e) => ({ ...e })) : [{ field: "", value: "" }];
      } else if (t === "list") {
        listRows = res.listItems?.length ? [...res.listItems] : [""];
      } else if (t === "set") {
        setRows = res.setMembers?.length ? [...res.setMembers] : [""];
      } else if (t === "zset") {
        zsetRows = res.zsetMembers?.length ? res.zsetMembers.map((m) => ({ ...m })) : [{ member: "", score: 0 }];
      }
    } catch (e) {
      if (ac.signal.aborted) return;
      error = e instanceof Error ? e.message : "Failed to inspect key";
    } finally {
      if (controller === ac) {
        loading = false;
        controller = null;
      }
    }
  }

  onMount(() => {
    if (initialKey !== null) {
      keyName = initialKey;
      void loadKey(initialKey);
    }
  });

  onDestroy(() => {
    controller?.abort();
    if (copyTimer) clearTimeout(copyTimer);
  });

  function formatStringJson() {
    error = "";
    try {
      stringValue = JSON.stringify(JSON.parse(stringValue), null, 2);
    } catch (e) {
      error = e instanceof Error ? e.message : "Invalid JSON";
    }
  }

  async function copyKeyName() {
    if (!keyName) return;
    try {
      await navigator.clipboard.writeText(keyName);
      copied = true;
      if (copyTimer) clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (copied = false), 1400);
    } catch {}
  }

  function parseTTL(): { ttlSeconds?: number; keepTtl?: boolean } {
    const raw = ttlInput.trim();
    if (raw === "") {
      return isCreate ? {} : { ttlSeconds: -1 };
    }
    const n = Number(raw);
    if (!Number.isFinite(n) || !Number.isInteger(n)) {
      throw new Error("TTL must be a whole number of seconds (or empty for no expiry)");
    }
    return { ttlSeconds: n };
  }

  async function handleSave() {
    const cleanKey = keyName.trim();
    if (!cleanKey) {
      error = "Key name is required";
      return;
    }
    busy = true;
    error = "";
    try {
      const ttl = parseTTL();
      const payload: RedisSetKeyRequest = {
        ...auth,
        key: cleanKey,
        type: keyType,
        ...ttl,
      };
      if (keyType === "string") {
        payload.stringValue = stringValue;
      } else if (keyType === "hash") {
        payload.hashEntries = hashRows.filter((r) => r.field.trim() !== "");
      } else if (keyType === "list") {
        payload.listItems = listRows.filter((v) => v !== "");
      } else if (keyType === "set") {
        payload.setMembers = setRows.filter((v) => v !== "");
      } else if (keyType === "zset") {
        payload.zsetMembers = zsetRows
          .filter((r) => r.member !== "")
          .map((r) => ({ member: r.member, score: Number(r.score) || 0 }));
      } else if (keyType === "stream") {
        const fields: Record<string, string> = {};
        for (const r of streamRows) {
          if (r.field.trim()) fields[r.field.trim()] = r.value;
        }
        payload.streamId = streamId.trim() || "*";
        payload.streamFields = fields;
      }

      await setRedisKey(targetId, payload);
      notifyRedisMutation(targetId, auth.db ?? 0);
      onmutated();
      onclose();
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to save key";
    } finally {
      busy = false;
    }
  }

  async function handleDelete() {
    if (!keyName) return;
    busy = true;
    error = "";
    try {
      await deleteRedisKeys(targetId, { ...auth, keys: [keyName] });
      notifyRedisMutation(targetId, auth.db ?? 0);
      onmutated();
      onclose();
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to delete key";
    } finally {
      busy = false;
    }
  }

  function onWindowKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") onclose();
  }
</script>

<svelte:window onkeydown={onWindowKeydown} />

<div class="overlay" onclick={onclose} role="presentation"></div>

<div
  class="drawer"
  role="dialog"
  aria-modal="true"
  aria-label={isCreate ? "New Redis key" : `Inspect Redis key ${keyName}`}
>
    <header class="drawer-head">
      <div class="head-title">
        <h2>{isCreate ? "New Redis Key" : keyName}</h2>
        <div class="head-meta">
          <span class="badge mono">db{auth.db ?? 0}</span>
          {#if detail?.found}
            <span class="badge mono">{detail.type}</span>
            <span class="badge mono">{formatRedisTTL(detail.ttlMs)}</span>
            {#if detail.encoding}<span class="badge mono">{detail.encoding}</span>{/if}
            {#if detail.memoryBytes}<span class="badge mono">{formatBytes(detail.memoryBytes)}</span>{/if}
          {/if}
        </div>
      </div>
      <div class="head-actions">
        {#if !isCreate}
          <button type="button" class="icon-btn" onclick={copyKeyName} title="Copy key name" aria-label="Copy key name">
            {#if copied}<CheckIcon size={14} />{:else}<CopyIcon size={14} />{/if}
          </button>
        {/if}
        <button type="button" class="icon-btn" onclick={onclose} aria-label="Close drawer">
          <XIcon size={15} />
        </button>
      </div>
    </header>

    <div class="drawer-body">
      {#if loading}
        <p class="note">Loading key value...</p>
      {:else}
        {#if detail?.truncated}
          <div class="info-banner">
            Large value truncated to keep the viewer fast ({detail.length.toLocaleString("en-GB")} total).
          </div>
        {/if}
        {#if detail?.isBinary}
          <div class="info-banner">
            Binary payload detected — shown as base64. Saving replaces it with UTF-8 text.
          </div>
        {/if}

        <div class="form-grid">
          <label class="field">
            <span>Key</span>
            <input class="mono" bind:value={keyName} readonly={!isCreate} placeholder="e.g. session:user:42" />
          </label>
          <label class="field">
            <span>Type</span>
            <select bind:value={keyType} disabled={!isCreate}>
              <option value="string">string</option>
              <option value="hash">hash</option>
              <option value="list">list</option>
              <option value="set">set</option>
              <option value="zset">zset</option>
              <option value="stream">stream</option>
            </select>
          </label>
          <label class="field">
            <span>TTL (seconds)</span>
            <input class="mono" bind:value={ttlInput} placeholder="Empty = no expiry" inputmode="numeric" />
          </label>
        </div>

        {#if keyType === "string"}
          <div class="editor-head">
            <span class="eyebrow">Value</span>
            <button type="button" class="mini-btn" onclick={formatStringJson}>
              <CodeIcon size={11} /> Format JSON
            </button>
          </div>
          <textarea class="code-area" bind:value={stringValue} rows="12" spellcheck="false" placeholder="String or JSON payload"></textarea>
        {:else if keyType === "hash"}
          <div class="editor-head">
            <span class="eyebrow">Fields ({hashRows.length})</span>
            <button type="button" class="mini-btn" onclick={() => (hashRows = [...hashRows, { field: "", value: "" }])}>
              <PlusIcon size={11} /> Add field
            </button>
          </div>
          <div class="kv-list">
            {#each hashRows as row, i (i)}
              <div class="kv-row">
                <input class="mono" placeholder="field" bind:value={row.field} />
                <input class="mono" placeholder="value" bind:value={row.value} />
                <button type="button" class="del-row" onclick={() => (hashRows = hashRows.filter((_, j) => j !== i))} aria-label="Remove field">&times;</button>
              </div>
            {/each}
          </div>
        {:else if keyType === "list"}
          <div class="editor-head">
            <span class="eyebrow">Items ({listRows.length})</span>
            <button type="button" class="mini-btn" onclick={() => (listRows = [...listRows, ""])}>
              <PlusIcon size={11} /> Add item
            </button>
          </div>
          <div class="kv-list">
            {#each listRows as _, i (i)}
              <div class="single-row indexed">
                <span class="idx mono">{i}</span>
                <input class="mono" placeholder="value" bind:value={listRows[i]} />
                <button type="button" class="del-row" onclick={() => (listRows = listRows.filter((_, j) => j !== i))} aria-label="Remove item">&times;</button>
              </div>
            {/each}
          </div>
        {:else if keyType === "set"}
          <div class="editor-head">
            <span class="eyebrow">Members ({setRows.length})</span>
            <button type="button" class="mini-btn" onclick={() => (setRows = [...setRows, ""])}>
              <PlusIcon size={11} /> Add member
            </button>
          </div>
          <div class="kv-list">
            {#each setRows as _, i (i)}
              <div class="single-row">
                <input class="mono" placeholder="member" bind:value={setRows[i]} />
                <button type="button" class="del-row" onclick={() => (setRows = setRows.filter((_, j) => j !== i))} aria-label="Remove member">&times;</button>
              </div>
            {/each}
          </div>
        {:else if keyType === "zset"}
          <div class="editor-head">
            <span class="eyebrow">Sorted Members ({zsetRows.length})</span>
            <button type="button" class="mini-btn" onclick={() => (zsetRows = [...zsetRows, { member: "", score: 0 }])}>
              <PlusIcon size={11} /> Add member
            </button>
          </div>
          <div class="kv-list">
            {#each zsetRows as row, i (i)}
              <div class="zset-row">
                <input class="mono" type="number" step="any" placeholder="score" bind:value={row.score} />
                <input class="mono" placeholder="member" bind:value={row.member} />
                <button type="button" class="del-row" onclick={() => (zsetRows = zsetRows.filter((_, j) => j !== i))} aria-label="Remove member">&times;</button>
              </div>
            {/each}
          </div>
        {:else if keyType === "stream"}
          {#if detail?.streamEntries?.length}
            <div class="editor-head"><span class="eyebrow">Recent Entries ({detail.streamEntries.length})</span></div>
            <div class="stream-list">
              {#each detail.streamEntries as entry (entry.id)}
                <div class="stream-entry">
                  <span class="stream-id mono">{entry.id}</span>
                  <code class="stream-fields">{JSON.stringify(entry.fields)}</code>
                </div>
              {/each}
            </div>
          {/if}
          <div class="editor-head">
            <span class="eyebrow">Append Entry (XADD)</span>
            <button type="button" class="mini-btn" onclick={() => (streamRows = [...streamRows, { field: "", value: "" }])}>
              <PlusIcon size={11} /> Add field
            </button>
          </div>
          <label class="field">
            <span>Entry ID</span>
            <input class="mono" bind:value={streamId} placeholder="*" />
          </label>
          <div class="kv-list">
            {#each streamRows as row, i (i)}
              <div class="kv-row">
                <input class="mono" placeholder="field" bind:value={row.field} />
                <input class="mono" placeholder="value" bind:value={row.value} />
                <button type="button" class="del-row" onclick={() => (streamRows = streamRows.filter((_, j) => j !== i))} aria-label="Remove field">&times;</button>
              </div>
            {/each}
          </div>
        {/if}

        {#if error}
          <div class="error-banner" role="alert">
            <WarningCircleIcon size={14} />
            <span>{error}</span>
          </div>
        {/if}
      {/if}
    </div>

    <footer class="drawer-foot">
      {#if !isCreate}
        {#if confirmDelete}
          <div class="confirm-row">
            <span>Delete key?</span>
            <button type="button" class="btn danger" disabled={busy} onclick={handleDelete}>Confirm</button>
            <button type="button" class="btn ghost" disabled={busy} onclick={() => (confirmDelete = false)}>Cancel</button>
          </div>
        {:else}
          <button type="button" class="btn ghost danger-text" disabled={busy || loading} onclick={() => (confirmDelete = true)}>
            <TrashIcon size={13} /> Delete
          </button>
        {/if}
      {:else}
        <span></span>
      {/if}
      <div class="foot-right">
        <button type="button" class="btn ghost" disabled={busy} onclick={onclose}>Close</button>
        <button type="button" class="btn primary" disabled={busy || loading} onclick={handleSave}>
          <FloppyDiskIcon size={13} />
          {busy ? "Saving..." : keyType === "stream" && !isCreate ? "Append Entry" : "Save Key"}
        </button>
      </div>
    </footer>
</div>

<style>
  .overlay {
    position: fixed; inset: 0; z-index: 80;
    background: rgba(0, 0, 0, 0.45);
    backdrop-filter: blur(2px);
  }
  .drawer {
    position: fixed;
    top: 50%; left: 50%;
    transform: translate(-50%, -50%);
    width: min(760px, calc(100vw - 24px));
    max-height: calc(100vh - 32px);
    background: var(--bg-app);
    border: 1px solid var(--border-default);
    border-radius: 10px;
    display: flex; flex-direction: column;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.35);
    z-index: 81;
    overflow: hidden;
  }
  .drawer-head {
    display: flex; align-items: flex-start; justify-content: space-between; gap: 12px;
    padding: 13px 16px; border-bottom: 1px solid var(--border-subtle);
    background: var(--bg-surface, var(--bg-app));
  }
  .head-title { min-width: 0; }
  .head-title h2 {
    margin: 0; font: 600 14px/1.3 var(--font-mono, ui-monospace, monospace);
    color: var(--text-primary); word-break: break-all;
  }
  .head-meta { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; }
  .badge {
    padding: 2px 7px; border-radius: 5px; font-size: 10.5px;
    background: var(--bg-element); border: 1px solid var(--border-subtle); color: var(--text-secondary);
  }
  .head-actions { display: flex; align-items: center; gap: 6px; }
  .icon-btn {
    width: 28px; height: 28px; border-radius: 6px;
    border: 1px solid var(--border-subtle); background: var(--bg-element);
    color: var(--text-secondary); display: inline-flex; align-items: center; justify-content: center; cursor: pointer;
  }
  .icon-btn:hover { color: var(--text-primary); border-color: var(--border-default); }
  .drawer-body {
    flex: 1; overflow-y: auto; padding: 16px 20px;
    display: flex; flex-direction: column; gap: 14px;
  }
  .form-grid { display: grid; grid-template-columns: 1.8fr 0.8fr 1fr; gap: 10px; }
  @media (max-width: 600px) { .form-grid { grid-template-columns: 1fr; } }
  .field { display: flex; flex-direction: column; gap: 5px; }
  .field span { font-size: 10.5px; color: var(--text-tertiary); text-transform: uppercase; letter-spacing: 0.05em; }
  .field input, .field select, .kv-row input, .single-row input, .zset-row input {
    height: 30px; padding: 0 9px; border-radius: 6px;
    border: 1px solid var(--border-subtle); background: var(--bg-element);
    color: var(--text-primary); font-size: 12px; outline: none;
  }
  .field input:focus, .field select:focus, .code-area:focus { border-color: var(--border-focus); }
  .field input[readonly] { opacity: 0.72; }
  .editor-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  .eyebrow { font-size: 10px; letter-spacing: 0.08em; text-transform: uppercase; color: var(--text-tertiary); }
  .mini-btn {
    display: inline-flex; align-items: center; gap: 5px;
    padding: 3px 8px; border-radius: 5px; border: 1px solid var(--border-subtle);
    background: var(--bg-element); color: var(--text-secondary); font-size: 11px; cursor: pointer;
  }
  .mini-btn:hover { color: var(--text-primary); border-color: var(--border-default); }
  .code-area {
    width: 100%; min-height: 220px; padding: 10px 12px; border-radius: 8px;
    border: 1px solid var(--border-subtle); background: var(--bg-element);
    color: var(--text-primary); font: 12px/1.5 var(--font-mono, ui-monospace, monospace);
    resize: vertical; outline: none;
  }
  .kv-list { display: flex; flex-direction: column; gap: 6px; }
  .kv-row { display: grid; grid-template-columns: 1fr 1.4fr 26px; gap: 6px; }
  .zset-row { display: grid; grid-template-columns: 110px 1fr 26px; gap: 6px; }
  .single-row { display: grid; grid-template-columns: 1fr 26px; gap: 6px; align-items: center; }
  .single-row.indexed { grid-template-columns: 28px 1fr 26px; }
  .idx { font-size: 11px; color: var(--text-tertiary); min-width: 22px; text-align: right; }
  .del-row {
    width: 26px; height: 30px; border-radius: 6px; border: 1px solid var(--border-subtle);
    background: transparent; color: var(--text-tertiary); cursor: pointer; font-size: 14px;
  }
  .del-row:hover { color: var(--accent-red); border-color: var(--accent-red); }
  .stream-list { display: flex; flex-direction: column; gap: 6px; max-height: 200px; overflow-y: auto; }
  .stream-entry {
    padding: 8px 10px; border-radius: 6px; border: 1px solid var(--border-subtle);
    background: var(--bg-element); display: flex; flex-direction: column; gap: 3px;
  }
  .stream-id { font-size: 11px; color: var(--accent-amber); }
  .stream-fields { font-size: 11px; color: var(--text-secondary); word-break: break-all; }
  .info-banner {
    padding: 8px 10px; border-radius: 7px; font-size: 11.5px;
    border: 1px solid color-mix(in srgb, var(--accent-amber) 35%, transparent);
    background: color-mix(in srgb, var(--accent-amber) 10%, transparent);
    color: var(--text-primary);
  }
  .error-banner {
    display: flex; align-items: center; gap: 8px; padding: 9px 11px; border-radius: 7px;
    border: 1px solid color-mix(in srgb, var(--accent-red) 40%, transparent);
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
    color: var(--accent-red); font-size: 12px;
  }
  .note { font-size: 12px; color: var(--text-tertiary); }
  .mono { font-family: var(--font-mono, ui-monospace, monospace); }
  .drawer-foot {
    display: flex; align-items: center; justify-content: space-between; gap: 10px;
    padding: 11px 16px; border-top: 1px solid var(--border-subtle);
    background: var(--bg-surface, var(--bg-app));
  }
  .foot-right, .confirm-row { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-secondary); }
  .btn {
    display: inline-flex; align-items: center; gap: 6px; height: 30px; padding: 0 12px;
    border-radius: 6px; border: 1px solid var(--border-subtle); font-size: 11.5px; font-weight: 500; cursor: pointer;
  }
  .btn.primary { background: var(--text-primary); border-color: var(--text-primary); color: var(--bg-app); }
  .btn.primary:hover:not(:disabled) { opacity: 0.9; }
  .btn.danger { background: var(--accent-red); border-color: var(--accent-red); color: #fff; font-weight: 600; }
  .btn.ghost { background: transparent; color: var(--text-secondary); }
  .btn.ghost:hover { color: var(--text-primary); background: var(--bg-element); }
  .btn.danger-text { color: var(--accent-red); }
  .btn:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
