<script lang="ts">
  import { onMount, onDestroy, tick } from "svelte";
  import { KeyIcon, LockKeyIcon, PlusIcon } from "phosphor-svelte";
  import RcButton from "#lib/components/rack/rc-button.svelte";
  import {
    AWSConnectionError, fetchAWSOwnerState, fetchAWSConnections, saveAWSConnection,
    testAWSConnection, deleteAWSConnection, lockAWSConnections, ownerUnlockError, fetchAWSProfiles,
    type AWSOwnerState, type AWSConnection, type AWSConnectionInput, type ManualAWSCredentials, type AWSIdentity,
  } from "#lib/aws-connections";
  import { looksLikeAWSCredentialBlock, parseAWSCredentialPaste } from "#lib/aws-credential-paste";

  type Editor = { kind: "closed" } | { kind: "create" } | { kind: "edit" | "replace"; connection: AWSConnection };
  let owner = $state<AWSOwnerState>({ kind: "locked" });
  let connections = $state<AWSConnection[]>([]);
  let editor = $state<Editor>({ kind: "closed" });
  let loading = $state(true);
  let busy = $state("");
  let error = $state("");
  let notice = $state("");
  let name = $state("");
  let region = $state("us-east-1");
  let credentialSource = $state<"profile" | "manual">("profile");
  let credentialKind = $state<ManualAWSCredentials["kind"]>("temporary");
  let profiles = $state<string[]>([]);
  let profile = $state("default");
  let pastedNotice = $state("");
  let accessKeyId = $state("");
  let secretAccessKey = $state("");
  let sessionToken = $state("");
  let proposedIdentity = $state<AWSIdentity | null>(null);
  let identityAcknowledged = $state(false);
  let deleting = $state<AWSConnection | null>(null);
  let errorElement = $state<HTMLParagraphElement>();
  let nameInput = $state<HTMLInputElement>();
  let active = true;

  const needsCredentials = $derived(editor.kind === "create" || editor.kind === "replace");
  const manualAvailable = $derived(owner.kind === "unlocked" && owner.manualCredentialsAvailable);

  function clearSecrets() { accessKeyId = ""; secretAccessKey = ""; sessionToken = ""; pastedNotice = ""; proposedIdentity = null; identityAcknowledged = false; }
  function closeEditor() { clearSecrets(); editor = { kind: "closed" }; }
  function resetAcknowledgement() { proposedIdentity = null; identityAcknowledged = false; }

  async function showError(cause: unknown) {
    error = cause instanceof Error ? cause.message : "Could not manage AWS connections.";
    if (cause instanceof AWSConnectionError && cause.status === 401) {
      owner = { kind: "locked" }; connections = []; closeEditor(); deleting = null;
    }
    await tick();
    errorElement?.focus();
  }

  async function load() {
    loading = true; error = "";
    try {
      const state = await fetchAWSOwnerState();
      if (!active) return;
      owner = state;
      connections = state.kind === "unlocked" ? await fetchAWSConnections() : [];
      profiles = state.kind === "unlocked" ? await fetchAWSProfiles().catch(() => []) : [];
      if (ownerUnlockError()) error = ownerUnlockError();
    } catch (cause) { if (active) await showError(cause); }
    finally { if (active) loading = false; }
  }

  onMount(() => { void load(); });
  onDestroy(() => { active = false; clearSecrets(); connections = []; });

  async function openEditor(next: Editor) {
    closeEditor(); deleting = null; error = ""; notice = "";
    editor = next;
    name = next.kind === "edit" || next.kind === "replace" ? next.connection.name : "";
    region = next.kind === "edit" || next.kind === "replace" ? next.connection.region : "us-east-1";
    const connection = next.kind === "replace" ? next.connection : null;
    credentialSource = connection && connection.kind !== "profile" ? "manual" : "profile";
    credentialKind = connection && connection.kind !== "profile" ? connection.kind : "temporary";
    profile = connection?.kind === "profile" ? connection.profile : profiles.includes("default") ? "default" : profiles[0] ?? "default";
    await tick(); nameInput?.focus();
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    if (busy || editor.kind === "closed") return;
    busy = "save"; error = ""; notice = "";
    const input: AWSConnectionInput = { name: name.trim(), region: region.trim() };
    if (needsCredentials) {
      input.credentials = credentialSource === "profile" ? { kind: "profile", profile: profile.trim() } : credentialKind === "temporary"
        ? { kind: "temporary", accessKeyId: accessKeyId.trim(), secretAccessKey: secretAccessKey.trim(), sessionToken: sessionToken.trim() }
        : { kind: "long-lived", accessKeyId: accessKeyId.trim(), secretAccessKey: secretAccessKey.trim() };
    }
    if (proposedIdentity && identityAcknowledged) input.acceptedIdentity = proposedIdentity;
    const id = editor.kind === "create" ? null : editor.connection.id;
    try {
      const saved = await saveAWSConnection(id, input);
      if (!active) return;
      connections = [...connections.filter((c) => c.id !== saved.id), saved].sort((a, b) => a.name.localeCompare(b.name));
      notice = needsCredentials ? "Identity verified and connection saved. AWS service permissions have not been checked." : "Connection details saved.";
      closeEditor();
    } catch (cause) {
      if (!active) return;
      if (cause instanceof AWSConnectionError && cause.identity) { proposedIdentity = cause.identity; identityAcknowledged = false; }
      await showError(cause);
    } finally { if (active) busy = ""; }
  }

  function pasteCredentials(event: ClipboardEvent, explicit = false) {
    const text = event.clipboardData?.getData("text/plain") ?? "";
    if (!explicit && !looksLikeAWSCredentialBlock(text)) return;
    event.preventDefault();
    if (!manualAvailable) { void showError(new Error("Enable Vault encryption to save copied AWS credentials.")); return; }
    try {
      const parsed = parseAWSCredentialPaste(text);
      clearSecrets(); credentialSource = "manual"; credentialKind = parsed.kind;
      accessKeyId = parsed.accessKeyId; secretAccessKey = parsed.secretAccessKey;
      sessionToken = parsed.kind === "temporary" ? parsed.sessionToken : "";
      error = "";
      pastedNotice = parsed.kind === "temporary" ? "Access key, secret key and session token filled. Ready to validate and save." : "Access key and secret key filled. Ready to validate and save.";
    } catch (cause) { void showError(cause); }
  }

  async function testConnection(connection: AWSConnection) {
    if (busy) return;
    busy = connection.id; error = ""; notice = "";
    try {
      const tested = await testAWSConnection(connection.id);
      if (!active) return;
      connections = connections.map((c) => c.id === tested.id ? tested : c);
      if (tested.status === "verified") notice = "Identity verified. AWS service permissions have not been checked.";
      else await showError(new Error(tested.message));
    } catch (cause) { if (active) await showError(cause); }
    finally { if (active) busy = ""; }
  }

  async function removeConnection() {
    if (!deleting || busy) return;
    const connection = deleting;
    busy = connection.id; error = "";
    try {
      await deleteAWSConnection(connection.id);
      if (!active) return;
      connections = connections.filter((c) => c.id !== connection.id);
      deleting = null;
      notice = "Connection deleted locally. Its AWS keys remain active until revoked in AWS.";
    } catch (cause) { if (active) await showError(cause); }
    finally { if (active) busy = ""; }
  }

  async function lock() {
    if (busy) return;
    busy = "lock"; error = "";
    try {
      await lockAWSConnections();
      closeEditor(); deleting = null; connections = []; owner = { kind: "locked" }; notice = "AWS connections locked.";
    } catch (cause) { await showError(cause); }
    finally { busy = ""; }
  }

  function statusLabel(connection: AWSConnection): string {
    switch (connection.status) {
      case "verified": return "Identity verified";
      case "rejected": return "Credentials rejected";
      case "expired": return "Session expired";
      case "unavailable": return "Could not verify";
      case "untested": return "Not tested this session";
      case "needs-login": return "Sign in again";
    }
  }
  function formatDate(value: string): string { return new Date(value).toLocaleString(); }
