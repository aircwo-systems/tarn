<script lang="ts">
  import { DatabaseIcon } from "phosphor-svelte";
  import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from "$lib/components/ui/table";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import EmptyState from "$lib/components/common/empty-state.svelte";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import SectionHeader from "./section-header.svelte";
  import { getDashboard } from "$lib/state.svelte";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const tables = $derived(dashboard.data?.dynamodbTables ?? []);
  const streams = $derived(dashboard.data?.dynamodbStreams ?? []);
  const config = $derived(dashboard.data?.config ?? null);
  const streamEnabledCount = $derived(tables.filter((table) => table.streamEnabled).length);

  let selectedTableName = $state("");

  $effect(() => {
    if (!selectedTableName && tables.length > 0) {
      selectedTableName = tables[0].name;
      return;
    }
    if (selectedTableName && !tables.some((table) => table.name === selectedTableName)) {
      selectedTableName = tables[0]?.name ?? "";
    }
  });

  const selectedTable = $derived(
    tables.find((table) => table.name === selectedTableName) ?? null,
  );
  const selectedStreams = $derived(
    selectedTableName
      ? streams.filter((stream) => stream.tableName === selectedTableName)
      : streams,
  );

  const connectionDetails = $derived(parseEndpoint(config?.endpoint));

  function formatDate(value?: string | number): string {
    if (!value) return "--";
    if (typeof value === "number") {
      return new Date(value * 1000).toLocaleString();
    }
    const parsed = new Date(value);
    return Number.isNaN(parsed.valueOf()) ? String(value) : parsed.toLocaleString();
  }

  function parseEndpoint(endpoint?: string) {
    if (!endpoint) {
      return {
        endpoint: "--",
        host: "--",
        port: "--",
      };
    }

    try {
      const parsed = new URL(endpoint);
      return {
        endpoint,
        host: parsed.hostname || "--",
        port: parsed.port || (parsed.protocol === "https:" ? "443" : "80"),
      };
    } catch {
      return {
        endpoint,
        host: "--",
        port: "--",
      };
    }
  }

</script>

