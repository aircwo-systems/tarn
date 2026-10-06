<script lang="ts">
  import { CheckIcon, CopyIcon, EyeIcon, EyeSlashIcon } from "phosphor-svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import RcButton from "$lib/components/rack/rc-button.svelte";
  import { fetchCognitoClientSecret } from "$lib/api";
  import type { CognitoClient } from "$lib/types";
  import { createCopier } from "./cognito-ui.svelte";

  let { poolId, clients, index = 0 }: { poolId: string; clients: CognitoClient[]; index?: number } = $props();

  const copier = createCopier();
  let secrets = $state<Record<string, string>>({});
  let shown = $state<Record<string, boolean>>({});
  let error = $state("");

  async function toggleSecret(c: CognitoClient) {
    if (shown[c.clientId]) {
      shown[c.clientId] = false;
      return;
    }
    if (!secrets[c.clientId]) {
      try {
        secrets[c.clientId] = (await fetchCognitoClientSecret(poolId, c.clientId)).clientSecret;
      } catch (err) {
        error = err instanceof Error ? err.message : "Failed to load client secret";
        return;
      }
    }
    shown[c.clientId] = true;
  }

  const flowLabel = (f: string) => f.replace(/^ALLOW_/, "").replace(/_/g, " ").toLowerCase();
</script>

{#if error}<p class="note err">{error}</p>{/if}

{#each clients as c, i (c.clientId)}
  <RcPanel title={c.name} description={c.hasSecret ? "Confidential client (has a secret)" : "Public client (no secret)"} index={index + i}>
    <div class="ids">
      <button type="button" class="id" title="Copy client ID" onclick={() => copier.copy(c.clientId, `id-${c.clientId}`)}>
        <span class="k">Client ID</span>
        <span class="v">{c.clientId}</span>
        {#if copier.copied === `id-${c.clientId}`}<CheckIcon size={11} />{:else}<CopyIcon size={11} />{/if}
      </button>
      {#if c.hasSecret}
        <div class="id secret">
          <span class="k">Secret</span>
          <span class="v">{shown[c.clientId] ? secrets[c.clientId] : "••••••••••••••••••••"}</span>
          {#if shown[c.clientId]}
            <RcButton small variant="ghost" onclick={() => copier.copy(secrets[c.clientId], `sec-${c.clientId}`)}>
              {#if copier.copied === `sec-${c.clientId}`}<CheckIcon size={11} />{:else}<CopyIcon size={11} />{/if}
            </RcButton>
          {/if}
          <RcButton small variant="ghost" onclick={() => toggleSecret(c)}>
            {#if shown[c.clientId]}<EyeSlashIcon size={11} />Hide{:else}<EyeIcon size={11} />Reveal{/if}
          </RcButton>
        </div>
      {/if}
    </div>

    <div class="flows">
      {#each c.authFlows as f (f)}<span class="chip">{flowLabel(f)}</span>{/each}
      {#each c.oauthFlows ?? [] as f (f)}<span class="chip oauth">oauth {f}</span>{/each}
    </div>

    <RcKv labelWidth="10rem" items={[
      { label: "Access token", value: c.accessTokenValidity },
      { label: "ID token", value: c.idTokenValidity },
      { label: "Refresh token", value: c.refreshTokenValidity },
      { label: "User existence errors", value: c.preventUserExistenceErrors === "ENABLED" ? "Hidden (ENABLED)" : "Shown (LEGACY)" },
      { label: "Token revocation", value: c.tokenRevocation ? "Enabled" : "Disabled" },
      { label: "Read attributes", value: c.readAttributes?.join(", ") || "All", dim: !c.readAttributes?.length },
      { label: "Write attributes", value: c.writeAttributes?.join(", ") || "All", dim: !c.writeAttributes?.length },
      ...(c.oauthScopes?.length ? [{ label: "OAuth scopes", value: c.oauthScopes.join(" "), mono: true }] : []),
    ]} />
  </RcPanel>
{:else}
  <RcPanel title="App clients" {index}>
    <p class="note">No app clients. Create one with <code>aws cognito-idp create-user-pool-client</code> or Terraform.</p>
  </RcPanel>
{/each}

<style>
  .ids { display: flex; flex-direction: column; gap: 4px; margin-bottom: 12px; }
  .id {
    display: grid; grid-template-columns: 5rem minmax(0, 1fr) auto auto; align-items: center; gap: 8px;
    min-height: 30px; padding: 0 10px; border-radius: 8px; background: var(--bg-app); text-align: left;
    color: var(--text-tertiary); transition: color 120ms ease;
  }
  button.id:hover { color: var(--text-primary); }
  .k { font-size: 10.5px; letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-tertiary); }
  .v { font: 12px var(--font-mono, ui-monospace, monospace); color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .flows { display: flex; flex-wrap: wrap; gap: 4px; margin-bottom: 12px; }
  .chip {
    height: 22px; padding: 0 8px; border-radius: 6px; border: 1px solid var(--border-subtle);
    font-size: 11px; line-height: 20px; color: var(--text-secondary); white-space: nowrap;
  }
  .chip.oauth { color: var(--text-tertiary); }
  .note { padding: 14px 12px; border-radius: 8px; background: var(--bg-app); font-size: 11.5px; color: var(--text-tertiary); }
  .note code { font-family: var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); }
  .note.err { color: var(--accent-red); margin-bottom: 8px; }
</style>
