<script lang="ts">
  import { CheckIcon, CopyIcon } from "phosphor-svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import type { CognitoPendingCode, CognitoUser } from "$lib/types";
  import { timeAgo } from "$lib/utils";
  import { createCopier, displayName, expiresIn, purposeLabel } from "./cognito-ui.svelte";

  let {
    codes,
    users = [],
    fixedCode = false,
    index = 0,
  }: { codes: CognitoPendingCode[]; users?: CognitoUser[]; fixedCode?: boolean; index?: number } = $props();

  const names = $derived(new Map(users.map((u) => [u.username, displayName(u)])));

  const copier = createCopier();
  const key = (c: CognitoPendingCode) => `${c.username}:${c.purpose}:${c.created}`;
</script>

<RcPanel
  title="Pending codes"
  description={fixedCode
    ? "TARN_COGNITO_FIXED_CODE is set, so every code is the fixed value"
    : "Codes Cognito would email or text. Nothing is sent; click a code to copy it."}
  {index}
>
  {#if codes.length === 0}
    <p class="note">No codes waiting. Sign-up, password reset, verification and MFA codes appear here as soon as they're issued.</p>
  {:else}
    <div class="codes">
      {#each codes as c (key(c))}
        <button type="button" class="code-row" onclick={() => copier.copy(c.code, key(c))} title="Copy code">
          <span class="who">
            <span class="user" title={c.username}>{names.get(c.username) ?? c.username}</span>
            <span class="purpose">{purposeLabel(c.purpose, c.attribute)}{c.attribute ? ` · ${c.attribute}` : ""}</span>
          </span>
          <span class="code" class:password={c.purpose === "invite"}>{c.code}</span>
          <span class="when">
            <span>{timeAgo(c.created)}</span>
            <span class="exp">expires {expiresIn(c.expires)}</span>
          </span>
          <span class="copy">
            {#if copier.copied === key(c)}<CheckIcon size={12} />{:else}<CopyIcon size={12} />{/if}
          </span>
        </button>
      {/each}
    </div>
  {/if}
</RcPanel>

<style>
  .codes { display: flex; flex-direction: column; gap: 4px; }
  .code-row {
    display: grid; grid-template-columns: minmax(0, 1fr) auto 7.5rem 16px; align-items: center; gap: 14px;
    padding: 8px 10px; border-radius: 8px; border: 1px solid var(--border-subtle); text-align: left;
    animation: codeIn 280ms var(--ease-snappy) both;
    transition: background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .code-row:hover { background: var(--bg-element-hover); border-color: var(--border-default); }
  .code-row:active { transform: scale(0.99); }
  .who { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
  .user {
    font: 12px var(--font-mono, ui-monospace, monospace); color: var(--text-primary);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .purpose { font-size: 10.5px; color: var(--text-tertiary); }
  .code {
    font: 600 17px var(--font-mono, ui-monospace, monospace); letter-spacing: 0.12em; color: var(--text-primary);
    font-variant-numeric: tabular-nums;
  }
  .code.password { font-size: 13px; letter-spacing: 0.02em; }
  .when { display: flex; flex-direction: column; align-items: flex-end; font-size: 10.5px; color: var(--text-tertiary); }
  .exp { color: var(--text-tertiary); opacity: 0.8; }
  .copy { color: var(--text-tertiary); display: flex; }
  .code-row:hover .copy { color: var(--text-primary); }
  .note { padding: 14px 12px; border-radius: 8px; background: var(--bg-app); font-size: 11.5px; color: var(--text-tertiary); }
  @keyframes codeIn { from { opacity: 0; transform: translateY(-4px); } }
  @media (prefers-reduced-motion: reduce) { .code-row { animation: none; } }
</style>
