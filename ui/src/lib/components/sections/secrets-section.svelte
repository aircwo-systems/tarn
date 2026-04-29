<script lang="ts">
  import {
    EyeIcon,
    EyeSlashIcon,
    KeyIcon,
    CopyIcon,
    CheckIcon,
  } from "phosphor-svelte";
  import {
    Table,
    TableHeader,
    TableBody,
    TableRow,
    TableHead,
    TableCell,
  } from "$lib/components/ui/table";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import EmptyState from "$lib/components/common/empty-state.svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import DetailPanel from "$lib/components/common/detail-panel.svelte";
  import SectionHeader from "./section-header.svelte";
  import { fetchSecretValue } from "$lib/api";
  import { formatJSONForViewer } from "$lib/json-format";
  import {
    getDashboard,
    getDashboardFilters,
    matchesTagFilter,
  } from "$lib/state.svelte";
  import { formatDate } from "$lib/utils";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const secrets = $derived(
    (dashboard.data?.secrets ?? []).filter((secret) =>
      matchesTagFilter(secret.tags, filters.tagFilter),
    ),
  );

  const revealedCount = $derived(
    secrets.filter((secret) => secretVisible[secret.name]).length,
  );
  const failedLoads = $derived(
    secrets.filter((secret) => Boolean(secretErrors[secret.name])).length,
  );

  let secretValues = $state<Record<string, string>>({});
  let secretValueTypes = $state<Record<string, string>>({});
  let secretVisible = $state<Record<string, boolean>>({});
  let secretLoading = $state<Record<string, boolean>>({});
  let secretErrors = $state<Record<string, string>>({});

  let selectedSecretName = $state<string | null>(null);
  let copiedField = $state<string | null>(null);

  $effect(() => {
    if (selectedSecretName && !secrets.some((s) => s.name === selectedSecretName)) {
      selectedSecretName = null;
    }
  });

  const selectedSecret = $derived(
    secrets.find((s) => s.name === selectedSecretName) ?? null,
  );

  function selectSecret(name: string) {
    selectedSecretName = selectedSecretName === name ? null : name;
  }

  function hasLoadedSecretValue(name: string): boolean {
    return Object.prototype.hasOwnProperty.call(secretValues, name);
  }

  async function toggleSecretValue(name: string) {
    if (secretVisible[name]) {
      secretVisible = { ...secretVisible, [name]: false };
      return;
    }
    if (!hasLoadedSecretValue(name) && !secretLoading[name]) {
      await loadSecretValue(name);
    }
    if (!secretErrors[name]) {
      secretVisible = { ...secretVisible, [name]: true };
    }
  }

  async function loadSecretValue(name: string) {
    secretLoading = { ...secretLoading, [name]: true };
    secretErrors = { ...secretErrors, [name]: "" };
    try {
      const secret = await fetchSecretValue(name);
      secretValues = { ...secretValues, [name]: secret.value };
      secretValueTypes = { ...secretValueTypes, [name]: secret.valueType };
    } catch (error) {
      secretErrors = {
        ...secretErrors,
        [name]: error instanceof Error ? error.message : "Failed to load secret value",
      };
    } finally {
      secretLoading = { ...secretLoading, [name]: false };
    }
  }

  function renderSecretValue(name: string): string {
    if (secretLoading[name]) return "Loading...";
    if (secretErrors[name]) return "Load failed";
    if (!secretVisible[name]) return "••••••••";
    const value = secretValues[name] ?? "";
    const valueType = secretValueTypes[name] ?? "string";
    if (valueType === "binary") return value ? `${value} (base64)` : "(empty binary)";
    return value || "(empty)";
  }

  function secretFormattedValue(
    name: string,
  ): { formatted: string; formattedHtml: string } | null {
    const valueType = secretValueTypes[name] ?? "string";
    if (valueType !== "string") return null;
    return formatJSONForViewer(secretValues[name] ?? "");
  }

  async function copyToClipboard(text: string, field: string) {
    try {
      await navigator.clipboard.writeText(text);
      copiedField = field;
      setTimeout(() => { copiedField = null; }, 1400);
    } catch {}
  }
</script>