</script>

<svelte:window onhashchange={() => { closeEditor(); void load(); }} />

<div class="aws-settings">
  <div class="intro">
    <p>Connect through an existing AWS profile, or save manual credentials encrypted with Tarn's Vault key.</p>
    <p class="scope">Resource browsing comes later. Saved credentials are not passed to local workloads.</p>
  </div>

  {#if error}<p class="feedback error" role="alert" tabindex="-1" bind:this={errorElement}>{error}</p>{/if}
  {#if notice}<p class="feedback notice" role="status">{notice}</p>{/if}

  {#if loading}
    <p class="hint" role="status">Checking local owner session…</p>
  {:else if owner.kind === "unavailable"}
    <div class="access-state">
      <LockKeyIcon size={18} aria-hidden="true" />
      <div><h3>Local access required</h3><p>{owner.reason}</p><code>tarn start --host 127.0.0.1 --ui</code></div>
    </div>
  {:else if owner.kind === "locked"}
    <div class="access-state">
      <LockKeyIcon size={18} aria-hidden="true" />
      <div><h3>Unlock from your terminal</h3><p>Run this beside your local Tarn server, then open the single-use link it prints.</p><code>tarn vault unlock</code><p class="hint">Use the same <code>--data-dir</code> if you started Tarn with a custom directory. The owner session lasts eight hours.</p></div>
    </div>
    <RcButton onclick={load}>Check session</RcButton>
  {:else}
    <div class="toolbar">
      <span class="owner-state"><span class="owner-dot"></span>Local owner session</span>
      <div class="actions"><RcButton disabled={!!busy} onclick={() => openEditor({ kind: "create" })}><PlusIcon size={12} aria-hidden="true" />Add AWS connection</RcButton><RcButton variant="ghost" disabled={!!busy} onclick={lock}>Lock</RcButton></div>
    </div>

    {#if connections.length === 0 && editor.kind === "closed"}
      <div class="empty"><KeyIcon size={20} aria-hidden="true" /><div><h3>No AWS connections yet</h3><p>Choose an AWS profile or add credentials to verify its account and identity.</p></div></div>
    {/if}

    <ul class="connections" aria-label="Saved AWS connections">
      {#each connections as connection (connection.id)}
        <li class="connection">
          <div class="connection-heading"><h3>{connection.name}</h3><span class="test-status" data-status={connection.status}>{statusLabel(connection)}</span></div>
          <dl>
            <div><dt>Account</dt><dd class="mono">{connection.identity.accountId}</dd></div>
            <div><dt>Region</dt><dd class="mono">{connection.region}</dd></div>
            <div><dt>Source</dt><dd>{#if connection.kind === "profile"}AWS profile <span class="mono">{connection.profile}</span>{:else}{connection.kind === "temporary" ? "Temporary session" : "Long-lived key"} <span class="mono">••••{connection.accessKeySuffix}</span>{/if}</dd></div>
            <div class="identity"><dt>Identity</dt><dd class="mono">{connection.identity.arn}</dd></div>
            <div class="identity"><dt>Last verified</dt><dd>{formatDate(connection.lastVerifiedAt)}</dd></div>
            {#if connection.status !== "untested"}<div class="identity"><dt>Last tested</dt><dd>{formatDate(connection.lastTestedAt)}</dd></div>{/if}
          </dl>
          {#if connection.message}<p class="hint">{connection.message}</p>{/if}
          <div class="actions">
            <RcButton disabled={!!busy} onclick={() => testConnection(connection)}>{busy === connection.id ? "Working…" : "Test connection"}</RcButton>
            <RcButton variant="ghost" disabled={!!busy} onclick={() => openEditor({ kind: "edit", connection })}>Edit details</RcButton>
            <RcButton variant="ghost" disabled={!!busy} onclick={() => openEditor({ kind: "replace", connection })}>Change credential source</RcButton>
            <RcButton variant="danger" disabled={!!busy} onclick={() => { closeEditor(); deleting = connection; error = ""; notice = ""; }}>Delete</RcButton>
          </div>
          {#if deleting?.id === connection.id}
            <div class="confirmation" role="group" aria-label="Confirm connection deletion">
              <p>Delete {connection.name} from this machine? Its AWS keys will remain active. No AWS resources will be deleted.</p>
              <div class="actions"><RcButton variant="danger" disabled={!!busy} onclick={removeConnection}>Delete local connection</RcButton><RcButton disabled={!!busy} onclick={() => deleting = null}>Cancel</RcButton></div>
            </div>
          {/if}
        </li>
      {/each}
    </ul>

    {#if editor.kind !== "closed"}
      <form class="editor" onsubmit={save}>
        <h3>{editor.kind === "create" ? "Add AWS connection" : editor.kind === "replace" ? `Change credential source for ${editor.connection.name}` : `Edit ${editor.connection.name}`}</h3>
        <fieldset disabled={!!busy}>
          <div class="form-grid">
            <label>Connection name<input bind:this={nameInput} bind:value={name} oninput={resetAcknowledgement} required maxlength="80" autocomplete="off" placeholder="Development" /></label>
            <label>AWS region<input class="mono" bind:value={region} oninput={resetAcknowledgement} required autocomplete="off" placeholder="eu-west-2" /></label>
            {#if needsCredentials}
              <label class="wide">Credential source<select bind:value={credentialSource} onchange={() => { clearSecrets(); error = ""; }}><option value="profile">Existing AWS profile</option><option value="manual" disabled={!manualAvailable}>Manual credentials in Vault</option></select></label>
              {#if credentialSource === "profile"}
                <label class="wide">AWS profile<input class="mono" list="tarn-aws-profiles" bind:value={profile} oninput={resetAcknowledgement} required maxlength="128" autocomplete="off" placeholder="company-dev" /></label>
                <datalist id="tarn-aws-profiles">{#each profiles as profileName (profileName)}<option value={profileName}></option>{/each}</datalist>
                <p class="hint wide">Uses the profile on the Tarn server. Tarn stores its name and region, and lets the AWS SDK renew credentials while your provider session permits it. When sign-in is required, log in through the AWS CLI and test again.</p>
              {/if}
              {#if manualAvailable}
                <label class="wide">Paste AWS credentials<textarea rows="2" autocomplete="off" spellcheck="false" onpaste={(event) => pasteCredentials(event, true)} oninput={(event) => { event.currentTarget.value = ""; }} placeholder="Paste the full credential block copied from AWS. Each key will fill its own field."></textarea></label>
                {#if pastedNotice}<p class="hint wide" role="status">{pastedNotice}</p>{/if}
              {:else}<p class="hint wide">Manual credentials require Vault encryption. Profile references can be saved without a Vault key.</p>{/if}
              {#if credentialSource === "manual"}
                <label class="wide">Credential type<select bind:value={credentialKind} onchange={() => { sessionToken = ""; resetAcknowledgement(); }}><option value="temporary">Temporary session</option><option value="long-lived">Long-lived access key</option></select></label>
                <label>Access key ID<input class="mono" bind:value={accessKeyId} onpaste={pasteCredentials} oninput={resetAcknowledgement} type="password" required minlength="16" maxlength="128" autocomplete="off" spellcheck="false" /></label>
                <label>Secret access key<input class="mono" bind:value={secretAccessKey} onpaste={pasteCredentials} oninput={resetAcknowledgement} type="password" required maxlength="4096" autocomplete="new-password" spellcheck="false" /></label>
                {#if credentialKind === "temporary"}<label class="wide">Session token<input class="mono" type="password" bind:value={sessionToken} onpaste={pasteCredentials} oninput={resetAcknowledgement} required maxlength="16384" autocomplete="off" spellcheck="false" /></label>{/if}
                <p class="hint wide">Manual credentials are encrypted in Vault. Temporary sessions need replacement when they expire. AWS is contacted only when you validate or test a connection.</p>
              {/if}
            {/if}
          </div>
          {#if proposedIdentity && editor.kind !== "create"}
            <div class="confirmation">
              <p>This replacement changes the saved AWS identity.</p>
              <p class="mono">Current: {editor.connection.identity.accountId} · {editor.connection.identity.arn}</p>
              <p class="mono">New: {proposedIdentity.accountId} · {proposedIdentity.arn}</p>
              <label class="acknowledgement"><input type="checkbox" bind:checked={identityAcknowledged} />I confirm this new AWS account and identity.</label>
            </div>
          {/if}
          <div class="actions"><RcButton type="submit" variant="primary" disabled={!!busy || (!!proposedIdentity && !identityAcknowledged)}>{busy === "save" ? "Saving…" : needsCredentials ? "Validate and save" : "Save details"}</RcButton><RcButton disabled={!!busy} onclick={() => { closeEditor(); error = ""; }}>Cancel</RcButton></div>
        </fieldset>
      </form>
    {/if}
    <p class="footnote">Identity tests do not check AWS service permissions. Connections are separate from your local Tarn accounts.</p>
  {/if}
</div>

<style>
  .aws-settings { color: var(--text-secondary); font-size: 12px; }
  p { margin: 0; line-height: 1.6; }
  h3 { margin: 0; font-size: 12.5px; font-weight: 550; color: var(--text-primary); }
  .intro { display: grid; gap: 4px; margin-bottom: 16px; }
  .scope, .hint, .footnote { color: var(--text-tertiary); font-size: 11.5px; }
  .mono, code { font-family: var(--font-mono); }
  code { display: inline-block; margin: 8px 0; color: var(--text-primary); font-size: 11.5px; }
  .hint code { margin: 0; }
  .access-state, .empty { display: flex; gap: 12px; padding: 20px 0; border-top: 1px solid var(--border-subtle); }
  .access-state > :global(svg), .empty > :global(svg) { flex: 0 0 auto; margin-top: 2px; color: var(--text-tertiary); }
  .access-state h3, .empty h3 { margin-bottom: 4px; }
  .toolbar, .connection-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
  .toolbar { padding: 12px 0; border-top: 1px solid var(--border-subtle); }
  .owner-state { display: inline-flex; gap: 7px; align-items: center; font-size: 11px; }
  .owner-dot { width: 5px; height: 5px; border-radius: 50%; background: var(--accent-green); }
  .actions { display: flex; flex-wrap: wrap; gap: 8px; }
  .connections { list-style: none; margin: 0; padding: 0; }
  .connection { padding: 20px 0; border-top: 1px solid var(--border-subtle); }
  .test-status { font-size: 11px; color: var(--text-tertiary); }
  .test-status[data-status="verified"] { color: var(--accent-green); }
  .test-status[data-status="expired"], .test-status[data-status="rejected"], .test-status[data-status="needs-login"] { color: var(--accent-red); }
  dl { display: flex; flex-wrap: wrap; gap: 10px 24px; margin: 12px 0; }
  dl > div { display: flex; gap: 8px; }
  dt { color: var(--text-tertiary); }
  dd { margin: 0; overflow-wrap: anywhere; }
  .identity { width: 100%; }
  .connection .hint { margin-bottom: 12px; }
  .editor { margin-top: 12px; border-top: 1px solid var(--border-default); padding: 20px 0; }
  .editor h3 { margin-bottom: 16px; }
  fieldset { border: 0; padding: 0; margin: 0; min-width: 0; }
  .form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-bottom: 16px; }
  label { display: grid; gap: 6px; color: var(--text-primary); font-size: 12px; }
  .wide { grid-column: 1 / -1; }
  input:not([type="checkbox"]), select, textarea { width: 100%; min-width: 0; padding: 8px 10px; background: var(--bg-app); border: 1px solid var(--border-subtle); border-radius: 6px; color: var(--text-primary); font-size: 12px; }
  input:focus-visible, select:focus-visible, textarea:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }
  textarea { resize: vertical; }
  .confirmation { border-left: 2px solid var(--accent-amber); padding: 0 0 0 12px; margin: 16px 0; display: grid; gap: 8px; overflow-wrap: anywhere; }
  .acknowledgement { display: flex; align-items: flex-start; gap: 8px; padding: 4px 0; }
  .feedback { margin: 12px 0; padding-left: 12px; border-left: 2px solid; }
  .error { color: var(--accent-red); }
  .notice { color: var(--accent-green); }
  .feedback:focus { outline: none; }
  .footnote { border-top: 1px solid var(--border-subtle); padding-top: 12px; }
  @media (max-width: 640px) {
    .form-grid { grid-template-columns: 1fr; }
    .actions :global(button) { min-height: 44px; }
    dl { gap: 10px; } dl > div { width: 100%; }
  }
</style>
