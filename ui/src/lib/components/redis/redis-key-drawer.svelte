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
    MagnifyingGlassIcon,
  } from "phosphor-svelte";
  import { getRedisKey, setRedisKey, mutateRedisKey, deleteRedisKeys } from "#lib/api";
  import { formatRedisTTL, notifyRedisMutation, tryPrettyJson } from "#lib/redis";
  import { formatBytes } from "#lib/utils";
  import type {
    RedisAuthOptions,
    RedisHashEntry,
    RedisKeyDetail,
    RedisMutateKeyRequest,
    RedisSetKeyRequest,
    RedisStreamEntry,
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

  const PAGE_SIZE = 50;
  const isCreate = $derived(initialKey === null);
  type KeyType = RedisSetKeyRequest["type"];

  let loading = $state(false);
  let loadingMore = $state(false);
  let busy = $state(false);
  let error = $state("");
  let statusNotice = $state("");
  let detail = $state<RedisKeyDetail | null>(null);
  let confirmDelete = $state(false);
  let copied = $state(false);

  // Key metadata & create/string state
  let keyName = $state("");
  let keyType = $state<KeyType>("string");
  let ttlInput = $state("");
  let stringValue = $state("");

  // Collection values (paginated when inspecting existing keys)
  let hashRows = $state<RedisHashEntry[]>([{ field: "", value: "" }]);
  let listRows = $state<string[]>([""]);
  let setRows = $state<string[]>([""]);
  let zsetRows = $state<RedisZSetMember[]>([{ member: "", score: 0 }]);
  let streamEntries = $state<RedisStreamEntry[]>([]);

  // Pagination & in-dialog search/filter state
  let rowFilter = $state("");
  let serverMatch = $state("");
  let nextCursor = $state("");
  let nextOffset = $state(0);
  let hasMore = $state(false);
  let totalLength = $state(0);

  // Inline incremental add controls (inspect mode)
  let addField = $state("");
  let addValue = $state("");
  let addScore = $state<number>(0);
  let streamId = $state("*");
  let streamRows = $state<RedisHashEntry[]>([{ field: "", value: "" }]);

  let controller: AbortController | null = null;
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  let noticeTimer: ReturnType<typeof setTimeout> | undefined;

  const isCollection = $derived(
    keyType === "hash" || keyType === "list" || keyType === "set" || keyType === "zset" || keyType === "stream",
  );
  const supportsServerMatch = $derived(keyType === "hash" || keyType === "set" || keyType === "zset");
  const isTruncatedString = $derived(!isCreate && keyType === "string" && !!detail?.truncated);

  const filterNeedle = $derived(rowFilter.trim().toLowerCase());
  const visibleHashRows = $derived(
    !filterNeedle
      ? hashRows
      : hashRows.filter(
          (r) => r.field.toLowerCase().includes(filterNeedle) || r.value.toLowerCase().includes(filterNeedle),
        ),
  );
  const visibleListRows = $derived(
    listRows
      .map((value, index) => ({ index, value }))
      .filter((r) => !filterNeedle || r.value.toLowerCase().includes(filterNeedle) || String(r.index) === filterNeedle),
  );
  const visibleSetRows = $derived(
    !filterNeedle ? setRows : setRows.filter((m) => m.toLowerCase().includes(filterNeedle)),
  );
  const visibleZSetRows = $derived(
    !filterNeedle
      ? zsetRows
      : zsetRows.filter(
          (m) => m.member.toLowerCase().includes(filterNeedle) || String(m.score).includes(filterNeedle),
        ),
  );
  const visibleStreamEntries = $derived(
    !filterNeedle
      ? streamEntries
      : streamEntries.filter(
          (e) => e.id.toLowerCase().includes(filterNeedle) || JSON.stringify(e.fields).toLowerCase().includes(filterNeedle),
        ),
  );

  const loadedCount = $derived.by(() => {
    switch (keyType) {
      case "hash": return hashRows.length;
      case "list": return listRows.length;
      case "set": return setRows.length;
      case "zset": return zsetRows.length;
      case "stream": return streamEntries.length;
      default: return 0;
    }
  });

  function flashNotice(msg: string) {
    statusNotice = msg;
    if (noticeTimer) clearTimeout(noticeTimer);
    noticeTimer = setTimeout(() => (statusNotice = ""), 1800);
  }

  async function loadPage(key: string, opts: { append?: boolean; match?: string } = {}) {
    const append = !!opts.append;
    const match = opts.match !== undefined ? opts.match : serverMatch;
    controller?.abort();
    const ac = new AbortController();
    controller = ac;
    if (append) loadingMore = true;
    else loading = true;
    error = "";

    try {
      const req: Parameters<typeof getRedisKey>[1] = {
        ...auth,
        key,
        limit: PAGE_SIZE,
      };
      if (match) req.match = match;
      if (append) {
        if (keyType === "list" || (keyType === "zset" && !match)) {
          req.offset = nextOffset;
        } else if (nextCursor) {
          req.cursor = nextCursor;
        }
      }

      const res = await getRedisKey(targetId, req, ac.signal);
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
      totalLength = res.length ?? 0;
      hasMore = !!res.hasMore;
      nextCursor = res.nextCursor ?? "";
      nextOffset = res.nextOffset ?? 0;

      if (!append) {
        ttlInput = res.ttlMs > 0 ? String(Math.ceil(res.ttlMs / 1000)) : "";
      }

      if (t === "string") {
        const pretty = tryPrettyJson(res.stringValue ?? "");
        stringValue = pretty.formatted;
      } else if (t === "hash") {
        const incoming = (res.hashEntries ?? []).map((e) => ({ ...e }));
        if (append) {
          const seen = new Set(hashRows.map((r) => r.field));
          hashRows = [...hashRows, ...incoming.filter((r) => !seen.has(r.field))];
        } else {
          hashRows = incoming;
        }
      } else if (t === "list") {
        const incoming = [...(res.listItems ?? [])];
        listRows = append ? [...listRows, ...incoming] : incoming;
      } else if (t === "set") {
        const incoming = [...(res.setMembers ?? [])];
        if (append) {
          const seen = new Set(setRows);
          setRows = [...setRows, ...incoming.filter((m) => !seen.has(m))];
        } else {
          setRows = incoming;
        }
      } else if (t === "zset") {
        const incoming = (res.zsetMembers ?? []).map((m) => ({ ...m }));
        if (append) {
          const seen = new Set(zsetRows.map((m) => m.member));
          zsetRows = [...zsetRows, ...incoming.filter((m) => !seen.has(m.member))];
        } else {
          zsetRows = incoming;
        }
      } else if (t === "stream") {
        const incoming = [...(res.streamEntries ?? [])];
        if (append) {
          const seen = new Set(streamEntries.map((e) => e.id));
          streamEntries = [...streamEntries, ...incoming.filter((e) => !seen.has(e.id))];
        } else {
          streamEntries = incoming;
        }
      }
    } catch (e) {
      if (ac.signal.aborted) return;
      error = e instanceof Error ? e.message : "Failed to inspect key";
    } finally {
      if (controller === ac) {
        loading = false;
        loadingMore = false;
        controller = null;
      }
    }
  }

  onMount(() => {
    if (initialKey !== null) {
      keyName = initialKey;
      void loadPage(initialKey);
    }
  });

  onDestroy(() => {
    controller?.abort();
    if (copyTimer) clearTimeout(copyTimer);
    if (noticeTimer) clearTimeout(noticeTimer);
  });

  async function runServerMatch() {
    if (!keyName || !supportsServerMatch) return;
    const q = rowFilter.trim();
    const pattern = q ? (q.includes("*") ? q : `*${q}*`) : "";
    serverMatch = pattern;
    await loadPage(keyName, { append: false, match: pattern });
  }

  async function clearServerMatch() {
    rowFilter = "";
    if (serverMatch) {
      serverMatch = "";
      await loadPage(keyName, { append: false, match: "" });
    }
  }

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

  function parseTTL(): number | null {
    const raw = ttlInput.trim();
    if (raw === "") return -1;
    const n = Number(raw);
    if (!Number.isFinite(n) || !Number.isInteger(n)) {
      throw new Error("TTL must be a whole number of seconds (or empty for no expiry)");
    }
    return n;
  }

  async function applyMutation(payload: Omit<RedisMutateKeyRequest, "key" | keyof RedisAuthOptions>, notice: string) {
    if (!keyName || busy) return false;
    busy = true;
    error = "";
    try {
      await mutateRedisKey(targetId, { ...auth, key: keyName, ...payload });
      notifyRedisMutation(targetId, auth.db ?? 0);
      onmutated();
      flashNotice(notice);
      return true;
    } catch (e) {
      error = e instanceof Error ? e.message : "Mutation failed";
      return false;
    } finally {
      busy = false;
    }
  }

  async function handleUpdateTTL() {
    try {
      const ttlSeconds = parseTTL();
      if (await applyMutation({ action: "ttl", ttlSeconds }, ttlSeconds && ttlSeconds > 0 ? `TTL set to ${ttlSeconds}s` : "Expiry cleared")) {
        if (detail) {
          detail = { ...detail, ttlMs: ttlSeconds && ttlSeconds > 0 ? ttlSeconds * 1000 : -1 };
        }
      }
    } catch (e) {
      error = e instanceof Error ? e.message : "Invalid TTL";
    }
  }

  // Incremental item-level handlers for existing collections
  async function handleAddHash() {
    const f = addField.trim();
    if (!f) return;
    if (await applyMutation({ action: "hset", field: f, value: addValue }, `Set field ${f}`)) {
      const idx = hashRows.findIndex((r) => r.field === f);
      if (idx >= 0) hashRows[idx] = { field: f, value: addValue };
      else {
        hashRows = [{ field: f, value: addValue }, ...hashRows];
        totalLength += 1;
      }
      addField = "";
      addValue = "";
    }
  }

  async function handleSaveHashRow(row: RedisHashEntry) {
    await applyMutation({ action: "hset", field: row.field, value: row.value }, `Saved ${row.field}`);
  }

  async function handleDeleteHashRow(field: string) {
    if (await applyMutation({ action: "hdel", field }, `Deleted ${field}`)) {
      hashRows = hashRows.filter((r) => r.field !== field);
      totalLength = Math.max(0, totalLength - 1);
    }
  }

  async function handlePushList() {
    if (addValue === "") return;
    const val = addValue;
    if (await applyMutation({ action: "rpush", value: val }, "Pushed item")) {
      if (!hasMore) listRows = [...listRows, val];
      totalLength += 1;
      addValue = "";
    }
  }

  async function handleSaveListRow(index: number, value: string) {
    await applyMutation({ action: "lset", index, value }, `Updated [${index}]`);
  }

  async function handleDeleteListRow(index: number, value: string) {
    if (await applyMutation({ action: "lrem", value }, `Removed [${index}]`)) {
      listRows = listRows.filter((_, i) => i !== index);
      totalLength = Math.max(0, totalLength - 1);
    }
  }

  async function handleAddSet() {
    const m = addValue.trim();
    if (!m) return;
    if (await applyMutation({ action: "sadd", member: m }, `Added ${m}`)) {
      if (!setRows.includes(m)) {
        setRows = [m, ...setRows];
        totalLength += 1;
      }
      addValue = "";
    }
  }

  async function handleDeleteSetMember(member: string) {
    if (await applyMutation({ action: "srem", member }, `Removed ${member}`)) {
      setRows = setRows.filter((m) => m !== member);
      totalLength = Math.max(0, totalLength - 1);
    }
  }

  async function handleAddZSet() {
    const m = addValue.trim();
    if (!m) return;
    const score = Number(addScore) || 0;
    if (await applyMutation({ action: "zadd", member: m, score }, `Set ${m} = ${score}`)) {
      const idx = zsetRows.findIndex((r) => r.member === m);
      if (idx >= 0) zsetRows[idx] = { member: m, score };
      else {
        zsetRows = [...zsetRows, { member: m, score }].sort((a, b) => a.score - b.score);
        totalLength += 1;
      }
      addValue = "";
    }
  }

  async function handleSaveZSetRow(row: RedisZSetMember) {
    await applyMutation({ action: "zadd", member: row.member, score: Number(row.score) || 0 }, `Updated ${row.member}`);
  }

  async function handleDeleteZSetMember(member: string) {
    if (await applyMutation({ action: "zrem", member }, `Removed ${member}`)) {
      zsetRows = zsetRows.filter((r) => r.member !== member);
      totalLength = Math.max(0, totalLength - 1);
    }
  }

  async function handleAppendStream() {
    const fields: Record<string, string> = {};
    for (const r of streamRows) {
      if (r.field.trim()) fields[r.field.trim()] = r.value;
    }
    if (Object.keys(fields).length === 0) {
      error = "Stream entry requires at least one field";
      return;
    }
    if (await applyMutation({ action: "xadd", streamId: streamId.trim() || "*", streamFields: fields }, "Appended entry")) {
      streamRows = [{ field: "", value: "" }];
      streamId = "*";
      await loadPage(keyName, { append: false });
    }
  }

  async function handleDeleteStreamEntry(id: string) {
    if (await applyMutation({ action: "xdel", streamId: id }, `Deleted ${id}`)) {
      streamEntries = streamEntries.filter((e) => e.id !== id);
      totalLength = Math.max(0, totalLength - 1);
    }
  }

  // Whole-key Save (used for Create mode and non-truncated String updates)
  async function handleSave() {
    const cleanKey = keyName.trim();
    if (!cleanKey) {
      error = "Key name is required";
      return;
    }
    if (isTruncatedString) {
      error = "Cannot overwrite a truncated string (>256 KiB). Update TTL or edit via CLI instead.";
      return;
    }
    busy = true;
    error = "";
    try {
      const ttlRaw = ttlInput.trim();
      const ttlSeconds = ttlRaw === "" ? (isCreate ? undefined : -1) : parseTTL();
      const payload: RedisSetKeyRequest = {
        ...auth,
        key: cleanKey,
        type: keyType,
        ttlSeconds,
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
          {#if isCollection}
            <span class="badge mono">{totalLength.toLocaleString("en-GB")} total</span>
          {/if}
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
      <button type="button" class="icon-btn" onclick={onclose} aria-label="Close dialog">
        <XIcon size={15} />
      </button>
    </div>
  </header>

  <div class="drawer-body">
    {#if loading}
      <p class="note">Loading key value...</p>
    {:else}
      {#if isTruncatedString}
        <div class="info-banner">
          Value exceeds the 256 KiB preview cap ({detail?.length.toLocaleString("en-GB")} bytes total). Full-replace save is disabled to prevent data loss — you can still update TTL, delete, or edit via CLI.
        </div>
      {/if}
      {#if detail?.isBinary}
        <div class="info-banner">
          Binary payload detected — shown as base64. Saving replaces it with UTF-8 text.
        </div>
      {/if}

      <div class="form-grid" class:inspect={!isCreate}>
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
        <div class="field">
          <span>TTL (seconds)</span>
          <div class="ttl-row">
            <input class="mono" bind:value={ttlInput} placeholder="No expiry" inputmode="numeric" aria-label="TTL in seconds" />
            {#if !isCreate}
              <button type="button" class="mini-btn" disabled={busy} onclick={handleUpdateTTL}>Set</button>
            {/if}
          </div>
        </div>
      </div>

      {#if keyType === "string"}
        <div class="editor-head">
          <span class="eyebrow">Value</span>
          {#if !isTruncatedString}
            <button type="button" class="mini-btn" onclick={formatStringJson}>
              <CodeIcon size={11} /> Format JSON
            </button>
          {/if}
        </div>
        <textarea
          class="code-area"
          bind:value={stringValue}
          readonly={isTruncatedString}
          rows="12"
          spellcheck="false"
          placeholder="String or JSON payload"
        ></textarea>
      {:else if isCreate}
        <!-- Create mode for collections -->
        {#if keyType === "hash"}
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
          <div class="editor-head">
            <span class="eyebrow">Initial Entry (XADD)</span>
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
      {:else}
        <!-- Inspect/edit mode for existing collections: filter + incremental mutations + Load more -->
        <div class="collection-toolbar">
          <form class="filter-bar" onsubmit={(e) => { e.preventDefault(); if (supportsServerMatch) void runServerMatch(); }}>
            <MagnifyingGlassIcon size={12} />
            <input
              class="mono"
              bind:value={rowFilter}
              placeholder={supportsServerMatch ? "Filter loaded rows (Enter to match on server)" : "Filter loaded rows..."}
              aria-label="Filter values"
            />
            {#if rowFilter || serverMatch}
              <button type="button" class="clear-btn" onclick={clearServerMatch} aria-label="Clear filter">&times;</button>
            {/if}
          </form>
          {#if supportsServerMatch && (hasMore || serverMatch)}
            <button type="button" class="mini-btn" disabled={loading || loadingMore} onclick={runServerMatch}>
              Match on server
            </button>
          {/if}
        </div>

        {#if keyType === "hash"}
          <form class="add-bar hash" onsubmit={(e) => { e.preventDefault(); void handleAddHash(); }}>
            <input class="mono" bind:value={addField} placeholder="New field" />
            <input class="mono" bind:value={addValue} placeholder="Value" />
            <button type="submit" class="mini-btn accent" disabled={busy || !addField.trim()}><PlusIcon size={11} /> HSET</button>
          </form>
          <div class="kv-list">
            {#each visibleHashRows as row (row.field)}
              <div class="kv-row-edit">
                <input class="mono" value={row.field} readonly />
                <input class="mono" bind:value={row.value} onkeydown={(e) => e.key === "Enter" && handleSaveHashRow(row)} />
                <button type="button" class="save-row" disabled={busy} onclick={() => handleSaveHashRow(row)} title="Save field (HSET)" aria-label="Save {row.field}">
                  <FloppyDiskIcon size={12} />
                </button>
                <button type="button" class="del-row" disabled={busy} onclick={() => handleDeleteHashRow(row.field)} title="Delete field (HDEL)" aria-label="Delete {row.field}">&times;</button>
              </div>
            {:else}
              <p class="note">No matching fields.</p>
            {/each}
          </div>
        {:else if keyType === "list"}
          <form class="add-bar single" onsubmit={(e) => { e.preventDefault(); void handlePushList(); }}>
            <input class="mono" bind:value={addValue} placeholder="Append new item (RPUSH)" />
            <button type="submit" class="mini-btn accent" disabled={busy || addValue === ""}><PlusIcon size={11} /> RPUSH</button>
          </form>
          <div class="kv-list">
            {#each visibleListRows as item (item.index)}
              <div class="single-row-edit">
                <span class="idx mono">{item.index}</span>
                <input class="mono" bind:value={listRows[item.index]} onkeydown={(e) => e.key === "Enter" && handleSaveListRow(item.index, listRows[item.index])} />
                <button type="button" class="save-row" disabled={busy} onclick={() => handleSaveListRow(item.index, listRows[item.index])} title="Update index (LSET)" aria-label="Update item {item.index}">
                  <FloppyDiskIcon size={12} />
                </button>
                <button type="button" class="del-row" disabled={busy} onclick={() => handleDeleteListRow(item.index, item.value)} title="Remove item (LREM)" aria-label="Remove item {item.index}">&times;</button>
              </div>
            {:else}
              <p class="note">No matching items.</p>
            {/each}
          </div>
        {:else if keyType === "set"}
          <form class="add-bar single" onsubmit={(e) => { e.preventDefault(); void handleAddSet(); }}>
            <input class="mono" bind:value={addValue} placeholder="Add member (SADD)" />
            <button type="submit" class="mini-btn accent" disabled={busy || !addValue.trim()}><PlusIcon size={11} /> SADD</button>
          </form>
          <div class="kv-list">
            {#each visibleSetRows as member (member)}
              <div class="single-row">
                <input class="mono" value={member} readonly />
                <button type="button" class="del-row" disabled={busy} onclick={() => handleDeleteSetMember(member)} title="Remove member (SREM)" aria-label="Remove {member}">&times;</button>
              </div>
            {:else}
              <p class="note">No matching members.</p>
            {/each}
          </div>
        {:else if keyType === "zset"}
          <form class="add-bar zset" onsubmit={(e) => { e.preventDefault(); void handleAddZSet(); }}>
            <input class="mono" type="number" step="any" bind:value={addScore} placeholder="Score" />
            <input class="mono" bind:value={addValue} placeholder="Member (ZADD)" />
            <button type="submit" class="mini-btn accent" disabled={busy || !addValue.trim()}><PlusIcon size={11} /> ZADD</button>
          </form>
          <div class="kv-list">
            {#each visibleZSetRows as row (row.member)}
              <div class="zset-row-edit">
                <input class="mono" type="number" step="any" bind:value={row.score} onkeydown={(e) => e.key === "Enter" && handleSaveZSetRow(row)} />
                <input class="mono" value={row.member} readonly />
                <button type="button" class="save-row" disabled={busy} onclick={() => handleSaveZSetRow(row)} title="Update score (ZADD)" aria-label="Update score for {row.member}">
                  <FloppyDiskIcon size={12} />
                </button>
                <button type="button" class="del-row" disabled={busy} onclick={() => handleDeleteZSetMember(row.member)} title="Remove member (ZREM)" aria-label="Remove {row.member}">&times;</button>
              </div>
            {:else}
              <p class="note">No matching members.</p>
            {/each}
          </div>
        {:else if keyType === "stream"}
          <div class="stream-list">
            {#each visibleStreamEntries as entry (entry.id)}
              <div class="stream-entry">
                <div class="stream-entry-head">
                  <span class="stream-id mono">{entry.id}</span>
                  <button type="button" class="del-row small" disabled={busy} onclick={() => handleDeleteStreamEntry(entry.id)} title="Delete entry (XDEL)" aria-label="Delete {entry.id}">&times;</button>
                </div>
                <code class="stream-fields">{JSON.stringify(entry.fields)}</code>
              </div>
            {:else}
              <p class="note">No matching stream entries.</p>
            {/each}
          </div>
          <div class="editor-head">
            <span class="eyebrow">Append Entry (XADD)</span>
            <button type="button" class="mini-btn" onclick={() => (streamRows = [...streamRows, { field: "", value: "" }])}>
              <PlusIcon size={11} /> Add field
            </button>
          </div>
          <div class="stream-add-head">
            <input class="mono" bind:value={streamId} placeholder="ID (*)" aria-label="Stream entry ID" />
            <button type="button" class="mini-btn accent" disabled={busy} onclick={handleAppendStream}><PlusIcon size={11} /> Append</button>
          </div>
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

        <div class="pager-bar">
          <span class="note">
            Loaded {loadedCount.toLocaleString("en-GB")} of {totalLength.toLocaleString("en-GB")}{serverMatch ? ` (match: ${serverMatch})` : ""}
          </span>
          {#if hasMore}
            <button type="button" class="mini-btn" disabled={loadingMore} onclick={() => loadPage(keyName, { append: true })}>
              {loadingMore ? "Loading..." : `Load ${PAGE_SIZE} more`}
            </button>
          {/if}
        </div>
      {/if}

      {#if statusNotice}
        <div class="notice-banner">{statusNotice}</div>
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
          <TrashIcon size={13} /> Delete key
        </button>
      {/if}
    {:else}
      <span></span>
    {/if}
    <div class="foot-right">
      <button type="button" class="btn ghost" disabled={busy} onclick={onclose}>Close</button>
      {#if isCreate || keyType === "string"}
        <button
          type="button"
          class="btn primary"
          disabled={busy || loading || isTruncatedString}
          onclick={handleSave}
          title={isTruncatedString ? "Disabled for truncated strings (>256 KiB)" : undefined}
        >
          <FloppyDiskIcon size={13} />
          {busy ? "Saving..." : isCreate ? "Create Key" : "Save String"}
        </button>
      {/if}
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
    flex: 1; overflow-y: auto; padding: 14px 16px;
    display: flex; flex-direction: column; gap: 12px;
  }
  .form-grid { display: grid; grid-template-columns: 1.7fr 0.75fr 1.15fr; gap: 10px; }
  @media (max-width: 640px) { .form-grid { grid-template-columns: 1fr; } }
  .field { display: flex; flex-direction: column; gap: 5px; }
  .field span { font-size: 10.5px; color: var(--text-tertiary); text-transform: uppercase; letter-spacing: 0.05em; }
  .ttl-row { display: flex; gap: 6px; }
  .ttl-row input { flex: 1; min-width: 0; }
  .field input, .field select, .kv-list input, .add-bar input, .stream-add-head input {
    height: 30px; padding: 0 9px; border-radius: 6px;
    border: 1px solid var(--border-subtle); background: var(--bg-element);
    color: var(--text-primary); font-size: 12px; outline: none;
  }
  .field input:focus, .field select:focus, .kv-list input:focus, .add-bar input:focus, .code-area:focus {
    border-color: var(--border-focus);
  }
  .field input[readonly], .kv-list input[readonly] { opacity: 0.78; }
  .editor-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  .eyebrow { font-size: 10px; letter-spacing: 0.08em; text-transform: uppercase; color: var(--text-tertiary); }
  .mini-btn {
    display: inline-flex; align-items: center; gap: 5px; height: 28px;
    padding: 0 9px; border-radius: 6px; border: 1px solid var(--border-subtle);
    background: var(--bg-element); color: var(--text-secondary); font-size: 11px; cursor: pointer; white-space: nowrap;
  }
  .mini-btn:hover:not(:disabled) { color: var(--text-primary); border-color: var(--border-default); }
  .mini-btn.accent { background: var(--text-primary); border-color: var(--text-primary); color: var(--bg-app); font-weight: 500; }
  .mini-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .code-area {
    width: 100%; min-height: 220px; padding: 10px 12px; border-radius: 8px;
    border: 1px solid var(--border-subtle); background: var(--bg-element);
    color: var(--text-primary); font: 12px/1.5 var(--font-mono, ui-monospace, monospace);
    resize: vertical; outline: none;
  }

  .collection-toolbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .filter-bar {
    flex: 1; min-width: 180px; display: flex; align-items: center; gap: 7px; height: 30px; padding: 0 9px;
    border-radius: 7px; border: 1px solid var(--border-subtle); background: var(--bg-element); color: var(--text-tertiary);
  }
  .filter-bar:focus-within { border-color: var(--border-focus); }
  .filter-bar input {
    flex: 1; min-width: 0; border: 0; outline: none; background: transparent;
    color: var(--text-primary); font-size: 12px;
  }
  .clear-btn { border: 0; background: transparent; color: var(--text-tertiary); cursor: pointer; font-size: 15px; line-height: 1; }
  .clear-btn:hover { color: var(--text-primary); }

  .add-bar { display: grid; gap: 6px; align-items: center; }
  .add-bar.hash { grid-template-columns: 1fr 1.3fr auto; }
  .add-bar.single { grid-template-columns: 1fr auto; }
  .add-bar.zset { grid-template-columns: 110px 1fr auto; }
  .stream-add-head { display: grid; grid-template-columns: 1fr auto; gap: 6px; align-items: center; }

  .kv-list { display: flex; flex-direction: column; gap: 6px; }
  .kv-row { display: grid; grid-template-columns: 1fr 1.4fr 26px; gap: 6px; }
  .kv-row-edit { display: grid; grid-template-columns: 1fr 1.4fr 26px 26px; gap: 6px; }
  .zset-row { display: grid; grid-template-columns: 110px 1fr 26px; gap: 6px; }
  .zset-row-edit { display: grid; grid-template-columns: 110px 1fr 26px 26px; gap: 6px; }
  .single-row { display: grid; grid-template-columns: 1fr 26px; gap: 6px; align-items: center; }
  .single-row.indexed { grid-template-columns: 28px 1fr 26px; }
  .single-row-edit { display: grid; grid-template-columns: 28px 1fr 26px 26px; gap: 6px; align-items: center; }
  .idx { font-size: 11px; color: var(--text-tertiary); min-width: 22px; text-align: right; }
  .save-row, .del-row {
    width: 26px; height: 30px; border-radius: 6px; border: 1px solid var(--border-subtle);
    background: transparent; color: var(--text-tertiary); cursor: pointer; font-size: 14px;
    display: inline-flex; align-items: center; justify-content: center;
  }
  .del-row.small { width: 22px; height: 22px; font-size: 13px; }
  .save-row:hover:not(:disabled) { color: var(--accent-green); border-color: var(--accent-green); }
  .del-row:hover:not(:disabled) { color: var(--accent-red); border-color: var(--accent-red); }

  .stream-list { display: flex; flex-direction: column; gap: 6px; max-height: 240px; overflow-y: auto; }
  .stream-entry {
    padding: 8px 10px; border-radius: 6px; border: 1px solid var(--border-subtle);
    background: var(--bg-element); display: flex; flex-direction: column; gap: 3px;
  }
  .stream-entry-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  .stream-id { font-size: 11px; color: var(--accent-amber); }
  .stream-fields { font-size: 11px; color: var(--text-secondary); word-break: break-all; }

  .pager-bar {
    display: flex; align-items: center; justify-content: space-between; gap: 10px;
    padding-top: 6px; border-top: 1px solid var(--border-subtle);
  }
  .info-banner {
    padding: 8px 10px; border-radius: 7px; font-size: 11.5px;
    border: 1px solid color-mix(in srgb, var(--accent-amber) 35%, transparent);
    background: color-mix(in srgb, var(--accent-amber) 10%, transparent);
    color: var(--text-primary);
  }
  .notice-banner {
    padding: 7px 10px; border-radius: 7px; font-size: 11.5px;
    border: 1px solid color-mix(in srgb, var(--accent-green) 35%, transparent);
    background: color-mix(in srgb, var(--accent-green) 10%, transparent);
    color: var(--accent-green);
  }
  .error-banner {
    display: flex; align-items: center; gap: 8px; padding: 9px 11px; border-radius: 7px;
    border: 1px solid color-mix(in srgb, var(--accent-red) 40%, transparent);
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
    color: var(--accent-red); font-size: 12px;
  }
  .note { font-size: 11.5px; color: var(--text-tertiary); margin: 0; }
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
