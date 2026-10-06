<script lang="ts">
  import { tick } from "svelte";
  import { CheckIcon, CopyIcon } from "phosphor-svelte";
  import RcStat from "$lib/components/rack/rc-stat.svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import { fetchCognitoPool } from "$lib/api";
  import type { CognitoPoolDetail, CognitoPoolSummary } from "$lib/types";
  import { timeAgo } from "$lib/utils";
  import CodesPanel from "./codes-panel.svelte";
  import UsersPanel from "./users-panel.svelte";
  import ClientsPanel from "./clients-panel.svelte";
  import TokensPanel from "./tokens-panel.svelte";
  import SettingsPanel from "./settings-panel.svelte";
  import { createCopier } from "./cognito-ui.svelte";

  let { summary, refreshKey }: { summary: CognitoPoolSummary; refreshKey: string | undefined } = $props();

  type View = "users" | "clients" | "groups" | "tokens" | "settings";
  const views: { id: View; label: string }[] = [
    { id: "users", label: "Users" },
    { id: "clients", label: "Clients" },
    { id: "groups", label: "Groups" },
    { id: "tokens", label: "Tokens" },
    { id: "settings", label: "Settings" },
  ];
  let view = $state<View>("users");
  let mintUser = $state("");

  let detail = $state<CognitoPoolDetail | null>(null);
  let error = $state("");
  const copier = createCopier();

  // Re-fetch on every overview poll, so a new sign-up's code shows up on its own.
  $effect(() => {
    void refreshKey;
    load();
  });

  let controller: AbortController | undefined;
  async function load() {
    controller?.abort();
    controller = new AbortController();
    try {
      detail = await fetchCognitoPool(summary.id, controller.signal);
      error = "";
    } catch (err) {
      if ((err as Error).name === "AbortError") return;
      error = err instanceof Error ? err.message : "Failed to load user pool";
    }
  }

  function openMint(username: string) {
    mintUser = username;
    select("tokens");
  }

  // Sliding pill for the view switcher.
  let tabEls = $state<HTMLButtonElement[]>([]);
  let pill = $state({ left: 0, width: 0 });
  let pillReady = $state(false);
  function select(v: View) {
    view = v;
  }
  $effect(() => {
    const i = views.findIndex((v) => v.id === view);
    const el = tabEls[i];
    if (!el) return;
    pill = { left: el.offsetLeft, width: el.offsetWidth };
    tick().then(() => (pillReady = true));
  });

  const users = $derived(detail?.userList ?? []);
  const clients = $derived(detail?.clientList ?? []);
  const groups = $derived(detail?.groupList ?? []);
  const codes = $derived(detail?.codes ?? []);
  const unconfirmed = $derived(users.filter((u) => u.status === "UNCONFIRMED").length);
</script>