<div class="flex min-h-full flex-col gap-4">
  <SectionHeader
    title="Secrets Manager"
    description="Inventory, versions and guarded value inspection."
    icon={KeyIcon}
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      <div class="flex flex-wrap items-center gap-4 text-xs font-mono text-muted-foreground">
        <span class="inline-flex items-center gap-1.5">
          <span class="font-mono text-foreground">{secrets.length}</span>
          <span class="text-muted-foreground/70">visible</span>
        </span>
        <span class="inline-flex items-center gap-1.5">
          <span class="font-mono text-foreground">{revealedCount}</span>
          <span class="text-muted-foreground/70">revealed</span>
        </span>
        {#if failedLoads > 0}
          <span class="inline-flex items-center gap-1.5 text-destructive">
            <span class="font-mono">{failedLoads}</span>
            <span>load errors</span>
          </span>
        {/if}
      </div>
    {/snippet}
  </SectionHeader>

  <div class="overflow-hidden rounded-lg border border-border/70 bg-background/50">
    <div class="relative min-h-0" style="height: calc(100vh - 10rem);">

      {#if dashboard.loading && !dashboard.data}
        <div class="space-y-2 p-3">
          {#each Array(6) as _, index (index)}
            <Skeleton class="h-11 w-full" />
          {/each}
        </div>
      {:else if secrets.length === 0}
        <div class="flex h-full min-h-[18rem] items-center justify-center">
          <EmptyState message="No secrets created yet." icon={KeyIcon} />
        </div>
      {:else}
        <div class="h-full overflow-auto">
          <Table class="table-fixed">
            <TableHeader class="sticky top-0 z-10 bg-background/95 backdrop-blur [&_th]:bg-background/95">
              <TableRow class="hover:bg-transparent">
                <TableHead class="w-[28%]">Name</TableHead>
                <TableHead class="w-[20%]">Description</TableHead>
                <TableHead class="w-[52%]">Value</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each secrets as secret}
                <TableRow
                  class="cursor-pointer {secret.name === selectedSecretName ? 'bg-muted/50' : ''}"
                  role="button"
                  tabindex={0}
                  onclick={() => selectSecret(secret.name)}
                  onkeydown={(e: KeyboardEvent) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      selectSecret(secret.name);
                    }
                  }}
                >
                  <TableCell class="align-top overflow-visible">
                    <div class="flex min-w-0 flex-col gap-0.5">
                      <span class="truncate font-medium text-foreground text-xs">{secret.name}</span>
                      <span class="block min-w-0 truncate font-mono text-[11px] text-muted-foreground/60">{secret.arn}</span>
                    </div>
                  </TableCell>
                  <TableCell class="text-xs text-muted-foreground whitespace-normal break-words align-top">
                    {secret.description || "--"}
                  </TableCell>
                  <TableCell class="!whitespace-normal align-top" onclick={(e: MouseEvent) => e.stopPropagation()}>
                    <div class="flex items-start gap-2">
                      <div class="min-w-0 flex-1">
                        {#if !secretVisible[secret.name] || secretLoading[secret.name] || secretErrors[secret.name]}
                          <span
                            class={`break-all font-mono text-xs ${secretErrors[secret.name] ? "text-destructive" : "text-muted-foreground/70"}`}
                          >
                            {renderSecretValue(secret.name)}
                          </span>
                        {:else if secretFormattedValue(secret.name)}
                          <FormattedMessageViewer
                            raw={secretValues[secret.name] ?? ""}
                            formatted={secretFormattedValue(secret.name)?.formatted}
                            formattedHtml={secretFormattedValue(secret.name)?.formattedHtml}
                            formattedLabel="JSON"
                            rawLabel="Raw Value"
                            formattedOpenByDefault={true}
                            rawOpenByDefault={false}
                            formattedContentClass="text-[11px] text-muted-foreground"
                            rawContentClass="text-[11px] text-muted-foreground"
                            formattedMaxHeightClass="max-h-52"
                            rawMaxHeightClass="max-h-40"
                          />
                        {:else}
                          <div class="max-h-40 overflow-y-auto rounded">
                            <span class="break-all font-mono text-xs text-muted-foreground/70">
                              {renderSecretValue(secret.name)}
                            </span>
                          </div>
                        {/if}
                      </div>
                      <button
                        type="button"
                        class="shrink-0 rounded-md border border-border p-1 text-muted-foreground transition-colors hover:bg-background-subtle hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
                        onclick={() => void toggleSecretValue(secret.name)}
                        disabled={secretLoading[secret.name]}
                        title={secretVisible[secret.name] ? "Hide secret value" : "View secret value"}
                        aria-label={secretVisible[secret.name]
                          ? `Hide secret value for ${secret.name}`
                          : `View secret value for ${secret.name}`}
                      >
                        {#if secretVisible[secret.name]}
                          <EyeSlashIcon size={14} />
                        {:else}
                          <EyeIcon size={14} />
                        {/if}
                      </button>
                    </div>
                  </TableCell>
                </TableRow>
              {/each}
            </TableBody>
          </Table>
        </div>
      {/if}

      <DetailPanel
        open={selectedSecret !== null}
        onclose={() => (selectedSecretName = null)}
        title={selectedSecret?.name ?? ""}
        subtitle="Secret detail"
      >
        {#if selectedSecret}
          {@const name = selectedSecret.name}

          <!-- Identity -->
          <div>
            <p class="mb-2.5 text-[10px] uppercase tracking-[0.24em] text-muted-foreground/55">Identity</p>
            <div class="rounded-md border border-border overflow-hidden divide-y divide-border/60">
              <div class="flex items-start gap-4 px-3 py-2.5">
                <span class="w-20 shrink-0 pt-px text-[10px] uppercase tracking-wider text-muted-foreground/60">Name</span>
                <div class="flex min-w-0 flex-1 items-center gap-2">
                  <span class="min-w-0 break-all font-mono text-[12px] leading-snug text-foreground">{name}</span>
                  <button
                    type="button"
                    class="shrink-0 rounded p-0.5 text-muted-foreground/40 transition-colors hover:text-foreground"
                    onclick={() => void copyToClipboard(name, "name")}
                    title={copiedField === "name" ? "Copied" : "Copy name"}
                  >
                    {#if copiedField === "name"}
                      <CheckIcon size={11} />
                    {:else}
                      <CopyIcon size={11} />
                    {/if}
                  </button>
                </div>
              </div>
              <div class="flex items-start gap-4 px-3 py-2.5">
                <span class="w-20 shrink-0 pt-px text-[10px] uppercase tracking-wider text-muted-foreground/60">ARN</span>
                <div class="flex min-w-0 flex-1 items-start gap-2">
                  <span class="min-w-0 break-all font-mono text-[11px] leading-snug text-muted-foreground">{selectedSecret.arn}</span>
                  <button
                    type="button"
                    class="mt-px shrink-0 rounded p-0.5 text-muted-foreground/40 transition-colors hover:text-foreground"
                    onclick={() => void copyToClipboard(selectedSecret.arn, "arn")}
                    title={copiedField === "arn" ? "Copied" : "Copy ARN"}
                  >
                    {#if copiedField === "arn"}
                      <CheckIcon size={11} />
                    {:else}
                      <CopyIcon size={11} />
                    {/if}
                  </button>
                </div>
              </div>
              {#if selectedSecret.description}
                <div class="flex items-start gap-4 px-3 py-2.5">
                  <span class="w-20 shrink-0 pt-px text-[10px] uppercase tracking-wider text-muted-foreground/60">Desc</span>
                  <span class="break-words text-[12px] leading-snug text-muted-foreground">{selectedSecret.description}</span>
                </div>
              {/if}
            </div>
          </div>

          <!-- Value -->
          <div>
            <p class="mb-2.5 text-[10px] uppercase tracking-[0.24em] text-muted-foreground/55">Value</p>
            <div class="rounded-md border border-border overflow-hidden">
              <div class="flex items-center justify-between gap-2 border-b border-border/60 px-3 py-2">
                <span class="text-[10px] uppercase tracking-wider text-muted-foreground/60">
                  {secretValueTypes[name] ?? "string"}
                </span>
                <div class="flex items-center gap-2">
                  {#if secretVisible[name] && secretValues[name]}
                    <button
                      type="button"
                      class="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-muted-foreground/60 transition-colors hover:bg-muted hover:text-foreground"
                      onclick={() => void copyToClipboard(secretValues[name], "value")}
                    >
                      {#if copiedField === "value"}
                        <CheckIcon size={11} />
                        <span>Copied</span>
                      {:else}
                        <CopyIcon size={11} />
                        <span>Copy</span>
                      {/if}
                    </button>
                  {/if}
                  <button
                    type="button"
                    class="flex items-center gap-1.5 rounded border border-border px-2 py-1 text-[11px] text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
                    onclick={() => void toggleSecretValue(name)}
                    disabled={secretLoading[name]}
                  >
                    {#if secretVisible[name]}
                      <EyeSlashIcon size={12} />
                      <span>Hide</span>
                    {:else}
                      <EyeIcon size={12} />
                      <span>Reveal</span>
                    {/if}
                  </button>
                </div>
              </div>
              <div class="px-3 py-2.5">
                {#if !secretVisible[name] || secretLoading[name]}
                  <span class="font-mono text-sm text-muted-foreground/50">
                    {renderSecretValue(name)}
                  </span>
                {:else if secretErrors[name]}
                  <span class="text-xs text-destructive">{secretErrors[name]}</span>
                {:else if secretFormattedValue(name)}
                  <FormattedMessageViewer
                    raw={secretValues[name] ?? ""}
                    formatted={secretFormattedValue(name)?.formatted}
                    formattedHtml={secretFormattedValue(name)?.formattedHtml}
                    formattedLabel="JSON"
                    rawLabel="Raw Value"
                    formattedOpenByDefault={true}
                    rawOpenByDefault={false}
                    formattedContentClass="text-[11px] text-foreground"
                    rawContentClass="text-[11px] text-muted-foreground"
                    formattedMaxHeightClass="max-h-[18rem]"
                    rawMaxHeightClass="max-h-[14rem]"
                  />
                {:else}
                  <div class="max-h-[18rem] overflow-y-auto">
                    <span class="break-all font-mono text-[12px] leading-relaxed text-muted-foreground">
                      {renderSecretValue(name)}
                    </span>
                  </div>
                {/if}
              </div>
            </div>
          </div>

          <!-- Metadata -->
          <div>
            <p class="mb-2.5 text-[10px] uppercase tracking-[0.24em] text-muted-foreground/55">Metadata</p>
            <div class="rounded-md border border-border overflow-hidden divide-y divide-border/60">
              <div class="flex items-start gap-4 px-3 py-2.5">
                <span class="w-20 shrink-0 pt-px text-[10px] uppercase tracking-wider text-muted-foreground/60">Version</span>
                <span class="break-all font-mono text-[12px] leading-snug text-muted-foreground">{selectedSecret.versionId}</span>
              </div>
              <div class="flex items-start gap-4 px-3 py-2.5">
                <span class="w-20 shrink-0 pt-px text-[10px] uppercase tracking-wider text-muted-foreground/60">Tags</span>
                <span class="font-mono text-[12px] leading-snug text-muted-foreground">
                  {selectedSecret.tagCount}
                  {#if selectedSecret.tags && Object.keys(selectedSecret.tags).length > 0}
                    <span class="ml-2 text-muted-foreground/60">
                      ({Object.entries(selectedSecret.tags).map(([k, v]) => `${k}=${v}`).join(", ")})
                    </span>
                  {/if}
                </span>
              </div>
              <div class="flex items-start gap-4 px-3 py-2.5">
                <span class="w-20 shrink-0 pt-px text-[10px] uppercase tracking-wider text-muted-foreground/60">Created</span>
                <span class="font-mono text-[12px] leading-snug text-muted-foreground">{formatDate(selectedSecret.createdDate)}</span>
              </div>
              <div class="flex items-start gap-4 px-3 py-2.5">
                <span class="w-20 shrink-0 pt-px text-[10px] uppercase tracking-wider text-muted-foreground/60">Changed</span>
                <span class="font-mono text-[12px] leading-snug text-muted-foreground">{formatDate(selectedSecret.lastChangedDate)}</span>
              </div>
            </div>
          </div>
        {/if}
      </DetailPanel>

    </div>
  </div>
</div>
