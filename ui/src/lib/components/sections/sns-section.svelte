<script lang="ts">
  import { matchesResourceFilter } from "$lib/filter-utils";
  import { onMount } from "svelte";
  import { MagnifyingGlassIcon, SidebarSimpleIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import TopicList from "$lib/components/sns/topic-list.svelte";
  import SubscriptionList from "$lib/components/sns/subscription-list.svelte";
  import TopicDetail from "$lib/components/sns/topic-detail.svelte";
  import SubscriptionDetail from "$lib/components/sns/subscription-detail.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import {
    getDashboard,
    getDashboardFilters,
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
      matchesResourceFilter("topic", filters.tagFilter, topic.tags),
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

  let listCollapsed = $state(false);

  function toggleListCollapse() {
    listCollapsed = !listCollapsed;
    try {
      localStorage.setItem("tarn-sns-list-collapsed", String(listCollapsed));
    } catch {}
  }

  let scope = $state<"topics" | "subscriptions">("topics");
  let selectedTopicName = $state<string | null>(null);
  let selectedSubscriptionArn = $state<string | null>(null);
  let query = $state("");

  const visibleTopics = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return topics;
    return topics.filter((t) => t.name.toLowerCase().includes(q));
  });

  const visibleSubscriptions = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return subscriptions;
    return subscriptions.filter(
      (sub) =>
        sub.topicName.toLowerCase().includes(q) ||
        sub.protocol.toLowerCase().includes(q) ||
        sub.endpoint.toLowerCase().includes(q),
    );
  });

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

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    const forward = e.key === "ArrowDown" || e.key === "ArrowRight";
    if (scope === "topics") {
      if (visibleTopics.length === 0) return;
      e.preventDefault();
      const idx = visibleTopics.findIndex((t) => t.name === selectedTopic?.name);
      const next = forward ? Math.min(visibleTopics.length - 1, idx + 1) : Math.max(0, idx - 1);
      selectTopic(visibleTopics[next].name);
    } else {
      if (visibleSubscriptions.length === 0) return;
      e.preventDefault();
      const idx = visibleSubscriptions.findIndex((s) => s.subscriptionArn === selectedSubscription?.subscriptionArn);
      const next = forward ? Math.min(visibleSubscriptions.length - 1, idx + 1) : Math.max(0, idx - 1);
      selectSubscription(visibleSubscriptions[next].subscriptionArn);
    }
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
    try {
      const saved = localStorage.getItem("tarn-sns-list-collapsed");
      if (saved !== null) {
        listCollapsed = saved === "true";
      }
    } catch {}
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
      <div class="header-filter">
        <MagnifyingGlassIcon size={12} />
        <input
          placeholder="Filter {scope}..."
          bind:value={query}
          aria-label="Filter {scope}"
        />
        {#if query}
          <button
            type="button"
            class="clear-query-btn"
            onclick={() => (query = "")}
            aria-label="Clear filter"
          >
            &times;
          </button>
        {/if}
      </div>
      {#if filters.tagFilter}
        <span class="filter" title={filters.tagFilter}>Filter <span>{filters.tagFilter}</span></span>
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
    <div class="layout" class:list-collapsed={listCollapsed}>
      {#if listCollapsed}
        <div class="list-toolbar" role="toolbar" aria-label="SNS overview">
          <div class="toolbar-leading">
            <button
              type="button"
              class="expand-list-btn"
              onclick={toggleListCollapse}
              title="Expand {scope} list"
              aria-label="Expand {scope} list"
            >
              <SidebarSimpleIcon size={13} weight="fill" />
              <span class="expand-label">{scope === "topics" ? "Topic list" : "Subscription list"}</span>
              <span class="count-badge">{scope === "topics" ? topics.length : subscriptions.length}</span>
            </button>

            {#if sidebarCollapsed}
              <span class="toolbar-divider" aria-hidden="true"></span>
              <div class="toolbar-stats">
                <span class="toolbar-stat" title="{topics.length} topics">
                  <span class="status-dot red"></span>
                  <span>{topics.length} topics</span>
                </span>
                <span class="toolbar-stat" title="{subscriptions.length} subscriptions">
                  <span class="status-dot green"></span>
                  <span>{subscriptions.length} subs</span>
                </span>
              </div>
            {/if}
          </div>

          <div class="toolbar-trailing">
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="chips-row" role="tablist" tabindex="0" aria-label="SNS switcher" onkeydown={onKeydown}>
              {#if scope === "topics"}
                {#each visibleTopics as topic (topic.name)}
                  <button
                    type="button"
                    role="tab"
                    class="item-chip"
                    class:selected={topic.name === selectedTopic?.name}
                    aria-selected={topic.name === selectedTopic?.name}
                    onclick={() => selectTopic(topic.name)}
                    title="{topic.name} ({topic.fifo ? 'FIFO' : 'Standard'} · {topic.subscriptions} subscriptions)"
                  >
                    <span class="chip-dot" style:background="var(--accent-red, #fb7185)"></span>
                    <span class="chip-name">{topic.name}</span>
                    <span class="chip-badge">{topic.subscriptions} sub{topic.subscriptions === 1 ? '' : 's'}</span>
                  </button>
                {:else}
                  <span class="chips-none">No match for "{query}"</span>
                {/each}
              {:else}
                {#each visibleSubscriptions as sub (sub.subscriptionArn)}
                  <button
                    type="button"
                    role="tab"
                    class="item-chip"
                    class:selected={sub.subscriptionArn === selectedSubscription?.subscriptionArn}
                    aria-selected={sub.subscriptionArn === selectedSubscription?.subscriptionArn}
                    onclick={() => selectSubscription(sub.subscriptionArn)}
                    title="{sub.endpoint} ({sub.protocol} · {sub.topicName})"
                  >
                    <span class="chip-dot" style:background="var(--accent-green, #10b981)"></span>
                    <span class="chip-name">{sub.endpoint}</span>
                    <span class="chip-badge">{sub.protocol}</span>
                  </button>
                {:else}
                  <span class="chips-none">No match for "{query}"</span>
                {/each}
              {/if}
            </div>
          </div>
        </div>
      {/if}

      {#if !listCollapsed}
        <RcResizableAside
          storageKey="tarn-sns-list-width"
          collapsible={true}
          onToggleCollapse={toggleListCollapse}
        >
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
            <TopicList
              {topics}
              selectedName={selectedTopic?.name ?? null}
              onselect={selectTopic}
              bind:query
              onToggleCollapse={toggleListCollapse}
            />
          {:else}
            <SubscriptionList
              {subscriptions}
              selectedArn={selectedSubscription?.subscriptionArn ?? null}
              onselect={selectSubscription}
              bind:query
              onToggleCollapse={toggleListCollapse}
            />
          {/if}
        </RcResizableAside>
      {/if}

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
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 9px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11px; color: var(--text-tertiary);
    background: #ffffff;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.03);
  }
  .filter span { font-family: var(--font-mono, ui-monospace, monospace); color: var(--text-primary); }

  :global(.dark) .filter {
    background: var(--bg-element);
    box-shadow: none;
  }

  .scope-toggle {
    display: flex; gap: 4px; padding: 3px; border-radius: 8px;
    background: var(--bg-element); border: 1px solid var(--border-subtle);
    margin-bottom: 8px;
  }
  .scope-toggle button {
    flex: 1; display: inline-flex; align-items: center; justify-content: center; gap: 6px;
    height: 26px; border: 0; border-radius: 6px; background: transparent;
    font-size: 11.5px; font-weight: 500; color: var(--text-secondary); cursor: pointer;
    transition: background 120ms ease, color 120ms ease, box-shadow 120ms ease;
  }
  .scope-toggle button.active {
    background: #ffffff; color: var(--text-primary);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
  }
  .scope-toggle button span {
    font-size: 10px; font-variant-numeric: tabular-nums; opacity: 0.7;
  }

  :global(.dark) .scope-toggle button.active {
    background: var(--bg-surface, #1e1e24);
    box-shadow: none;
  }

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 4px; font-size: 12px; color: var(--text-secondary); }
  .blank code { font-family: var(--font-mono, ui-monospace, monospace); font-size: 11.5px; color: var(--text-primary); }

  .skeleton-list { display: flex; flex-direction: column; gap: 6px; width: 260px; }
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