<div class="detail">
  <header class="hero">
    <h1 title={summary.name}>{summary.name}</h1>
    <div class="subline">
      <span>User pool</span><i></i>
      <button type="button" class="copyable" title="Copy pool ID" onclick={() => copier.copy(summary.id, "id")}>
        <span>{summary.id}</span>
        {#if copier.copied === "id"}<CheckIcon size={11} />{:else}<CopyIcon size={11} />{/if}
      </button>
      <i></i>
      <button type="button" class="copyable" title="Copy issuer" onclick={() => copier.copy(summary.issuer, "iss")}>
        <span>{summary.issuer}</span>
        {#if copier.copied === "iss"}<CheckIcon size={11} />{:else}<CopyIcon size={11} />{/if}
      </button>
    </div>
  </header>

  <div class="stats">
    <RcStat label="Users" value={summary.users} sub={unconfirmed ? `${unconfirmed} unconfirmed` : "all confirmed"} tone={unconfirmed ? "amber" : undefined} />
    <RcStat label="Pending codes" value={summary.pendingCodes} sub="never sent" tone={summary.pendingCodes ? "amber" : undefined} />
    <RcStat label="Clients" value={summary.clients} sub={`${summary.groups} group${summary.groups === 1 ? "" : "s"}`} />
    <RcStat label="MFA" value={summary.mfaMode.toLowerCase()} sub={summary.triggers?.length ? `${summary.triggers.length} trigger${summary.triggers.length === 1 ? "" : "s"}` : "no triggers"} />
  </div>

  {#if error}
    <p class="note err">{error}</p>
  {:else if !detail}
    <div class="skeleton"><span></span><span></span></div>
  {:else}
    <CodesPanel {codes} {users} fixedCode={detail.tarn.fixedCode} index={0} />

    <div class="switcher" role="tablist" aria-label="Pool views">
      <span class="pill" class:ready={pillReady} style:transform="translateX({pill.left}px)" style:width="{pill.width}px"></span>
      {#each views as v, i (v.id)}
        <button
          type="button"
          role="tab"
          aria-selected={view === v.id}
          class:on={view === v.id}
          bind:this={tabEls[i]}
          onclick={() => select(v.id)}
        >
          {v.label}
          {#if v.id === "users"}<em>{detail.usersTotal}</em>{/if}
          {#if v.id === "clients"}<em>{clients.length}</em>{/if}
          {#if v.id === "groups"}<em>{groups.length}</em>{/if}
        </button>
      {/each}
    </div>

    {#if view === "users"}
      <UsersPanel poolId={summary.id} {users} total={detail.usersTotal} omitted={detail.usersOmitted} onChanged={load} onMint={openMint} index={1} />
    {:else if view === "clients"}
      <ClientsPanel poolId={summary.id} {clients} index={1} />
    {:else if view === "groups"}
      <RcPanel title="Groups" description="Ordered by name. Lower precedence wins when a user is in several." index={1}>
        {#if groups.length}
          <div class="groups">
            {#each groups as g (g.name)}
              <div class="group">
                <span class="gname">{g.name}</span>
                <span class="gdesc">{g.description || "No description"}</span>
                <span class="gmeta">{g.precedence ?? "--"}<b>precedence</b></span>
                <span class="gmeta">{g.members}<b>member{g.members === 1 ? "" : "s"}</b></span>
                <span class="gage">{timeAgo(g.created)}</span>
              </div>
            {/each}
          </div>
        {:else}
          <p class="note">No groups. Members' groups appear in the <code>cognito:groups</code> claim.</p>
        {/if}
      </RcPanel>
    {:else if view === "tokens"}
      <TokensPanel poolId={summary.id} {clients} {users} bind:username={mintUser} index={1} />
    {:else}
      <SettingsPanel pool={detail} index={1} />
    {/if}
  {/if}
</div>

<style>
  .detail { display: flex; flex-direction: column; gap: 14px; min-width: 0; }
  .hero { padding: 4px 2px 2px; min-width: 0; }
  h1 { font-size: 19px; font-weight: 600; letter-spacing: -0.02em; color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .subline { display: flex; align-items: center; gap: 8px; margin-top: 4px; font-size: 11.5px; color: var(--text-tertiary); min-width: 0; }
  .subline i { width: 3px; height: 3px; border-radius: 1px; background: var(--border-default); flex-shrink: 0; }
  .copyable {
    display: inline-flex; align-items: center; gap: 6px; min-width: 0; padding: 1px 4px; margin-left: -4px; border-radius: 6px;
    font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); transition: color 120ms ease, background 120ms ease;
  }
  .copyable span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .copyable:hover { color: var(--text-primary); background: var(--bg-element-hover); }

  .stats {
    display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; padding: 14px 2px 10px;
    animation: statsIn 320ms var(--ease-snappy) both;
  }
  @keyframes statsIn { from { opacity: 0; transform: translateY(4px); } }
  @media (max-width: 1100px) { .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); } }

  .switcher {
    position: relative; display: inline-flex; align-self: flex-start; gap: 2px; padding: 3px; border-radius: 8px;
    border: 1px solid var(--border-subtle);
  }
  .switcher button {
    position: relative; z-index: 1; display: inline-flex; align-items: center; gap: 6px; height: 26px; padding: 0 11px;
    border-radius: 6px; font-size: 12px; color: var(--text-tertiary); transition: color 120ms ease;
  }
  .switcher button:hover { color: var(--text-primary); }
  .switcher button.on { color: var(--text-primary); }
  .switcher em { font-style: normal; font-size: 10.5px; color: var(--text-tertiary); font-variant-numeric: tabular-nums; }
  .pill {
    position: absolute; top: 3px; left: 0; height: 26px; border-radius: 6px;
    background: var(--bg-element); border: 1px solid var(--border-default);
  }
  .pill.ready { transition: transform 240ms var(--ease-snappy), width 240ms var(--ease-snappy); }

  .groups { display: flex; flex-direction: column; gap: 2px; }
  .group {
    display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.5fr) 6.5rem 6rem 4.5rem; align-items: center; gap: 12px;
    padding: 8px 10px; border-radius: 8px; transition: background 120ms ease;
  }
  .group:hover { background: var(--bg-element-hover); }
  .gname { font: 12px var(--font-mono, ui-monospace, monospace); color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; }
  .gdesc { font-size: 11.5px; color: var(--text-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .gmeta { font: 12px var(--font-mono, ui-monospace, monospace); color: var(--text-primary); }
  .gmeta b { margin-left: 5px; font: 10.5px var(--font-sans, inherit); color: var(--text-tertiary); font-weight: 400; }
  .gage { font-size: 10.5px; color: var(--text-tertiary); text-align: right; }

  .note { padding: 14px 12px; border-radius: 8px; background: var(--bg-app); font-size: 11.5px; color: var(--text-tertiary); }
  .note code { font-family: var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); }
  .note.err { color: var(--accent-red); }
  .skeleton { display: flex; flex-direction: column; gap: 10px; }
  .skeleton span { height: 120px; border-radius: 12px; background: var(--bg-element); animation: pulse 1.4s ease-in-out infinite; }
  @keyframes pulse { 50% { opacity: 0.5; } }
  @media (prefers-reduced-motion: reduce) { .stats, .skeleton span { animation: none; } .pill.ready { transition: none; } }
</style>
