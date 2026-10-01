<script lang="ts">
  import { matchesResourceFilter } from "$lib/filter-utils";
  import { onMount } from "svelte";
  import { fly } from "svelte/transition";
  import { MagnifyingGlassIcon, SidebarSimpleIcon, PlusIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import RcButton from "$lib/components/rack/rc-button.svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import RuleList from "$lib/components/eventbridge/rule-list.svelte";
  import RuleDetail from "$lib/components/eventbridge/rule-detail.svelte";
  import RuleForm, { type RuleDraft } from "$lib/components/eventbridge/rule-form.svelte";
  import {
    putEventBridgeRule,
    enableEventBridgeRule,
    disableEventBridgeRule,
    deleteEventBridgeRule,
    putEventBridgeTargets,
    removeEventBridgeTargets,
    fireEventBridgeRule,
  } from "$lib/api";
  import { getDashboard, getDashboardFilters, refresh } from "$lib/state.svelte";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const functions = $derived(dashboard.data?.functions ?? []);
  const rules = $derived((dashboard.data?.eventBridgeRules ?? []).filter(() => matchesResourceFilter("eventbridge", filters.tagFilter)));

  let listCollapsed = $state(false);

  function toggleListCollapse() {
    listCollapsed = !listCollapsed;
    try {
      localStorage.setItem("tarn-eventbridge-list-collapsed", String(listCollapsed));
    } catch {}
  }

  // Keyed by name: polling replaces the objects.
  let selectedName = $state<string | null>(null);
  let creating = $state(false);
  let query = $state("");

  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return rules;
    return rules.filter((r) => r.name.toLowerCase().includes(q) || r.scheduleExpression.toLowerCase().includes(q));
  });

  const selected = $derived(rules.find((r) => r.name === selectedName) ?? rules[0] ?? null);

  const enabledCount = $derived(rules.filter((r) => r.state === "ENABLED").length);
  const targetCount = $derived(rules.reduce((n, r) => n + (r.targets?.length ?? 0), 0));

  let error = $state<string | null>(null);

  function select(name: string) {
    selectedName = name;
    creating = false;
    history.replaceState(null, "", `#eventbridge?rule=${encodeURIComponent(name)}`);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((r) => r.name === selected?.name);
    const forward = e.key === "ArrowDown" || e.key === "ArrowRight";
    const next = forward ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    select(visible[next].name);
  }

  onMount(() => {
    const qs = window.location.hash.split("?")[1];
    const rule = qs ? new URLSearchParams(qs).get("rule") : null;
    if (rule) selectedName = rule;
    try {
      const saved = localStorage.getItem("tarn-eventbridge-list-collapsed");
      if (saved !== null) {
        listCollapsed = saved === "true";
      }
    } catch {}
  });

  /** Runs a mutation, refreshes, and surfaces failures in one place. */
  async function run<T>(action: () => Promise<T>): Promise<T | null> {
    error = null;
    try {
      const result = await action();
      await refresh();
      return result;
    } catch (err) {
      error = err instanceof Error ? err.message : "Request failed";
      return null;
    }
  }

  const blankDraft: RuleDraft = { name: "", scheduleExpression: "rate(5 minutes)", eventPattern: "", description: "", enabled: true };

  async function create(draft: RuleDraft) {
    const ok = await run(() =>
      putEventBridgeRule({
        name: draft.name,
        scheduleExpression: draft.scheduleExpression,
        eventPattern: draft.eventPattern,
        description: draft.description,
        state: draft.enabled ? "ENABLED" : "DISABLED",
      }),
    );
    if (ok) select(draft.name);
  }

  async function save(draft: RuleDraft) {
    if (!selected) return;
    const name = selected.name;
    await run(() =>
      putEventBridgeRule({
        name,
        scheduleExpression: draft.scheduleExpression,
        eventPattern: draft.eventPattern,
        description: draft.description,
        state: draft.enabled ? "ENABLED" : "DISABLED",
      }),
    );
  }

  async function toggle() {
    if (!selected) return;
    const { name, state } = selected;
    await run(() => (state === "ENABLED" ? disableEventBridgeRule(name) : enableEventBridgeRule(name)));
  }

  function fire() {
    if (!selected) return Promise.resolve(null);
    const name = selected.name;
    return run(() => fireEventBridgeRule(name));
  }

  async function remove() {
    if (!selected) return;
    const name = selected.name;
    const ok = await run(async () => {
      await deleteEventBridgeRule(name);
      return true;
    });
    if (ok) {
      selectedName = null;
      history.replaceState(null, "", "#eventbridge");
    }
  }

  async function addTarget(target: { id: string; arn: string; input?: string; inputPath?: string }) {
    if (!selected) return false;
    const name = selected.name;
    const result = await run(async () => {
      const res = await putEventBridgeTargets(name, [target]);
      if (res.failedEntryCount > 0) throw new Error(res.failedEntries[0]?.errorMessage ?? "Failed to add target");
      return true;
    });
    return !!result;
  }

  async function removeTarget(id: string) {
    if (!selected) return;
    const name = selected.name;
    await run(async () => {
      const res = await removeEventBridgeTargets(name, [id]);
      if (res.failedEntryCount > 0) throw new Error(res.failedEntries[0]?.errorMessage ?? "Failed to remove target");
      return true;
    });
  }
</script>

