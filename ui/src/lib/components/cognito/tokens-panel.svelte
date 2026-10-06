<script lang="ts">
  import { CheckIcon, CopyIcon, MagnifyingGlassIcon } from "phosphor-svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcButton from "$lib/components/rack/rc-button.svelte";
  import RcTonePill from "$lib/components/rack/rc-tone-pill.svelte";
  import { decodeCognitoToken, mintCognitoTokens } from "$lib/api";
  import { formatJSONForViewer } from "$lib/json-format";
  import type { CognitoClient, CognitoDecodedToken, CognitoTokens, CognitoUser } from "$lib/types";
  import { formatDate } from "$lib/utils";
  import { createCopier, displayName, expiresIn } from "./cognito-ui.svelte";

  let {
    poolId,
    clients,
    users,
    username = $bindable(""),
    index = 0,
  }: {
    poolId: string;
    clients: CognitoClient[];
    users: CognitoUser[];
    username?: string;
    index?: number;
  } = $props();

  const copier = createCopier();
  let clientId = $state("");
  let minting = $state(false);
  let mintError = $state("");
  let tokens = $state<CognitoTokens | null>(null);

  let raw = $state("");
  let decoded = $state<CognitoDecodedToken | null>(null);
  let decodeError = $state("");
  let decodeTimer: ReturnType<typeof setTimeout> | undefined;

  // Keep the selections valid as polling refreshes the lists.
  $effect(() => {
    if (!clients.some((c) => c.clientId === clientId)) clientId = clients[0]?.clientId ?? "";
    if (!users.some((u) => u.username === username)) username = users[0]?.username ?? "";
  });

  async function mint() {
    minting = true;
    mintError = "";
    try {
      tokens = await mintCognitoTokens(poolId, clientId, username);
      decode(tokens.IdToken);
    } catch (err) {
      mintError = err instanceof Error ? err.message : "Failed to mint tokens";
      tokens = null;
    } finally {
      minting = false;
    }
  }

  function decode(token: string) {
    raw = token;
    clearTimeout(decodeTimer);
    decodeTimer = setTimeout(run, 150);
  }

  async function run() {
    const t = raw.trim();
    decodeError = "";
    if (!t) {
      decoded = null;
      return;
    }
    try {
      decoded = await decodeCognitoToken(t);
    } catch (err) {
      decodeError = err instanceof Error ? err.message : "Failed to decode";
    }
  }

  const claimsJson = $derived(decoded?.claims ? formatJSONForViewer(JSON.stringify(decoded.claims)) : null);
  const headerJson = $derived(decoded?.header ? formatJSONForViewer(JSON.stringify(decoded.header)) : null);
  const tokenUse = $derived(decoded?.claims?.token_use as string | undefined);

  const curl = $derived(tokens ? `curl -H "Authorization: Bearer ${tokens.AccessToken}" "$API_URL"` : "");
  const minted = $derived(
    tokens
      ? [
          { key: "access", label: "Access token", value: tokens.AccessToken },
          { key: "id", label: "ID token", value: tokens.IdToken },
          ...(tokens.RefreshToken ? [{ key: "refresh", label: "Refresh token", value: tokens.RefreshToken }] : []),
        ]
      : [],
  );
</script>

<RcPanel
  title="Mint tokens"
  description="Tarn-only: issue real tokens for a user without their password, for curl and API testing. Pre token generation still runs."
  {index}
