<script lang="ts">
  import { CaretRightIcon, MagnifyingGlassIcon } from "phosphor-svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcTonePill from "$lib/components/rack/rc-tone-pill.svelte";
  import RcButton from "$lib/components/rack/rc-button.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import { cognitoUserAction } from "$lib/api";
  import type { CognitoUser, CognitoUserAction } from "$lib/types";
  import { formatDate, timeAgo } from "$lib/utils";
  import { displayName, statusLabel, statusTone } from "./cognito-ui.svelte";

  let {
    poolId,
    users,
    total,
    omitted = 0,
    index = 0,
    onChanged,
    onMint,
  }: {
    poolId: string;
    users: CognitoUser[];
    total: number;
    omitted?: number;
    index?: number;
    onChanged: () => void;
    onMint: (username: string) => void;
  } = $props();

  let query = $state("");
  let expanded = $state<string | null>(null);
  let busy = $state<string | null>(null);
  let error = $state("");
  // Destructive actions take a second click on the same button.
  let armed = $state<string | null>(null);
  let armTimer: ReturnType<typeof setTimeout> | undefined;
  let passwordFor = $state<string | null>(null);
  let password = $state("");
  let permanent = $state(true);

  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return users;
    return users.filter(
      (u) =>
        u.username.toLowerCase().includes(q) ||
        (u.email ?? "").toLowerCase().includes(q) ||
        (u.phone ?? "").includes(q) ||
        u.sub.includes(q) ||
        u.status.toLowerCase().includes(q),
    );
  });

  function arm(id: string) {
    armed = id;
    clearTimeout(armTimer);
    armTimer = setTimeout(() => (armed = null), 3000);
  }

  async function run(u: CognitoUser, action: CognitoUserAction, destructive = false) {
    const id = `${u.username}:${action}`;
    if (destructive && armed !== id) {
      arm(id);
      return;
    }
    armed = null;
    busy = id;
    error = "";
    try {
      await cognitoUserAction(poolId, u.username, action);
      onChanged();
    } catch (err) {
      error = err instanceof Error ? err.message : `${action} failed`;
    } finally {
      busy = null;
    }
  }

  async function setPassword(u: CognitoUser) {
    busy = `${u.username}:set-password`;
    error = "";
    try {
      await cognitoUserAction(poolId, u.username, "set-password", { password, permanent });
      passwordFor = null;
      password = "";
      onChanged();
    } catch (err) {
      error = err instanceof Error ? err.message : "Set password failed";
    } finally {
      busy = null;
    }
  }

  function attributes(u: CognitoUser) {
    return Object.entries(u.attributes)
      .sort(([a], [b]) => (a === "sub" ? -1 : b === "sub" ? 1 : a.localeCompare(b)))
      .map(([label, value]) => ({ label, value, mono: true }));
  }
</script>

<RcPanel
  title="Users"
  description={omitted > 0 ? `Newest ${users.length} of ${total} users` : `${total} user${total === 1 ? "" : "s"}, newest first`}
  {index}