<div class="eventbridge">
  <SectionHeader
    title="EventBridge"
    description="{rules.length} rule{rules.length === 1 ? '' : 's'} · {enabledCount} enabled · {targetCount} target{targetCount === 1 ? '' : 's'} · default bus"
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      <div class="header-filter">
        <MagnifyingGlassIcon size={12} />
        <input
          placeholder="Filter rules..."
          bind:value={query}
          aria-label="Filter rules"
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
      <RcButton variant={creating ? "default" : "primary"} small onclick={() => (creating = !creating)}>
        <PlusIcon size={11} />New rule
      </RcButton>
    {/snippet}
  </SectionHeader>

  {#if error}
    <div class="error" transition:fly={{ y: -4, duration: 180 }}>
      <span>{error}</span>
      <button type="button" aria-label="Dismiss" onclick={() => (error = null)}>×</button>
    </div>
  {/if}

  {#if dashboard.loading && !dashboard.data}
    <div class="layout">
      <div class="skeleton-list">
        {#each Array(5) as _, i (i)}<span style:--i={i}></span>{/each}
      </div>
    </div>
  {:else if rules.length === 0 && !creating}
    <div class="blank">
      <h2>No rules yet</h2>
      <p>Rules run targets on a schedule, when an event matches a pattern, or when you fire them by hand.</p>
      <RcButton variant="primary" onclick={() => (creating = true)}><PlusIcon size={12} />Create your first rule</RcButton>
    </div>
  {:else}
    <div class="layout" class:list-collapsed={listCollapsed}>
      {#if listCollapsed}
        <div class="list-toolbar" role="toolbar" aria-label="Rules overview">
          <div class="toolbar-leading">
            <button
              type="button"
              class="expand-list-btn"
              onclick={toggleListCollapse}
              title="Expand rule list"
              aria-label="Expand rule list"
            >
              <SidebarSimpleIcon size={13} weight="fill" />
              <span class="expand-label">Rule list</span>
              <span class="count-badge">{rules.length}</span>
            </button>

            {#if sidebarCollapsed}
              <span class="toolbar-divider" aria-hidden="true"></span>
              <div class="toolbar-stats">
                <span class="toolbar-stat" title="{enabledCount} enabled rules">
                  <span class="status-dot green"></span>
                  <span>{enabledCount} enabled</span>
                </span>
                <span class="toolbar-stat" title="{targetCount} targets">
                  <span class="status-dot blue"></span>
                  <span>{targetCount} targets</span>
                </span>
              </div>
            {/if}
          </div>

          <div class="toolbar-trailing">
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="chips-row" role="tablist" tabindex="0" aria-label="Rule switcher" onkeydown={onKeydown}>
              {#each visible as r (r.name)}
                <button
                  type="button"
                  role="tab"
                  class="item-chip"
                  class:selected={r.name === selected?.name}
                  aria-selected={r.name === selected?.name}
                  onclick={() => select(r.name)}
                  title="{r.name} ({r.state} · {r.scheduleExpression})"
                >
                  <span
                    class="chip-dot"
                    style:background={r.state === "ENABLED" ? "var(--accent-green, #10b981)" : "var(--text-tertiary)"}
                  ></span>
                  <span class="chip-name">{r.name}</span>
                  <span class="chip-badge">{r.targets?.length ?? 0} targets</span>
                </button>
              {:else}
                <span class="chips-none">No match for "{query}"</span>
              {/each}
            </div>
          </div>
        </div>
      {/if}

      {#if !listCollapsed}
        <RcResizableAside
          storageKey="tarn-eventbridge-list-width"
          collapsible={true}
          onToggleCollapse={toggleListCollapse}
        >
          <RuleList
            {rules}
            selectedName={creating ? null : (selected?.name ?? null)}
            onselect={select}
            bind:query
            onToggleCollapse={toggleListCollapse}
          />
        </RcResizableAside>
      {/if}

      {#if creating}
        <RcPanel title="New rule" description="Choose a schedule or event pattern, then add targets.">
          <RuleForm initial={blankDraft} creating submitLabel="Create rule" onsubmit={create} oncancel={() => (creating = false)} />
        </RcPanel>
      {:else if selected}
        {#key selected.name}
          <RuleDetail
            rule={selected}
            {functions}
            onsave={save}
            ontoggle={toggle}
            onfire={fire}
            ondelete={remove}
            onaddtarget={addTarget}
            onremovetarget={removeTarget}
          />
        {/key}
      {/if}
    </div>
  {/if}
</div>

<style>
  .eventbridge { display: flex; flex-direction: column; min-height: 100%; }
  .layout {
    display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 28px; padding: 20px 0 48px;
    max-width: 1320px; align-items: start;
  }
  @media (max-width: 900px) {
    .layout { grid-template-columns: minmax(0, 1fr); }
  }

  .error {
    display: flex; align-items: center; gap: 10px; margin-top: 12px; padding: 8px 12px; border-radius: 10px; font-size: 11.5px;
    color: var(--accent-red); max-width: 1320px;
    border: 1px solid color-mix(in srgb, var(--accent-red) 45%, transparent);
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
  }
  .error span { flex: 1; }
  .error button {
    border: 0; background: transparent; font-size: 15px; color: var(--accent-red);
    cursor: pointer; padding: 0 4px; border-radius: 4px;
  }

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; display: flex; flex-direction: column; align-items: center; gap: 8px; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin: 0 0 8px; font-size: 12px; color: var(--text-secondary); max-width: 380px; }

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
