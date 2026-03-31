<script lang="ts">
  import {
    EyeIcon,
    EyeSlashIcon,
    KeyIcon,
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
  import SectionHeader from "./section-header.svelte";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
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
        [name]:
          error instanceof Error ? error.message : "Failed to load secret value",
      };
    } finally {
      secretLoading = { ...secretLoading, [name]: false };
    }
  }

  function renderSecretValue(name: string): string {
    if (secretLoading[name]) return "Loading...";
    if (secretErrors[name]) return "Load failed";
    if (!secretVisible[name]) return "********";

    const value = secretValues[name] ?? "";
    const valueType = secretValueTypes[name] ?? "string";
    if (valueType === "binary") {
      return value ? `${value} (base64)` : "(empty binary)";
    }
    return value || "(empty)";
  }

  function secretFormattedValue(
    name: string,
  ): { formatted: string; formattedHtml: string } | null {
    const valueType = secretValueTypes[name] ?? "string";
    if (valueType !== "string") return null;
    return formatJSONForViewer(secretValues[name] ?? "");
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
    {#snippet stats()}
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
    {/snippet}
  </SectionHeader>

  <div class="min-h-0 flex-1 overflow-hidden rounded-lg border border-border/70 bg-background/60">
    {#if dashboard.loading && !dashboard.data}
      <div class="space-y-2 p-3">
        {#each Array(6) as _, index (index)}
          <Skeleton class="h-11 w-full" />
        {/each}
      </div>
    {:else if secrets.length === 0}
      <div class="flex h-full min-h-[18rem] items-center justify-center">
        <EmptyState
          message="No secrets created yet."
          icon={KeyIcon}
        />
      </div>
    {:else}
      <div class="h-full overflow-auto">
        <Table>
          <TableHeader class="sticky top-0 z-10 bg-background/95 backdrop-blur [&_th]:bg-background/95">
            <TableRow class="hover:bg-transparent">
              <TableHead>Name</TableHead>
              <TableHead>Description</TableHead>
              <TableHead>Value</TableHead>
              <TableHead>Version</TableHead>
              <TableHead>Tags</TableHead>
              <TableHead>Created</TableHead>
              <TableHead>Changed</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each secrets as secret}
              <TableRow>
                <TableCell><ArnCell name={secret.name} arn={secret.arn} /></TableCell>
                <TableCell class="text-xs text-muted-foreground">
                  {secret.description || "--"}
                </TableCell>
                <TableCell class="!whitespace-normal align-top">
                  <div class="flex items-start gap-2">
                    <div class="min-w-0 flex-1">
                      {#if !secretVisible[secret.name] || secretLoading[secret.name] || secretErrors[secret.name]}
                        <span
                          class={`break-all font-mono text-xs ${secretErrors[secret.name] ? "text-destructive" : "text-muted-foreground/70"}`}
                          title={secretVisible[secret.name]
                            ? renderSecretValue(secret.name)
                            : "Hidden"}
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
                        <span
                          class="break-all font-mono text-xs text-muted-foreground/70"
                          title={renderSecretValue(secret.name)}
                        >
                          {renderSecretValue(secret.name)}
                        </span>
                      {/if}
                    </div>
                    <button
                      type="button"
                      class="shrink-0 rounded-md border border-border p-1 text-muted-foreground transition-colors hover:bg-background-subtle hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
                      onclick={() => void toggleSecretValue(secret.name)}
                      disabled={secretLoading[secret.name]}
                      title={secretVisible[secret.name]
                        ? "Hide secret value"
                        : "View secret value"}
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
                <TableCell class="font-mono text-xs text-muted-foreground/70">
                  {secret.versionId}
                </TableCell>
                <TableCell class="text-muted-foreground">{secret.tagCount}</TableCell>
                <TableCell class="text-xs text-muted-foreground/70">
                  {formatDate(secret.createdDate)}
                </TableCell>
                <TableCell class="text-xs text-muted-foreground/70">
                  {formatDate(secret.lastChangedDate)}
                </TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
      </div>
    {/if}
  </div>
</div>