<div class="flex min-h-full flex-col gap-4">
  <SectionHeader
    title="DynamoDB"
    description="Tables, stream state, and local connection details."
    icon={DatabaseIcon}
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      <div class="flex flex-wrap items-center gap-4 text-xs font-mono text-muted-foreground">
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{tables.length}</span>
        <span class="text-muted-foreground/70">tables</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{streams.length}</span>
        <span class="text-muted-foreground/70">streams</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{streamEnabledCount}</span>
        <span class="text-muted-foreground/70">stream-enabled</span>
      </span>
      </div>
    {/snippet}
  </SectionHeader>

  {#if dashboard.loading && !dashboard.data}
    <div
      class="min-h-0 flex-1 overflow-hidden rounded-lg border border-border/70 bg-background/50"
      style="height: calc(100vh - 10rem);"
    >
      <div class="grid gap-0 xl:grid-cols-[minmax(0,1.2fr)_minmax(20rem,0.8fr)]">
        <div class="space-y-2 border-b border-border/70 p-4 xl:border-b-0 xl:border-r">
          {#each Array(7) as _, index (index)}
            <Skeleton class="h-12 w-full" />
          {/each}
        </div>
        <div class="space-y-3 p-4">
          {#each Array(8) as _, index (index)}
            <Skeleton class="h-11 w-full" />
          {/each}
        </div>
      </div>
    </div>
  {:else if tables.length === 0 && streams.length === 0}
    <div
      class="flex min-h-0 flex-1 items-center justify-center rounded-lg border border-border/70 bg-background/50"
      style="height: calc(100vh - 10rem);"
    >
      <EmptyState icon={DatabaseIcon} message="No DynamoDB tables created yet." />
    </div>
  {:else}
    <div
      class="min-h-0 flex-1 overflow-hidden rounded-lg border border-border/70 bg-background/50"
      style="height: calc(100vh - 10rem);"
    >
      <div class="grid h-full gap-0 xl:grid-cols-[minmax(0,1.2fr)_minmax(20rem,0.8fr)]">
        <div class="flex min-h-0 flex-col xl:border-r xl:border-border/70">
          <div class="flex flex-wrap items-end justify-between gap-4 border-b border-border/70 px-4 py-4">
            <div>
              <p class="text-[10px] uppercase tracking-[0.24em] text-muted-foreground/55">Tables</p>
              <p class="mt-1 text-sm text-foreground">Primary keys, index layout, and stream state.</p>
            </div>
            <div class="flex flex-wrap gap-6 text-xs">
              <div>
                <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Account</p>
                <p class="mt-1 font-mono text-foreground">{config?.accountId ?? "--"}</p>
              </div>
              <div>
                <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Region</p>
                <p class="mt-1 font-mono text-foreground">{config?.region ?? "--"}</p>
              </div>
              <div>
                <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Endpoint</p>
                <p class="mt-1 font-mono text-foreground">{connectionDetails.host}:{connectionDetails.port}</p>
              </div>
            </div>
          </div>

          <div class="min-h-0 flex-1 overflow-auto">
            <Table>
              <TableHeader class="sticky top-0 z-10 bg-background/95 backdrop-blur [&_th]:bg-background/95">
                <TableRow class="hover:bg-transparent">
                  <TableHead>Table</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Keys</TableHead>
                  <TableHead>Indexes</TableHead>
                  <TableHead>Stream</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {#each tables as table}
                  <TableRow
                    class={`cursor-pointer ${table.name === selectedTableName ? "bg-muted/50" : ""}`}
                    role="button"
                    tabindex={0}
                    onclick={() => (selectedTableName = table.name)}
                    onkeydown={(event: KeyboardEvent) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        selectedTableName = table.name;
                      }
                    }}
                  >
                    <TableCell>
                      <ArnCell name={table.name} arn={table.arn} />
                    </TableCell>
                    <TableCell class="font-mono text-xs text-muted-foreground">{table.status}</TableCell>
                    <TableCell class="font-mono text-xs text-muted-foreground">{table.keySchema}</TableCell>
                    <TableCell class="font-mono text-xs text-muted-foreground">
                      {table.localIndexes}/{table.globalIndexes}
                    </TableCell>
                    <TableCell class="font-mono text-xs">
                      <span class={table.streamEnabled ? "text-[var(--topology-dynamodb)]" : "text-muted-foreground/50"}>
                        {table.streamEnabled ? table.streamViewType || "enabled" : "disabled"}
                      </span>
                    </TableCell>
                  </TableRow>
                {/each}
              </TableBody>
            </Table>
          </div>
        </div>

        <div class="flex min-h-0 flex-col bg-background/35">
          <div class="border-b border-border/70 px-4 py-4">
            <p class="text-[10px] uppercase tracking-[0.24em] text-muted-foreground/55">Selected Table</p>
            {#if selectedTable}
              <div class="mt-2 space-y-3">
                <div>
                  <p class="font-mono text-sm text-foreground">{selectedTable.name}</p>
                  <p class="mt-1 text-xs text-muted-foreground/75">{selectedTable.arn}</p>
                </div>
                <div class="grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
                  <div>
                    <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Status</p>
                    <p class="mt-1 font-mono text-foreground">{selectedTable.status}</p>
                  </div>
                  <div>
                    <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Created</p>
                    <p class="mt-1 font-mono text-foreground">{formatDate(selectedTable.createdDate)}</p>
                  </div>
                  <div>
                    <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Items</p>
                    <p class="mt-1 font-mono text-foreground">{selectedTable.itemCount}</p>
                  </div>
                  <div>
                    <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Keys</p>
                    <p class="mt-1 font-mono text-foreground">{selectedTable.keySchema}</p>
                  </div>
                  <div>
                    <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Indexes</p>
                    <p class="mt-1 font-mono text-foreground">{selectedTable.localIndexes} LSI / {selectedTable.globalIndexes} GSI</p>
                  </div>
                </div>
              </div>
            {:else}
              <div class="pt-4">
                <EmptyState icon={DatabaseIcon} message="No table selected." />
              </div>
            {/if}
          </div>

          <div class="border-b border-border/70 px-4 py-4">
            <div class="flex items-center justify-between gap-4">
              <div>
                <p class="text-[10px] uppercase tracking-[0.24em] text-muted-foreground/55">Streams</p>
                <p class="mt-1 text-[11px] text-muted-foreground/70">
                  {selectedTableName ? `Records emitted by ${selectedTableName}.` : "All active DynamoDB streams."}
                </p>
              </div>
              <p class="font-mono text-xs text-foreground">{selectedStreams.length}</p>
            </div>

            {#if selectedStreams.length === 0}
              <div class="pt-4">
                <EmptyState icon={DatabaseIcon} message="No streams on the selected table." />
              </div>
            {:else}
              <div class="mt-4 space-y-3">
                {#each selectedStreams as stream}
                  <div class="border-t border-border/60 pt-3 first:border-t-0 first:pt-0">
                    <div class="flex items-start justify-between gap-3">
                      <div>
                        <p class="font-mono text-xs text-foreground">{stream.streamViewType}</p>
                        <p class="mt-1 text-[11px] text-muted-foreground/70">{stream.streamArn}</p>
                      </div>
                      <div class="text-right text-[11px]">
                        <p class="font-mono text-foreground">{stream.streamStatus}</p>
                        <p class="mt-1 text-muted-foreground/70">{stream.shardCount} shards</p>
                      </div>
                    </div>
                    <div class="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-muted-foreground/70">
                      <span>created (local): <span class="font-mono text-foreground">{formatDate(stream.createdDate)}</span></span>
                    </div>
                  </div>
                {/each}
              </div>
            {/if}
          </div>

          <div class="overflow-y-auto px-4 py-4 text-xs">
            <p class="text-[10px] uppercase tracking-[0.24em] text-muted-foreground/55">Connect Externally</p>
            <p class="mt-2 text-muted-foreground/75">
              Use an AWS SDK, the AWS CLI, or any DynamoDB-compatible client with an endpoint override pointed at your Tarn instance.
            </p>
            <dl class="mt-4 grid grid-cols-[7rem_minmax(0,1fr)] gap-x-3 gap-y-2">
              <dt class="text-muted-foreground/60">Endpoint</dt>
              <dd class="break-all font-mono text-foreground">{connectionDetails.endpoint}</dd>
              <dt class="text-muted-foreground/60">Host</dt>
              <dd class="font-mono text-foreground">{connectionDetails.host}</dd>
              <dt class="text-muted-foreground/60">Port</dt>
              <dd class="font-mono text-foreground">{connectionDetails.port}</dd>
              <dt class="text-muted-foreground/60">Region</dt>
              <dd class="font-mono text-foreground">{config?.region ?? "--"}</dd>
              <dt class="text-muted-foreground/60">Access key</dt>
              <dd class="font-mono text-foreground">test</dd>
              <dt class="text-muted-foreground/60">Secret key</dt>
              <dd class="font-mono text-foreground">test</dd>
            </dl>
            <div class="mt-4 border-t border-border/60 pt-4">
              <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Client setup</p>
              <p class="mt-2 text-muted-foreground/75">
                Configure your client with the endpoint override, region, and local credentials above. Tarn speaks the standard DynamoDB JSON API used by AWS SDKs and CLI tooling.
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  {/if}
</div>