>
  {#snippet actions()}
    <label class="search">
      <MagnifyingGlassIcon size={12} />
      <input placeholder="Filter users" bind:value={query} aria-label="Filter users" />
    </label>
  {/snippet}

  {#if error}<p class="note err">{error}</p>{/if}

  {#if users.length === 0}
    <p class="note">No users yet. Sign up through your app, or run <code>aws cognito-idp admin-create-user</code>.</p>
  {:else}
    <div class="rows">
      {#each visible as u (u.username)}
        {@const open = expanded === u.username}
        <div class="user" class:open>
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="row" onclick={() => (expanded = open ? null : u.username)}>
            <span class="caret"><CaretRightIcon size={10} /></span>
            <span class="who">
              <span class="name" title={u.username}>{displayName(u)}</span>
              <span class="sub">
                {#if displayName(u) !== u.username}
                  {u.username}
                {:else if u.email}
                  {u.email}
                {:else}
                  {u.sub}
                {/if}
                {#if u.email && displayName(u) === u.email}{#if u.emailVerified}<em>verified</em>{:else}<em class="no">unverified</em>{/if}{:else if u.email && u.emailVerified}<em>verified</em>{/if}
                {#if u.phone && displayName(u) !== u.phone}<b>·</b>{u.phone}{#if u.phoneVerified}<em>verified</em>{/if}{/if}
              </span>
            </span>
            <span class="tags">
              {#each u.groups ?? [] as g (g)}<span class="chip">{g}</span>{/each}
              {#each u.mfa ?? [] as m (m)}<span class="chip mfa" class:preferred={m === u.preferredMfa}>{m === "SMS_MFA" ? "SMS MFA" : "Email MFA"}</span>{/each}
            </span>
            <span class="state">
              {#if !u.enabled}<RcTonePill tone="red">disabled</RcTonePill>{/if}
              <RcTonePill tone={statusTone(u.status)}>{statusLabel(u.status)}</RcTonePill>
            </span>
            <span class="age">
              <span class="age-text">{timeAgo(u.created)}</span>
              {#if u.status === "UNCONFIRMED"}
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <span class="quick" onclick={(e) => e.stopPropagation()}>
                  <RcButton small variant="primary" disabled={!!busy} onclick={() => run(u, "confirm")}>Confirm</RcButton>
                </span>
              {/if}
            </span>
          </div>

          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="actions" onclick={(e) => e.stopPropagation()}>
            {#if u.status === "UNCONFIRMED"}
              <RcButton small variant="primary" disabled={!!busy} onclick={() => run(u, "confirm")}>Confirm</RcButton>
            {/if}
            <RcButton small disabled={!!busy || !u.enabled} onclick={() => onMint(u.username)} title="Issue tokens without the password">Mint tokens</RcButton>
            <RcButton small disabled={!!busy} onclick={() => { passwordFor = passwordFor === u.username ? null : u.username; password = ""; }}>Set password</RcButton>
            <RcButton small disabled={!!busy} onclick={() => run(u, "reset-password")} title="Move to RESET_REQUIRED and issue a reset code">Reset</RcButton>
            <RcButton small disabled={!!busy} onclick={() => run(u, u.enabled ? "disable" : "enable")}>{u.enabled ? "Disable" : "Enable"}</RcButton>
            <RcButton small variant={armed === `${u.username}:sign-out` ? "danger" : "default"} disabled={!!busy || u.sessions === 0} onclick={() => run(u, "sign-out", true)}>
              {armed === `${u.username}:sign-out` ? "Confirm sign out" : `Sign out${u.sessions ? ` (${u.sessions})` : ""}`}
            </RcButton>
            <RcButton small variant={armed === `${u.username}:delete` ? "danger" : "ghost"} disabled={!!busy} onclick={() => run(u, "delete", true)}>
              {armed === `${u.username}:delete` ? "Confirm delete" : "Delete"}
            </RcButton>
          </div>

          {#if passwordFor === u.username}
            <form class="pw" onsubmit={(e) => { e.preventDefault(); setPassword(u); }}>
              <input type="text" placeholder="New password" bind:value={password} autocomplete="off" aria-label="New password" />
              <label class="check"><input type="checkbox" bind:checked={permanent} /> Permanent</label>
              <RcButton small type="submit" variant="primary" disabled={!password || !!busy}>Save</RcButton>
              <span class="hint">{permanent ? "User becomes CONFIRMED" : "User must change it at next sign-in"}</span>
            </form>
          {/if}

          {#if open}
            <div class="attrs">
              <RcKv labelWidth="11rem" items={[
                ...attributes(u),
                { label: "created", value: formatDate(u.created) },
                { label: "modified", value: formatDate(u.modified) },
                { label: "active sessions", value: String(u.sessions) },
              ]} />
            </div>
          {/if}
        </div>
      {:else}
        <p class="note">No user matches “{query}”.</p>
      {/each}
    </div>
  {/if}
</RcPanel>

<style>
  .search {
    display: flex; align-items: center; gap: 7px; height: 28px; width: 200px; padding: 0 10px; border-radius: 8px;
    border: 1px solid var(--border-subtle); color: var(--text-tertiary); transition: border-color 120ms ease;
  }
  .search:hover { border-color: var(--border-default); }
  .search:focus-within { border-color: var(--border-focus); }
  .search input { flex: 1; min-width: 0; background: transparent; border: 0; outline: none; font-size: 12px; color: var(--text-primary); }

  .rows { display: flex; flex-direction: column; gap: 2px; }
  .user { position: relative; border-radius: 8px; border: 1px solid transparent; transition: background 120ms ease, border-color 120ms ease; }
  .user:hover { background: var(--bg-element-hover); }
  .user.open { background: var(--bg-element); border-color: var(--border-default); }
  .row {
    display: grid; grid-template-columns: 12px minmax(0, 1.4fr) minmax(0, 1fr) auto 4.5rem; align-items: center; gap: 12px;
    padding: 8px 10px; cursor: pointer;
  }
  .caret { display: flex; color: var(--text-tertiary); transition: transform 200ms var(--ease-snappy); }
  .open .caret { transform: rotate(90deg); }
  .who { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
  .name {
    font: 12px var(--font-mono, ui-monospace, monospace); color: var(--text-primary);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .sub { font-size: 10.5px; color: var(--text-tertiary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sub em { font-style: normal; margin-left: 5px; color: var(--accent-green); }
  .sub em.no { color: var(--text-tertiary); }
  .sub b { font-weight: 400; margin: 0 5px; }
  .tags { display: flex; flex-wrap: wrap; gap: 4px; min-width: 0; }
  .chip {
    height: 20px; padding: 0 7px; border-radius: 6px; border: 1px solid var(--border-subtle);
    font-size: 10.5px; line-height: 18px; color: var(--text-secondary); white-space: nowrap;
  }
  .chip.mfa { color: var(--text-tertiary); }
  .chip.preferred { color: var(--text-primary); border-color: var(--border-default); }
  .state { display: flex; gap: 4px; justify-content: flex-end; }
  .age { display: flex; justify-content: flex-end; font-size: 10.5px; color: var(--text-tertiary); white-space: nowrap; }

  /* The full action set lives in the open row so hovering never shifts the list. */
  .actions { display: none; flex-wrap: wrap; gap: 4px; padding: 0 10px 8px 34px; }
  .user.open .actions { display: flex; animation: actionsIn 160ms var(--ease-snappy) both; }
  .quick { display: none; }
  .user:hover:not(.open) .quick { display: inline-flex; }
  .user:hover:not(.open) .age-text { display: none; }
  @keyframes actionsIn { from { opacity: 0; transform: translateY(-2px); } }

  .pw { display: flex; align-items: center; gap: 8px; padding: 0 10px 10px 34px; flex-wrap: wrap; }
  .pw input[type="text"] {
    height: 26px; width: 220px; padding: 0 9px; border-radius: 8px; border: 1px solid var(--border-subtle);
    background: var(--bg-app); font: 12px var(--font-mono, ui-monospace, monospace); color: var(--text-primary); outline: none;
  }
  .pw input[type="text"]:focus { border-color: var(--border-focus); }
  .check { display: flex; align-items: center; gap: 5px; font-size: 11.5px; color: var(--text-secondary); }
  .hint { font-size: 10.5px; color: var(--text-tertiary); }

  .attrs { padding: 4px 12px 12px 34px; }
  .note { padding: 14px 12px; border-radius: 8px; background: var(--bg-app); font-size: 11.5px; color: var(--text-tertiary); }
  .note code { font-family: var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); }
  .note.err { margin-bottom: 8px; color: var(--accent-red); }
  @media (max-width: 1100px) { .row { grid-template-columns: 12px minmax(0, 1fr) auto; } .tags, .age { display: none; } }
  @media (prefers-reduced-motion: reduce) { .caret, .actions { transition: none; animation: none; } }
</style>
