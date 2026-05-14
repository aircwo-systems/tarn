<script lang="ts">
  import { PlusIcon, TrashIcon, XIcon, CheckIcon } from "phosphor-svelte";

  import type { FrontendTarget, InfraProbeKind, KnownAccount, ThemeMode } from "$lib/state.svelte";

  let {
    open = false,
    pollingIntervalDraft = $bindable(10),
    themeModeDraft = $bindable<ThemeMode>("system"),
    persistenceDraft = $bindable(false),
    schemaSourceDirDraft = $bindable(""),
    logRetentionMinutesDraft = $bindable(30),
    infraEnabledKindsDraft = $bindable<InfraProbeKind[]>([]),
    infraFrontendTargetsDraft = $bindable<FrontendTarget[]>([]),
    newTargetName = $bindable(""),
    newTargetPort = $bindable(""),
    infraKinds = [],
    instanceInfo = null,
    knownAccounts = [],
    activeAccountId = "000000000000",
    sanitizeSchemaSourceDir = (value: string) => value,
    onClose = () => {},
    onSave = () => {},
    onAddFrontendTarget = () => {},
    onRemoveFrontendTarget = (_id: string) => {},
    onSwitchAccount = (_id: string) => {},
    onAddAccount = (_id: string, _label: string) => false,
    onRemoveAccount = (_id: string) => {},
  }: {
    open?: boolean;
    pollingIntervalDraft?: number;
    themeModeDraft?: ThemeMode;
    persistenceDraft?: boolean;
    schemaSourceDirDraft?: string;
    logRetentionMinutesDraft?: number;
    infraEnabledKindsDraft?: InfraProbeKind[];
    infraFrontendTargetsDraft?: FrontendTarget[];
    newTargetName?: string;
    newTargetPort?: string;
    infraKinds?: Array<{ id: InfraProbeKind; label: string; detail: string }>;
    instanceInfo?: {
      region?: string;
      accountId?: string;
      endpoint?: string;
    } | null;
    knownAccounts?: KnownAccount[];
    activeAccountId?: string;
    sanitizeSchemaSourceDir?: (value: string) => string;
    onClose?: () => void;
    onSave?: () => void;
    onAddFrontendTarget?: () => void;
    onRemoveFrontendTarget?: (id: string) => void;
    onSwitchAccount?: (id: string) => void;
    onAddAccount?: (id: string, label: string) => boolean;
    onRemoveAccount?: (id: string) => void;
  } = $props();

  let newAccountId = $state("");
  let newAccountLabel = $state("");
  let newAccountError = $state("");

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === "Escape" && open) onClose();
  }

  function handleAddAccount() {
    newAccountError = "";
    const id = newAccountId.trim();
    if (!/^\d{12}$/.test(id)) {
      newAccountError = "Must be exactly 12 digits";
      return;
    }
    const ok = onAddAccount(id, newAccountLabel);
    if (!ok) {
      newAccountError = "Account already exists";
      return;
    }
    newAccountId = "";
    newAccountLabel = "";
  }

  const fieldClass =
    "h-8 w-full rounded border border-border bg-background px-2.5 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary";
  const sectionClass = "space-y-2 py-4";
  const sectionHeadingClass =
    "text-[11px] font-semibold uppercase tracking-[0.2em] text-muted-foreground/70";
  const unitFieldClass =
    `${fieldClass} pr-16`;
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
  <div
    class="fixed inset-0 z-[70] bg-black/45"
    onclick={onClose}
    aria-hidden="true"
  ></div>
  <div
    role="dialog"
    aria-modal="true"
    aria-label="UI Settings"
    class="fixed left-1/2 top-1/2 z-[75] w-[min(72rem,calc(100vw-4rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background shadow-xl"
  >
    <div class="flex items-start justify-between border-b border-border px-4 py-3">
      <div class="space-y-1">
        <h2 class="text-[14px] font-semibold text-foreground">UI Settings</h2>
        <p class="text-sm text-muted-foreground/70">
          These preferences are saved in a browser cookie and local storage.
        </p>
      </div>
      <button
        type="button"
        onclick={onClose}
        class="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-background-subtle hover:text-foreground"
        aria-label="Close settings"
      >
        <XIcon size={14} />
      </button>
    </div>

    <div class="max-h-[70vh] space-y-1 overflow-y-auto px-4 py-4">
      <div class={sectionClass}>
        <p class={sectionHeadingClass}>Accounts</p>
        <p class="text-[11px] text-muted-foreground/70 leading-relaxed">
          Switch between AWS accounts. The active account is used for all API requests.
          Use a 12-digit account ID (e.g. <code class="font-mono">111111111111</code>).
        </p>
        <ul class="mt-2 space-y-1">
          {#each knownAccounts as acct (acct.id)}
            {@const isActive = acct.id === activeAccountId}
            <li class="flex items-center gap-2 rounded-md border px-2.5 py-1.5
              {isActive ? 'border-primary/40 bg-primary/[0.06]' : 'border-border bg-background-subtle/30'}">
              <span class="h-1.5 w-1.5 shrink-0 rounded-full {isActive ? 'bg-primary shadow-[0_0_4px_var(--color-primary)]' : 'bg-muted-foreground/30'}"></span>
              <span class="flex-1 min-w-0">
                <span class="block truncate text-xs {isActive ? 'font-semibold text-primary' : 'text-foreground'}">{acct.label}</span>
                <span class="block font-mono text-[10px] text-muted-foreground/60">{acct.id}</span>
              </span>
              {#if !isActive}
                <button
                  type="button"
                  onclick={() => onSwitchAccount(acct.id)}
                  class="shrink-0 rounded border border-primary/40 bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary transition-colors hover:bg-primary/20"
                >
                  Switch
                </button>
              {:else}
                <span class="flex shrink-0 items-center gap-1 text-[10px] font-medium text-primary/70">
                  <CheckIcon size={11} />Active
                </span>
              {/if}
              {#if acct.id !== "000000000000"}
                <button
                  type="button"
                  onclick={() => onRemoveAccount(acct.id)}
                  class="flex h-5 w-5 shrink-0 items-center justify-center rounded text-muted-foreground/50 transition-colors hover:bg-destructive/10 hover:text-destructive"
                  aria-label={`Remove account ${acct.id}`}
                >
                  <TrashIcon size={11} />
                </button>
              {/if}
            </li>
          {/each}
        </ul>
        <div class="mt-2 grid grid-cols-[6rem_minmax(0,1fr)_auto] items-start gap-1.5">
          <div class="relative">
            <input
              type="text"
              maxlength="12"
              bind:value={newAccountId}
              onkeydown={(e) => { if (e.key === "Enter") handleAddAccount(); }}
              placeholder="012345678901"
              class="{fieldClass} font-mono placeholder:text-muted-foreground/40"
            />
          </div>
          <input
            type="text"
            bind:value={newAccountLabel}
            onkeydown={(e) => { if (e.key === "Enter") handleAddAccount(); }}
            placeholder="Label (optional)"
            class="{fieldClass} placeholder:text-muted-foreground/40"
          />
          <button
            type="button"
            onclick={handleAddAccount}
            class="flex h-8 w-8 shrink-0 items-center justify-center rounded border border-primary/50 bg-primary/10 text-primary transition-colors hover:bg-primary/20"
            aria-label="Add account"
          >
            <PlusIcon size={12} />
          </button>
        </div>
        {#if newAccountError}
          <p class="text-[11px] text-destructive">{newAccountError}</p>
        {/if}
      </div>

      <div class={sectionClass}>
        <p class={sectionHeadingClass}>Polling Interval</p>
        <div class="space-y-1.5">
          <div class="max-w-xs">
            <label class="text-sm font-medium text-foreground" for="polling-interval">Seconds</label>
            <div class="relative">
              <input id="polling-interval" type="number" min="1" max="120" step="1"
                bind:value={pollingIntervalDraft}
                class={unitFieldClass} />
              <div class="pointer-events-none absolute inset-y-1.5 right-2 flex items-center gap-2">
                <span class="h-4 w-px bg-border/70"></span>
                <span class="text-[11px] font-medium text-muted-foreground/70">Seconds</span>
              </div>
            </div>
          </div>
          <p class="text-[11px] leading-relaxed text-muted-foreground/70">
            Controls how often the dashboard refreshes live backend state.
          </p>
        </div>
      </div>

      <div class={sectionClass}>
        <p class={sectionHeadingClass}>Log &amp; Trace Retention</p>
        <div class="space-y-1.5">
          <div class="max-w-xs">
            <label class="sr-only" for="log-retention">Log &amp; Trace Retention (minutes)</label>
            <div class="relative">
              <input id="log-retention" type="number" min="1" max="1440" step="1"
                bind:value={logRetentionMinutesDraft}
                class={unitFieldClass} />
              <div class="pointer-events-none absolute inset-y-1.5 right-2 flex items-center gap-2">
                <span class="h-4 w-px bg-border/70"></span>
                <span class="text-[11px] font-medium text-muted-foreground/70">Minutes</span>
              </div>
            </div>
          </div>
          <p class="text-[11px] leading-relaxed text-muted-foreground/70">
            Automatically remove log events and traces older than this. Default 30 minutes. Max 1440 (24h).
          </p>
        </div>
      </div>

      <div class={sectionClass}>
        <p class={sectionHeadingClass}>Theme</p>
        <div class="max-w-xs space-y-1.5">
          <label class="sr-only" for="theme-mode">Theme</label>
          <select id="theme-mode" bind:value={themeModeDraft}
            class={fieldClass}>
            <option value="system">System</option>
            <option value="light">Light</option>
            <option value="dark">Dark</option>
          </select>
        </div>
      </div>

      <div class={sectionClass}>
        <p class={sectionHeadingClass}>Persistence</p>
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0">
            <label class="text-sm font-medium text-foreground" for="persistence-enabled">
              {persistenceDraft ? "Enabled" : "Disabled"}
            </label>
            <p class="mt-1 text-[11px] leading-relaxed text-muted-foreground/70">
              Persist configuration over Tarn sessions.
            </p>
          </div>
          <label class="relative inline-flex cursor-pointer items-center self-center">
            <input id="persistence-enabled" type="checkbox" bind:checked={persistenceDraft} class="peer sr-only" />
            <span class="h-6 w-11 rounded-full border border-border/80 bg-muted/70 transition-colors peer-checked:border-primary/60 peer-checked:bg-primary/20 dark:bg-zinc-800"></span>
            <span class="pointer-events-none absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-background shadow-sm ring-1 ring-border/70 transition-transform peer-checked:translate-x-5 dark:bg-white dark:ring-white/15"></span>
          </label>
        </div>
      </div>

      <div class={sectionClass}>
        <p class={sectionHeadingClass}>Schema Source</p>
        <div class="space-y-1.5">
          <label class="sr-only" for="schema-source">Schema Source</label>
          <input id="schema-source" type="text" placeholder="/path/to/lambda-repos"
            bind:value={schemaSourceDirDraft}
            onblur={() => (schemaSourceDirDraft = sanitizeSchemaSourceDir(schemaSourceDirDraft))}
            class={`${fieldClass} font-mono`} />
          <p class="text-[11px] leading-relaxed text-muted-foreground/70">
            Local directory used by Chaos Probe to discover <code>schemas.ts</code> and event samples.
          </p>
        </div>
      </div>

      <div class={sectionClass}>
        <p class={sectionHeadingClass}>Infrastructure Probes</p>
        <p class="text-[11px] text-muted-foreground/70 leading-relaxed">
          Show local services probed by the backend. Docker is checked by default.
        </p>
        <div class="space-y-1.5 pt-0.5">
          {#each infraKinds as kind}
            <label class="group flex cursor-pointer items-center gap-2.5">
              <input type="checkbox" bind:group={infraEnabledKindsDraft} value={kind.id}
                class="h-3.5 w-3.5 cursor-pointer rounded" style="accent-color: var(--color-accent)" />
              <span class="flex-1 text-sm text-foreground group-hover:text-foreground">{kind.label}</span>
              <span class="text-[10px] font-mono text-muted-foreground/70">{kind.detail}</span>
            </label>
          {/each}
        </div>
      </div>

      <div class={sectionClass}>
        <p class={sectionHeadingClass}>Additional Services</p>
        <p class="text-[11px] text-muted-foreground/70 leading-relaxed">
          Add external APIs or frontend apps that sit alongside Tarn so the dashboard can probe them and include them in the topology and health view.
        </p>
        {#if infraFrontendTargetsDraft.length > 0}
          <ul class="space-y-1 pt-0.5">
            {#each infraFrontendTargetsDraft as target (target.id)}
              <li class="group flex items-center gap-2">
                <span class="flex-1 truncate text-xs text-foreground">{target.name}</span>
                <span class="shrink-0 text-[10px] font-mono text-muted-foreground/70">:{target.port}</span>
                <button type="button" onclick={() => onRemoveFrontendTarget(target.id)}
                  class="flex h-5 w-5 items-center justify-center rounded text-muted-foreground/70 transition-colors opacity-0 group-hover:opacity-100 hover:bg-destructive/10 hover:text-destructive"
                  aria-label={`Remove ${target.name}`}>
                  <TrashIcon size={11} />
                </button>
              </li>
            {/each}
          </ul>
        {/if}
        <div class="grid grid-cols-[minmax(0,3fr)_7rem_auto] items-center gap-1.5 pt-0.5">
          <input type="text" bind:value={newTargetName}
            onkeydown={(event) => { if (event.key === "Enter") onAddFrontendTarget(); }}
            placeholder="Service name"
            class={`${fieldClass} min-w-0 flex-1 placeholder:text-muted-foreground/70`} />
          <input type="number" bind:value={newTargetPort}
            onkeydown={(event) => { if (event.key === "Enter") onAddFrontendTarget(); }}
            placeholder="Port" min="1" max="65535"
            class={`${fieldClass} w-full shrink-0 placeholder:text-muted-foreground/70`} />
          <button type="button" onclick={onAddFrontendTarget}
            class="flex h-6 w-6 shrink-0 items-center justify-center rounded border border-primary/50 bg-primary/10 text-primary transition-colors hover:bg-primary/20"
            aria-label="Add frontend service">
            <PlusIcon size={12} />
          </button>
        </div>
      </div>

      <div class={sectionClass}>
        <p class={sectionHeadingClass}>Instance Info</p>
        <div class="space-y-1.5 text-sm">
          <div class="grid grid-cols-[6.5rem_1fr] gap-2">
            <span class="text-muted-foreground/70">Region</span>
            <span class="break-all font-mono text-foreground">{instanceInfo?.region ?? "--"}</span>
          </div>
          <div class="grid grid-cols-[6.5rem_1fr] gap-2">
            <span class="text-muted-foreground/70">Account</span>
            <span class="break-all font-mono text-foreground">{instanceInfo?.accountId ?? "--"}</span>
          </div>
          <div class="grid grid-cols-[6.5rem_1fr] gap-2">
            <span class="text-muted-foreground/70">API URL</span>
            <span class="break-all font-mono text-foreground">{instanceInfo?.endpoint ?? "--"}</span>
          </div>
        </div>
        <p class="mt-2 text-[11px] text-muted-foreground/70">These values are currently read-only.</p>
      </div>
    </div>

    <div class="flex items-center justify-end gap-2 border-t border-border px-4 py-3">
      <button type="button" onclick={onClose}
        class="rounded-md border border-border px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-background-subtle">Cancel</button>
      <button type="button" onclick={onSave}
        class="rounded-md border border-primary/50 bg-primary/10 px-3 py-1.5 text-xs text-primary hover:bg-primary/20">Save</button>
    </div>
  </div>
{/if}
