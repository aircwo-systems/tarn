<script lang="ts">
  import { onMount } from "svelte";
  import SectionHeader from "./section-header.svelte";
  import TopicList from "$lib/components/sns/topic-list.svelte";
  import SubscriptionList from "$lib/components/sns/subscription-list.svelte";
  import TopicDetail from "$lib/components/sns/topic-detail.svelte";
  import SubscriptionDetail from "$lib/components/sns/subscription-detail.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import {
    getDashboard,
    getDashboardFilters,
    matchesTagFilter,
  } from "$lib/state.svelte";

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

  let scope = $state<"topics" | "subscriptions">("topics");
  let selectedTopicName = $state<string | null>(null);
  let selectedSubscriptionArn = $state<string | null>(null);

  // Keyed by name/arn: polling replaces the objects, so holding one would freeze the panel.
  const selectedTopic = $derived(
    topics.find((topic) => topic.name === selectedTopicName) ?? topics[0] ?? null,
  );
  const selectedSubscription = $derived(
    subscriptions.find((sub) => sub.subscriptionArn === selectedSubscriptionArn) ??
      subscriptions[0] ??
      null,
  );
  const topicSubscriptions = $derived(
    selectedTopic
      ? subscriptions.filter((sub) => sub.topicName === selectedTopic.name)
      : [],
  );

  function selectTopic(name: string) {
    selectedTopicName = name;
    scope = "topics";
    history.replaceState(null, "", `#sns?topic=${encodeURIComponent(name)}`);
  }

  function selectSubscription(subscriptionArn: string) {
    selectedSubscriptionArn = subscriptionArn;
    scope = "subscriptions";
    history.replaceState(null, "", `#sns?sub=${encodeURIComponent(subscriptionArn)}`);
  }

  onMount(() => {
    const qs = window.location.hash.split("?")[1];
    const params = qs ? new URLSearchParams(qs) : null;
    const topic = params?.get("topic");
    const sub = params?.get("sub");
    if (sub) {
      selectedSubscriptionArn = sub;
      scope = "subscriptions";
    } else if (topic) {
      selectedTopicName = topic;
      scope = "topics";
    }
  });
</script>

<div class="sns">
  <SectionHeader
    title="SNS Topics"
    description="{topics.length} topics · {subscriptions.length} subscriptions · {fifoTopics} fifo · {lambdaSubscriptions} lambda targets · {sqsSubscriptions} sqs targets"
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      {#if filters.tagFilter}
        <span class="filter" title={filters.tagFilter}>Tag <span>{filters.tagFilter}</span></span>
      {/if}
    {/snippet}
  </SectionHeader>

  {#if dashboard.loading && !dashboard.data}
    <div class="layout">
      <div class="skeleton-list">
        {#each Array(6) as _, i (i)}<span style:--i={i}></span>{/each}
      </div>
    </div>
  {:else if topics.length === 0 && subscriptions.length === 0}
    <div class="blank">
      <h2>No topics yet</h2>
      <p>Create one with <code>aws sns create-topic</code> or deploy through your IaC, and it appears here.</p>
    </div>
  {:else}
    <div class="layout">
      <RcResizableAside storageKey="tarn-sns-list-width">
        <div class="scope-toggle" role="group" aria-label="SNS scope">
          <button
            type="button"
            class:active={scope === "topics"}
            onclick={() => (scope = "topics")}
          >Topics <span>{topics.length}</span></button>
          <button
            type="button"
            class:active={scope === "subscriptions"}
            onclick={() => (scope = "subscriptions")}
          >Subscriptions <span>{subscriptions.length}</span></button>
        </div>
        {#if scope === "topics"}
          <TopicList topics={topics} selectedName={selectedTopic?.name ?? null} onselect={selectTopic} />
        {:else}
          <SubscriptionList
            subscriptions={subscriptions}
            selectedArn={selectedSubscription?.subscriptionArn ?? null}
            onselect={selectSubscription}
          />
        {/if}
      </RcResizableAside>
      {#if scope === "topics" && selectedTopic}
        {#key selectedTopic.name}
          <TopicDetail
            topic={selectedTopic}
            subscriptions={topicSubscriptions}
            onselectsubscription={selectSubscription}
          />
        {/key}
      {:else if scope === "subscriptions" && selectedSubscription}
        {#key selectedSubscription.subscriptionArn}
          <SubscriptionDetail subscription={selectedSubscription} />
        {/key}
      {:else}
        <div class="blank">
          <h2>Nothing selected</h2>
          <p>Pick an entry from the list to inspect it.</p>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .sns { display: flex; flex-direction: column; min-height: 100%; }
  .layout {
    display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 28px; padding: 20px 0 48px;
    max-width: 1320px; align-items: start;
  }
  @media (max-width: 900px) {
    .layout { grid-template-columns: minmax(0, 1fr); }
  }

  .filter {
    display: inline-flex; align-items: center; gap: 6px; height: 24px; padding: 0 9px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11px; color: var(--text-tertiary);
  }
  .filter span { font-family: var(--font-mono, ui-monospace, monospace); color: var(--text-primary); }

  .scope-toggle {
    display: grid; grid-template-columns: 1fr 1fr; gap: 2px; padding: 2px; margin-bottom: 8px;
    border-radius: 8px; border: 1px solid var(--border-subtle); background: var(--bg-app);
  }
  .scope-toggle button {
    display: inline-flex; align-items: center; justify-content: center; gap: 6px;
    height: 26px; border-radius: 6px; font-size: 11.5px; color: var(--text-tertiary);
    transition: color 120ms ease, background 120ms ease;
  }
  .scope-toggle button:hover { color: var(--text-secondary); }
  .scope-toggle button.active { color: var(--text-primary); background: var(--bg-element); }
  .scope-toggle button span {
    font: 10.5px var(--font-mono, ui-monospace, monospace); font-variant-numeric: tabular-nums;
    color: var(--text-tertiary);
  }
  .scope-toggle button.active span { color: var(--text-secondary); }

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 4px; font-size: 12px; color: var(--text-secondary); }
  .blank code { font-family: var(--font-mono, ui-monospace, monospace); font-size: 11.5px; color: var(--text-primary); }

  .skeleton-list { display: flex; flex-direction: column; gap: 6px; }
  .skeleton-list span {
    height: 34px; border-radius: 8px; background: var(--bg-element);
    animation: pulse 1.4s ease-in-out infinite; animation-delay: calc(var(--i) * 80ms);
  }
  @keyframes pulse { 50% { opacity: 0.5; } }
  @keyframes fadeUp { from { opacity: 0; transform: translateY(6px); } }
  @media (prefers-reduced-motion: reduce) {
    .blank, .skeleton-list span { animation: none; }
  }
</style>