>
  {#if clients.length === 0 || users.length === 0}
    <p class="note">Minting needs at least one app client and one user.</p>
  {:else}
    <form class="mint" onsubmit={(e) => { e.preventDefault(); mint(); }}>
      <label>
        <span>Client</span>
        <select bind:value={clientId}>
          {#each clients as c (c.clientId)}<option value={c.clientId}>{c.name} · {c.clientId}</option>{/each}
        </select>
      </label>
      <label>
        <span>User</span>
        <select bind:value={username}>
          {#each users as u (u.username)}<option value={u.username} disabled={!u.enabled}>{displayName(u)}{displayName(u) !== u.username ? "" : u.email ? ` · ${u.email}` : ""}</option>{/each}
        </select>
      </label>
      <RcButton type="submit" variant="primary" disabled={minting || !clientId || !username}>{minting ? "Minting…" : "Mint"}</RcButton>
    </form>
    {#if mintError}<p class="note err">{mintError}</p>{/if}

    {#if tokens}
      <div class="minted">
        {#each minted as t (t.key)}
          <div class="tok">
            <span class="k">{t.label}</span>
            <span class="v" title={t.value}>{t.value}</span>
            <RcButton small variant="ghost" onclick={() => copier.copy(t.value, t.key)}>
              {#if copier.copied === t.key}<CheckIcon size={11} />{:else}<CopyIcon size={11} />{/if}
            </RcButton>
            {#if t.key !== "refresh"}
              <RcButton small variant="ghost" onclick={() => decode(t.value)} title="Show in decoder"><MagnifyingGlassIcon size={11} /></RcButton>
            {/if}
          </div>
        {/each}
        <button type="button" class="curl" onclick={() => copier.copy(curl, "curl")} title="Copy curl command">
          <span>{curl}</span>
          {#if copier.copied === "curl"}<CheckIcon size={11} />{:else}<CopyIcon size={11} />{/if}
        </button>
        <p class="hint">Expires in {Math.round(tokens.ExpiresIn / 60)} minutes.</p>
      </div>
    {/if}
  {/if}
</RcPanel>

<RcPanel title="Decode a token" description="Paste any JWT. Tarn checks it against this account's pool keys." index={index + 1}>
  <textarea
    placeholder="eyJraWQiOi…"
    spellcheck="false"
    value={raw}
    oninput={(e) => decode((e.currentTarget as HTMLTextAreaElement).value)}
    aria-label="JWT to decode"
  ></textarea>

  {#if decodeError}
    <p class="note err">{decodeError}</p>
  {:else if decoded?.error}
    <p class="note err">{decoded.error}</p>
  {:else if decoded}
    <div class="verdict">
      {#if !decoded.knownPool}
        <RcTonePill tone="amber">not from a Tarn pool in this account</RcTonePill>
      {:else if decoded.signatureValid}
        <RcTonePill tone="green">signature verified</RcTonePill>
      {:else}
        <RcTonePill tone="red">signature invalid</RcTonePill>
      {/if}
      {#if decoded.expiresAt}
        <RcTonePill tone={decoded.expired ? "red" : "neutral"}>
          {decoded.expired ? `expired ${formatDate(decoded.expiresAt)}` : `expires ${expiresIn(decoded.expiresAt)}`}
        </RcTonePill>
      {/if}
      {#if tokenUse}<RcTonePill>{tokenUse} token</RcTonePill>{/if}
      {#if decoded.poolId}<span class="pool">{decoded.poolId}</span>{/if}
    </div>
    <div class="json">
      <div>
        <span class="k">Header</span>
        {#if headerJson}<pre>{@html headerJson.formattedHtml}</pre>{/if}
      </div>
      <div>
        <span class="k">Claims</span>
        {#if claimsJson}<pre>{@html claimsJson.formattedHtml}</pre>{/if}
      </div>
    </div>
  {/if}
</RcPanel>

<style>
  .mint { display: flex; align-items: flex-end; gap: 10px; flex-wrap: wrap; }
  .mint label { display: flex; flex-direction: column; gap: 4px; min-width: 0; flex: 1 1 220px; }
  .mint label span, .k { font-size: 10.5px; letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-tertiary); }
  select {
    height: 28px; padding: 0 8px; border-radius: 8px; border: 1px solid var(--border-subtle); background: var(--bg-app);
    font-size: 12px; color: var(--text-primary); outline: none; transition: border-color 120ms ease;
  }
  select:hover { border-color: var(--border-default); }
  select:focus { border-color: var(--border-focus); }

  .minted { display: flex; flex-direction: column; gap: 4px; margin-top: 14px; }
  .tok {
    display: grid; grid-template-columns: 7rem minmax(0, 1fr) auto auto; align-items: center; gap: 6px;
    min-height: 30px; padding: 0 4px 0 10px; border-radius: 8px; background: var(--bg-app);
  }
  .v { font: 11.5px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .curl {
    display: flex; align-items: center; gap: 8px; margin-top: 4px; padding: 7px 10px; border-radius: 8px;
    border: 1px dashed var(--border-subtle); text-align: left; color: var(--text-tertiary); transition: border-color 120ms ease, color 120ms ease;
  }
  .curl:hover { border-color: var(--border-default); color: var(--text-primary); }
  .curl span { flex: 1; min-width: 0; font: 11px var(--font-mono, ui-monospace, monospace); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .hint { margin-top: 2px; font-size: 10.5px; color: var(--text-tertiary); }

  textarea {
    width: 100%; min-height: 76px; resize: vertical; padding: 9px 11px; border-radius: 8px;
    border: 1px solid var(--border-subtle); background: var(--bg-app); outline: none;
    font: 11.5px/1.5 var(--font-mono, ui-monospace, monospace); color: var(--text-primary); word-break: break-all;
    transition: border-color 120ms ease;
  }
  textarea:focus { border-color: var(--border-focus); }
  .verdict { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin: 10px 0; }
  .pool { font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .json { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 2fr); gap: 12px; }
  .json > div { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  pre {
    max-height: 420px; overflow: auto; padding: 10px 12px; border-radius: 8px; background: var(--bg-app);
    font: 11.5px/1.6 var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); white-space: pre-wrap; word-break: break-word;
  }
  .note { padding: 14px 12px; border-radius: 8px; background: var(--bg-app); font-size: 11.5px; color: var(--text-tertiary); }
  .note.err { margin-top: 8px; color: var(--accent-red); }
  @media (max-width: 1100px) { .json { grid-template-columns: minmax(0, 1fr); } }
</style>
