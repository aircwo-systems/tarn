<script lang="ts">
  import { PlusIcon, TrashIcon, XIcon } from "phosphor-svelte";

  import type { FrontendTarget, InfraProbeKind, ThemeMode } from "$lib/state.svelte";

  let {
    open = false,
    pollingIntervalDraft = $bindable(10),
    themeModeDraft = $bindable<ThemeMode>("system"),
    persistenceDraft = $bindable(false),
    schemaSourceDirDraft = $bindable(""),
    logRetentionMinutesDraft = $bindable(15),
    infraEnabledKindsDraft = $bindable<InfraProbeKind[]>([]),
    infraFrontendTargetsDraft = $bindable<FrontendTarget[]>([]),
    newTargetName = $bindable(""),
    newTargetPort = $bindable(""),
    infraKinds = [],
    instanceInfo = null,
    sanitizeSchemaSourceDir = (value: string) => value,
    onClose = () => {},
    onSave = () => {},
    onAddFrontendTarget = () => {},
    onRemoveFrontendTarget = (_id: string) => {},
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
    sanitizeSchemaSourceDir?: (value: string) => string;
    onClose?: () => void;
    onSave?: () => void;
    onAddFrontendTarget?: () => void;
    onRemoveFrontendTarget?: (id: string) => void;
  } = $props();

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === "Escape" && open) onClose();
  }
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
    class="fixed left-1/2 top-1/2 z-[75] w-[min(32rem,90vw)] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-card shadow-xl"
  >
    <div class="flex items-center justify-between border-b border-border px-4 py-3">
      <h2 class="text-[14px] font-semibold text-foreground">UI Settings</h2>
      <button
        type="button"
        onclick={onClose}
        class="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
        aria-label="Close settings"
      >
        <XIcon size={14} />
      </button>
    </div>

    <div class="max-h-[70vh] space-y-4 overflow-y-auto px-4 py-4">
      <p class="text-sm text-muted-foreground/70">
        These preferences are saved in a browser cookie and local storage.
      </p>

      <div class="space-y-1.5">
        <label class="text-sm font-medium text-foreground" for="polling-interval">Polling Interval (seconds)</label>
        <input id="polling-interval" type="number" min="1" max="120" step="1"
          bind:value={pollingIntervalDraft}
          class="w-full rounded-md border border-border bg-muted px-2.5 py-1.5 text-sm text-foreground outline-none focus:ring-1 focus:ring-primary" />
      </div>

      <div class="space-y-1.5">
        <label class="text-sm font-medium text-foreground" for="log-retention">Log Retention (minutes)</label>
        <input id="log-retention" type="number" min="1" max="1440" step="1"
          bind:value={logRetentionMinutesDraft}
          class="w-full rounded-md border border-border bg-muted px-2.5 py-1.5 text-sm text-foreground outline-none focus:ring-1 focus:ring-primary" />
        <p class="text-[11px] leading-relaxed text-muted-foreground/70">
          Automatically remove log events older than this. Default 15 minutes. Max 1440 (24h).
        </p>
      </div>

      <div class="space-y-1.5">
        <label class="text-sm font-medium text-foreground" for="theme-mode">Theme</label>
        <select id="theme-mode" bind:value={themeModeDraft}
          class="w-full rounded-md border border-border bg-muted px-2.5 py-1.5 text-sm text-foreground outline-none focus:ring-1 focus:ring-primary">
          <option value="system">System</option>
          <option value="light">Light</option>
          <option value="dark">Dark</option>
        </select>
      </div>

      <div class="rounded-md border border-border bg-muted/70 p-3">
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0">
            <label class="text-sm font-medium text-foreground" for="persistence-enabled">Persistence</label>
            <p class="mt-1 text-[11px] leading-relaxed text-muted-foreground/70">
              Persist configuration over Tarn sessions.
            </p>
          </div>
          <label class="relative inline-flex cursor-pointer items-center self-center">
            <input id="persistence-enabled" type="checkbox" bind:checked={persistenceDraft} class="peer sr-only" />
            <span class="h-6 w-11 rounded-full border border-border bg-muted transition-colors peer-checked:border-primary/50 peer-checked:bg-primary/10"></span>
            <span class="pointer-events-none absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white shadow-sm transition-transform peer-checked:translate-x-5 dark:bg-zinc-950"></span>
          </label>
        </div>
        <div class="mt-2 text-[11px] font-mono text-muted-foreground/70">{persistenceDraft ? "true" : "false"}</div>
      </div>

      <div class="space-y-1.5">
        <label class="text-sm font-medium text-foreground" for="schema-source">Schema Source</label>
        <input id="schema-source" type="text" placeholder="/path/to/lambda-repos"
          bind:value={schemaSourceDirDraft}
          onblur={() => (schemaSourceDirDraft = sanitizeSchemaSourceDir(schemaSourceDirDraft))}
          class="w-full rounded-md border border-border bg-muted px-2.5 py-1.5 font-mono text-sm text-foreground outline-none focus:ring-1 focus:ring-primary" />
        <p class="text-[11px] leading-relaxed text-muted-foreground/70">
          Local directory used by Chaos Probe to discover <code>schemas.ts</code> and event samples.
        </p>
      </div>

      <div class="rounded-md border border-border bg-muted/70 p-3 space-y-2">
        <p class="text-sm font-semibold uppercase tracking-wide text-muted-foreground/70">Infrastructure Probes</p>
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

      <div class="rounded-md border border-border bg-muted/70 p-3 space-y-2">
        <p class="text-sm font-semibold uppercase tracking-wide text-muted-foreground/70">Frontend Services</p>
        <p class="text-[11px] text-muted-foreground/70 leading-relaxed">
          Add locally running apps to probe from the browser (localhost).
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
        <div class="flex items-center gap-1.5 pt-0.5">
          <input type="text" bind:value={newTargetName}
            onkeydown={(event) => { if (event.key === "Enter") onAddFrontendTarget(); }}
            placeholder="Name"
            class="min-w-0 flex-1 rounded border border-border bg-muted px-2 py-1 text-xs text-foreground placeholder:text-muted-foreground/70 outline-none focus:ring-1 focus:ring-primary" />
          <input type="number" bind:value={newTargetPort}
            onkeydown={(event) => { if (event.key === "Enter") onAddFrontendTarget(); }}
            placeholder="Port" min="1" max="65535"
            class="w-16 rounded border border-border bg-muted px-2 py-1 text-xs text-foreground placeholder:text-muted-foreground/70 outline-none focus:ring-1 focus:ring-primary" />
          <button type="button" onclick={onAddFrontendTarget}
            class="flex h-6 w-6 shrink-0 items-center justify-center rounded border border-primary/50 bg-primary/10 text-primary transition-colors hover:bg-primary/20"
            aria-label="Add frontend service">
            <PlusIcon size={12} />
          </button>
        </div>
      </div>

      <div class="rounded-md border border-border bg-muted/70 p-3">
        <p class="mb-2 text-sm font-semibold uppercase tracking-wide text-muted-foreground/70">Instance Info</p>
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
        class="rounded-md border border-border px-3 py-1.5 text-xs text-muted-foreground hover:bg-muted">Cancel</button>
      <button type="button" onclick={onSave}
        class="rounded-md border border-primary/50 bg-primary/10 px-3 py-1.5 text-xs text-primary hover:bg-primary/20">Save</button>
    </div>
  </div>
{/if}
