<script lang="ts">
  import RcPanel from "#lib/components/rack/rc-panel.svelte";
  import RcKv from "#lib/components/rack/rc-kv.svelte";
  import type { CognitoPoolDetail } from "#lib/types";
  import { functionName } from "./cognito-ui.svelte";

  let { pool, index = 0 }: { pool: CognitoPoolDetail; index?: number } = $props();

  const s = $derived(pool.settings);
  const pp = $derived(s.passwordPolicy);
  const signIn = $derived(
    s.usernameAttributes?.length
      ? `${s.usernameAttributes.join(" or ")} (as username)`
      : s.aliasAttributes?.length
        ? `username, or verified ${s.aliasAttributes.join(", ")}`
        : "username",
  );
  const passwordRules = $derived(
    pp
      ? [
          `${pp.MinimumLength ?? 8}+ characters`,
          pp.RequireUppercase && "uppercase",
          pp.RequireLowercase && "lowercase",
          pp.RequireNumbers && "number",
          pp.RequireSymbols && "symbol",
        ].filter(Boolean).join(", ")
      : "--",
  );
  const mfa = $derived(
    s.mfaMode === "OFF"
      ? "Off: users are never challenged, whatever their preference"
      : `${s.mfaMode === "ON" ? "Required" : "Optional"} · ${[s.smsMfa && "SMS", s.emailMfa && "email"].filter(Boolean).join(" and ") || "no factor configured"}`,
  );
  const triggers = $derived(Object.entries(pool.lambdaConfig).sort(([a], [b]) => a.localeCompare(b)));
  const customSender = $derived("CustomEmailSender" in pool.lambdaConfig || "CustomSMSSender" in pool.lambdaConfig);
</script>

<RcPanel title="Sign-in" {index}>
  <RcKv labelWidth="11rem" items={[
    { label: "Sign in with", value: signIn },
    { label: "Usernames", value: s.caseSensitive ? "Case sensitive" : "Case insensitive" },
    { label: "Auto-verified", value: s.autoVerifiedAttributes?.join(", ") || "None: sign-ups get no code and need an admin to confirm", dim: !s.autoVerifiedAttributes?.length },
    { label: "Password", value: passwordRules },
    { label: "Temporary passwords", value: pp ? `Valid ${pp.TemporaryPasswordValidityDays} days` : "--" },
    { label: "MFA", value: mfa, dim: s.mfaMode === "OFF" },
    { label: "Self sign-up", value: s.adminCreateOnly ? "Disabled (admins create users)" : "Allowed" },
    { label: "Required attributes", value: s.requiredAttributes?.join(", ") || "None", dim: !s.requiredAttributes?.length },
    { label: "Custom attributes", value: s.customAttributes?.join(", ") || "None", mono: !!s.customAttributes?.length, dim: !s.customAttributes?.length },
  ]} />
</RcPanel>

<RcPanel title="Lambda triggers" description={pool.tarn.triggers ? "Run on Tarn's Lambda service" : "Configured but not run: no Lambda invoker"} index={index + 1}>
  {#if triggers.length}
    <RcKv labelWidth="11rem" items={triggers.map(([name, arn]) => ({
      label: name,
      value: name === "PreTokenGeneration" && pool.preTokenGenerationVersion
        ? `${functionName(arn)} · ${pool.preTokenGenerationVersion === "V1_0" ? "V1_0, changes the ID token only" : `${pool.preTokenGenerationVersion}, changes ID and access tokens`}`
        : functionName(arn),
      mono: true,
    }))} />
    {#if customSender}
      {#if pool.kmsKeyId}
        <a class="kms-link" href="#kms?{new URLSearchParams({ key: pool.kmsKeyId })}">
          <span>Codes are encrypted with</span>
          <code title={pool.kmsKeyId}>{pool.kmsKeyId}</code>
          <em>Open in KMS</em>
        </a>
      {:else}
        <p class="note warn">Custom senders are skipped: the pool has no KMSKeyID.</p>
      {/if}
    {/if}
  {:else}
    <p class="note">No triggers configured.</p>
  {/if}
</RcPanel>

<RcPanel title="Tokens and Tarn" description="Where tokens come from and the local settings that change behaviour" index={index + 2}>
  <RcKv labelWidth="11rem" items={[
    { label: "Issuer", value: pool.issuer, mono: true },
    { label: "JWKS", value: pool.jwksUrl, mono: true },
    { label: "Issuer mode", value: `${pool.tarn.issuerMode} (TARN_COGNITO_ISSUER)` },
    { label: "Codes", value: `${pool.tarn.fixedCode ? "Fixed (TARN_COGNITO_FIXED_CODE)" : "Random 6-digit"}${customSender && pool.kmsKeyId ? ", sent encrypted to the custom sender" : ", never sent"}` },
    { label: "Token lifetime", value: pool.tarn.tokenTtl ? `${pool.tarn.tokenTtl} for every client (TARN_COGNITO_TOKEN_TTL)` : "Per app client" },
    ...(pool.resourceServerScopes?.length ? [{ label: "Resource scopes", value: pool.resourceServerScopes.join(" "), mono: true }] : []),
    ...(pool.domain ? [{ label: "Domain", value: pool.domain, mono: true }] : []),
    { label: "Deletion protection", value: s.deletionProtection === "ACTIVE" ? "Active" : "Inactive" },
    { label: "ARN", value: pool.arn, mono: true, dim: true },
  ]} />
</RcPanel>

<style>
  .note { padding: 14px 12px; border-radius: 8px; background: var(--bg-app); font-size: 11.5px; color: var(--text-tertiary); }
  .note.warn { margin-top: 12px; color: var(--accent-amber); }
  .kms-link {
    display: flex; align-items: center; gap: 8px; margin-top: 12px; padding: 9px 10px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11.5px; color: var(--text-tertiary); min-width: 0;
    transition: border-color 120ms ease, background 120ms ease;
  }
  .kms-link:hover { border-color: var(--border-default); background: var(--bg-element-hover); }
  .kms-link code {
    flex: 1; min-width: 0; font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-primary);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .kms-link em { font-style: normal; color: var(--text-secondary); white-space: nowrap; }
  .kms-link:hover em { color: var(--text-primary); }
</style>
