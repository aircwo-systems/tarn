<script lang="ts">
  import { BellIcon, CheckIcon, CopySimpleIcon } from "phosphor-svelte";
  import { PaneGroup, Pane, Handle } from "$lib/components/ui/resizable";
  import {
    Table,
    TableHeader,
    TableBody,
    TableRow,
    TableHead,
    TableCell,
  } from "$lib/components/ui/table";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import EmptyState from "$lib/components/common/empty-state.svelte";
  import SectionHeader from "./section-header.svelte";
  import {
    Tabs,
    TabsList,
    TabsTrigger,
    TabsContent,
  } from "$lib/components/ui/tabs";
  import {
    getDashboard,
    getDashboardFilters,
    matchesTagFilter,
  } from "$lib/state.svelte";
  import { formatJSONForViewer } from "$lib/json-format";
  import { formatUnixSeconds } from "$lib/utils";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const filters = getDashboardFilters();

  const topics = $derived(
    (dashboard.data?.topics ?? []).filter((topic) =>
      matchesTagFilter(topic.tags, filters.tagFilter),
    ),
  );

  const topicNames = $derived(new Set(topics.map((topic) => topic.name)));
  const subscriptions = $derived(
    (dashboard.data?.subscriptions ?? []).filter((sub) => {
      if (!filters.tagFilter.trim()) return true;
      return topicNames.has(sub.topicName);
    }),
  );
  const fifoTopics = $derived(topics.filter((topic) => topic.fifo).length);
  const lambdaSubscriptions = $derived(
    subscriptions.filter((sub) => sub.protocol.toLowerCase() === "lambda").length,
  );
  const sqsSubscriptions = $derived(
    subscriptions.filter((sub) => sub.protocol.toLowerCase() === "sqs").length,
  );

  let activeTab = $state<"topics" | "subscriptions">("topics");
  let selectedTopicName = $state("");
  let selectedSubscriptionArn = $state("");
  let selectedFilterView = $state<"formatted" | "raw">("formatted");
  let copiedFilter = $state(false);

  $effect(() => {
    if (!selectedTopicName && topics.length > 0) {
      selectedTopicName = topics[0].name;
      return;
    }
    if (selectedTopicName && !topics.some((topic) => topic.name === selectedTopicName)) {
      selectedTopicName = topics[0]?.name ?? "";
    }
  });

  $effect(() => {
    if (!selectedSubscriptionArn && subscriptions.length > 0) {
      selectedSubscriptionArn = subscriptions[0].subscriptionArn;
      return;
    }
    if (
      selectedSubscriptionArn &&
      !subscriptions.some((sub) => sub.subscriptionArn === selectedSubscriptionArn)
    ) {
      selectedSubscriptionArn = subscriptions[0]?.subscriptionArn ?? "";
    }
  });

  const selectedTopic = $derived(
    topics.find((topic) => topic.name === selectedTopicName) ?? null,
  );
  const selectedSubscription = $derived(
    subscriptions.find((sub) => sub.subscriptionArn === selectedSubscriptionArn) ?? null,
  );
  const selectedFilter = $derived(
    selectedSubscription?.filterPolicy
      ? formatJSONForViewer(selectedSubscription.filterPolicy)
      : null,
  );

  function protocolColor(protocol: string): string {
    switch (protocol.toLowerCase()) {
      case "sqs":
        return "text-amber";
      case "lambda":
        return "text-primary";
      default:
        return "text-muted-foreground";
    }
  }

  function selectTopic(name: string) {
    selectedTopicName = name;
    activeTab = "topics";
  }

  function selectSubscription(subscriptionArn: string) {
    selectedSubscriptionArn = subscriptionArn;
    activeTab = "subscriptions";
    selectedFilterView = "formatted";
  }

  async function copySelectedFilter() {
    if (!selectedSubscription?.filterPolicy) return;

    const content =
      selectedFilterView === "formatted"
        ? (selectedFilter?.formatted ?? selectedSubscription.filterPolicy)
        : selectedSubscription.filterPolicy;

    try {
      await navigator.clipboard.writeText(content);
      copiedFilter = true;
      setTimeout(() => {
        copiedFilter = false;
      }, 1400);
    } catch (error) {
      console.error("Failed to copy SNS filter", error);
    }
  }

  const lineTabTriggerClass =
    "rounded-none border-0 bg-transparent px-0 shadow-none data-active:border-transparent dark:data-active:border-transparent data-active:bg-transparent dark:data-active:bg-transparent";
</script>

<div class="flex min-h-full flex-col gap-4">
  <SectionHeader
    title="SNS topics"
    description="Topic fan-out, subscriber mix and delivery filter rules."
    icon={BellIcon}
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      <div class="flex flex-wrap items-center gap-4 text-xs font-mono text-muted-foreground">
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{topics.length}</span>
        <span class="text-muted-foreground/70">topics</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{subscriptions.length}</span>
        <span class="text-muted-foreground/70">subscriptions</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{fifoTopics}</span>
        <span class="text-muted-foreground/70">fifo</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{lambdaSubscriptions}</span>
        <span class="text-muted-foreground/70">lambda targets</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{sqsSubscriptions}</span>
        <span class="text-muted-foreground/70">sqs targets</span>
      </span>
      </div>
    {/snippet}
  </SectionHeader>

  {#if dashboard.loading && !dashboard.data}
    <div
      class="min-h-0 flex-1 overflow-hidden rounded-lg border border-border/70 bg-background/50"
      style="height: calc(100vh - 10rem);"
    >
      <div class="grid h-full gap-0 xl:grid-cols-[minmax(0,1.2fr)_minmax(20rem,0.8fr)]">
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
  {:else if topics.length === 0 && subscriptions.length === 0}
    <div
      class="flex min-h-0 flex-1 items-center justify-center rounded-lg border border-border/70 bg-background/50"
      style="height: calc(100vh - 10rem);"
    >
      <EmptyState icon={BellIcon} message="No SNS topics or subscriptions created yet." />
    </div>
  {:else}
    <PaneGroup direction="horizontal" class="min-h-0 flex-1 rounded-lg border border-border/70" style="height: calc(100vh - 10rem);">
      <Pane defaultSize={62} minSize={35} class="flex min-h-0 flex-col overflow-hidden bg-background/50">
          <Tabs bind:value={activeTab} class="flex min-h-0 flex-1 flex-col gap-0">
            <div class="flex flex-wrap items-end justify-between gap-4 border-b border-border/70 px-4 py-4">
              <div>
                <p class="text-[10px] uppercase tracking-[0.24em] text-muted-foreground/55">SNS</p>
                <p class="mt-1 text-sm text-foreground">Browse topics and subscriptions, then inspect details on the right.</p>
              </div>
              <TabsList variant="line" class="shrink-0 gap-3">
                <TabsTrigger value="topics" class={lineTabTriggerClass}>Topics</TabsTrigger>
                <TabsTrigger value="subscriptions" class={lineTabTriggerClass}>Subscriptions</TabsTrigger>
              </TabsList>
            </div>

            <TabsContent value="topics" class="min-h-0 flex-1">
              {#if topics.length === 0}
                <div class="flex min-h-[18rem] items-center justify-center">
                  <EmptyState icon={BellIcon} message="No SNS topics created yet." />
                </div>
              {:else}
                <div class="min-h-0 flex-1 overflow-auto">
                  <Table>
                    <TableHeader class="sticky top-0 z-10 bg-background/95 backdrop-blur [&_th]:bg-background/95">
                      <TableRow class="hover:bg-transparent">
                        <TableHead>Topic</TableHead>
                        <TableHead>Type</TableHead>
                        <TableHead>Subscriptions</TableHead>
                        <TableHead>Created</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {#each topics as topic}
                        <TableRow
                          class={`cursor-pointer ${topic.name === selectedTopicName ? "bg-muted/50" : ""}`}
                          role="button"
                          tabindex={0}
                          onclick={() => selectTopic(topic.name)}
                          onkeydown={(event: KeyboardEvent) => {
                            if (event.key === "Enter" || event.key === " ") {
                              event.preventDefault();
                              selectTopic(topic.name);
                            }
                          }}
                        >
                          <TableCell>
                            <ArnCell name={topic.name} arn={topic.arn} />
                          </TableCell>
                          <TableCell class={`font-mono text-xs ${topic.fifo ? "text-amber" : "text-muted-foreground"}`}>
                            {topic.fifo ? "FIFO" : "Standard"}
                          </TableCell>
                          <TableCell class="font-mono text-xs text-muted-foreground">
                            {topic.subscriptions}
                          </TableCell>
                          <TableCell class="text-xs text-muted-foreground/70">
                            {formatUnixSeconds(topic.createdTimestamp)}
                          </TableCell>
                        </TableRow>
                      {/each}
                    </TableBody>
                  </Table>
                </div>
              {/if}
            </TabsContent>

            <TabsContent value="subscriptions" class="min-h-0 flex-1">
              {#if subscriptions.length === 0}
                <div class="flex min-h-[18rem] items-center justify-center">
                  <EmptyState icon={BellIcon} message="No subscriptions configured yet." />
                </div>
              {:else}
                <div class="min-h-0 flex-1 overflow-auto">
                  <Table>
                    <TableHeader class="sticky top-0 z-10 bg-background/95 backdrop-blur [&_th]:bg-background/95">
                      <TableRow class="hover:bg-transparent">
                        <TableHead>Topic</TableHead>
                        <TableHead>Protocol</TableHead>
                        <TableHead>Endpoint</TableHead>
                        <TableHead>Delivery</TableHead>
                        <TableHead>Filter</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {#each subscriptions as sub}
                        <TableRow
                          class={`cursor-pointer ${sub.subscriptionArn === selectedSubscriptionArn ? "bg-muted/50" : ""}`}
                          role="button"
                          tabindex={0}
                          onclick={() => selectSubscription(sub.subscriptionArn)}
                          onkeydown={(event: KeyboardEvent) => {
                            if (event.key === "Enter" || event.key === " ") {
                              event.preventDefault();
                              selectSubscription(sub.subscriptionArn);
                            }
                          }}
                        >
                          <TableCell>
                            <ArnCell name={sub.topicName} arn={sub.topicArn} />
                          </TableCell>
                          <TableCell class={`font-mono text-xs ${protocolColor(sub.protocol)}`}>
                            {sub.protocol}
                          </TableCell>
                          <TableCell class="max-w-56 break-all font-mono text-xs text-muted-foreground">
                            {sub.endpoint}
                          </TableCell>
                          <TableCell class={`font-mono text-xs ${sub.rawMessageDelivery ? "text-primary" : "text-muted-foreground"}`}>
                            {sub.rawMessageDelivery ? "Raw" : "Envelope"}
                          </TableCell>
                          <TableCell class="font-mono text-xs text-muted-foreground/70">
                            {sub.filterPolicy ? "Configured" : "None"}
                          </TableCell>
                        </TableRow>
                      {/each}
                    </TableBody>
                  </Table>
                </div>
              {/if}
            </TabsContent>
          </Tabs>
      </Pane>
      <Handle />
      <Pane defaultSize={38} minSize={22} class="flex min-h-0 flex-col overflow-hidden bg-background/35">
          {#if activeTab === "topics"}
            <div class="border-b border-border/70 px-4 py-4">
              <p class="text-[10px] uppercase tracking-[0.24em] text-muted-foreground/55">Selected Topic</p>
              {#if selectedTopic}
                <div class="mt-2 space-y-3">
                  <div>
                    <p class="font-mono text-sm text-foreground">{selectedTopic.name}</p>
                    <p class="mt-1 text-xs text-muted-foreground/75">{selectedTopic.arn}</p>
                  </div>
                  <div class="grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
                    <div>
                      <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Type</p>
                      <p class={`mt-1 font-mono ${selectedTopic.fifo ? "text-amber" : "text-foreground"}`}>
                        {selectedTopic.fifo ? "FIFO" : "Standard"}
                      </p>
                    </div>
                    <div>
                      <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Created</p>
                      <p class="mt-1 font-mono text-foreground">{formatUnixSeconds(selectedTopic.createdTimestamp)}</p>
                    </div>
                    <div>
                      <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Subscriptions</p>
                      <p class="mt-1 font-mono text-foreground">{selectedTopic.subscriptions}</p>
                    </div>
                    <div>
                      <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Tags</p>
                      <p class="mt-1 font-mono text-foreground">{selectedTopic.tagCount}</p>
                    </div>
                  </div>
                </div>
              {:else}
                <div class="pt-4">
                  <EmptyState icon={BellIcon} message="No topic selected." />
                </div>
              {/if}
            </div>

            <div class="px-4 py-4">
              <p class="text-[10px] uppercase tracking-[0.24em] text-muted-foreground/55">Selection Notes</p>
              <div class="mt-3 space-y-2 text-xs text-muted-foreground/75">
                <p>Switch to the subscriptions tab to inspect endpoints, raw delivery settings, and filter policies.</p>
              </div>
            </div>
          {:else}
            <div class="border-b border-border/70 px-4 py-4">
              <p class="text-[10px] uppercase tracking-[0.24em] text-muted-foreground/55">Selected Subscription</p>
              {#if selectedSubscription}
                <div class="mt-2 space-y-3">
                  <div>
                    <p class="font-mono text-sm text-foreground">{selectedSubscription.topicName}</p>
                    <p class="mt-1 break-all text-xs text-muted-foreground/75">{selectedSubscription.subscriptionArn}</p>
                  </div>
                  <div class="grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
                    <div>
                      <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Protocol</p>
                      <p class={`mt-1 font-mono ${protocolColor(selectedSubscription.protocol)}`}>
                        {selectedSubscription.protocol}
                      </p>
                    </div>
                    <div>
                      <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Delivery</p>
                      <p class="mt-1 font-mono text-foreground">
                        {selectedSubscription.rawMessageDelivery ? "Raw" : "Envelope"}
                      </p>
                    </div>
                    <div class="col-span-2">
                      <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Endpoint</p>
                      <p class="mt-1 break-all font-mono text-foreground">{selectedSubscription.endpoint}</p>
                    </div>
                    <div>
                      <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Filter Scope</p>
                      <p class="mt-1 font-mono text-foreground">{selectedSubscription.filterPolicyScope ?? "--"}</p>
                    </div>
                    <div>
                      <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Filter</p>
                      <p class="mt-1 font-mono text-foreground">{selectedSubscription.filterPolicy ? "Configured" : "None"}</p>
                    </div>
                  </div>
                </div>
              {:else}
                <div class="pt-4">
                  <EmptyState icon={BellIcon} message="No subscription selected." />
                </div>
              {/if}
            </div>

            <div class="flex min-h-0 flex-1 flex-col px-4 py-4">
              <div class="flex items-center justify-between gap-4">
                <div>
                  <p class="text-[10px] uppercase tracking-[0.24em] text-muted-foreground/55">Filter Policy</p>
                  <p class="mt-1 text-[11px] text-muted-foreground/70">
                    {selectedSubscription?.filterPolicy
                      ? "Inspect the active subscription filter."
                      : "No filter policy on the selected subscription."}
                  </p>
                </div>
                {#if selectedSubscription?.filterPolicy}
                  <Tabs bind:value={selectedFilterView} class="gap-0">
                    <TabsList variant="line" class="shrink-0 gap-3">
                      <TabsTrigger value="formatted" class={lineTabTriggerClass}>Formatted</TabsTrigger>
                      <TabsTrigger value="raw" class={lineTabTriggerClass}>Raw</TabsTrigger>
                    </TabsList>
                  </Tabs>
                {/if}
              </div>

              {#if !selectedSubscription}
                <div class="pt-4">
                  <EmptyState icon={BellIcon} message="Select a subscription to inspect its filter." />
                </div>
              {:else if !selectedSubscription.filterPolicy}
                <div class="pt-4">
                  <EmptyState icon={BellIcon} message="No filter policy configured on this subscription." />
                </div>
              {:else}
                <div class="group relative mt-4 min-h-0 flex-1 overflow-hidden rounded-md border border-border">
                  <button
                    type="button"
                    class="absolute right-2 top-2 z-10 inline-flex h-8 items-center gap-1.5 rounded-md border border-border/80 bg-background/90 px-2 text-[11px] text-muted-foreground opacity-0 shadow-sm backdrop-blur-sm transition-all duration-200 hover:bg-background-subtle hover:text-foreground group-hover:opacity-100 focus-visible:opacity-100"
                    onclick={() => void copySelectedFilter()}
                    title={copiedFilter ? "Copied" : "Copy filter"}
                    aria-label={copiedFilter ? "Copied filter" : "Copy filter"}
                  >
                    <span class="relative flex h-3.5 w-3.5 items-center justify-center">
                      <span
                        class={`absolute transition-all duration-200 ${copiedFilter ? "scale-75 opacity-0" : "scale-100 opacity-100"}`}
                      >
                        <CopySimpleIcon size={13} />
                      </span>
                      <span
                        class={`absolute transition-all duration-200 ${copiedFilter ? "scale-100 opacity-100" : "scale-75 opacity-0"}`}
                      >
                        <CheckIcon size={13} />
                      </span>
                    </span>
                    <span>{copiedFilter ? "Copied" : "Copy"}</span>
                  </button>
                  {#if selectedFilterView === "formatted"}
                    {#if selectedFilter}
                      <pre
                        class="h-full overflow-y-auto bg-[var(--code-bg)] px-3 py-3 font-mono text-[11px] text-foreground leading-relaxed whitespace-pre-wrap break-all"
                        >{@html selectedFilter.formattedHtml}</pre
                      >
                    {:else}
                      <pre
                        class="h-full overflow-y-auto bg-[var(--code-bg)] px-3 py-3 font-mono text-[11px] text-muted-foreground leading-relaxed whitespace-pre-wrap break-all"
                        >{selectedSubscription.filterPolicy}</pre
                      >
                    {/if}
                  {:else}
                    <pre
                      class="h-full overflow-y-auto bg-[var(--code-bg)] px-3 py-3 font-mono text-[11px] text-muted-foreground leading-relaxed whitespace-pre-wrap break-all"
                      >{selectedSubscription.filterPolicy}</pre
                    >
                  {/if}
                </div>
              {/if}
            </div>
          {/if}
      </Pane>
    </PaneGroup>
  {/if}
</div>
