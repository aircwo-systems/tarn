<script lang="ts">
  import { onMount } from "svelte";
  import { fly } from "svelte/transition";
  import { PlusIcon } from "phosphor-svelte";
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
  import { getDashboard, refresh } from "$lib/state.svelte";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const functions = $derived(dashboard.data?.functions ?? []);
  const rules = $derived(dashboard.data?.eventBridgeRules ?? []);

  // Keyed by name: polling replaces the objects.
  let selectedName = $state<string | null>(null);
  let creating = $state(false);
  const selected = $derived(rules.find((r) => r.name === selectedName) ?? rules[0] ?? null);

  const enabledCount = $derived(rules.filter((r) => r.state === "ENABLED").length);
  const targetCount = $derived(rules.reduce((n, r) => n + (r.targets?.length ?? 0), 0));

  let error = $state<string | null>(null);

  function select(name: string) {
    selectedName = name;
    creating = false;
    history.replaceState(null, "", `#eventbridge?rule=${encodeURIComponent(name)}`);
  }

  onMount(() => {
    const qs = window.location.hash.split("?")[1];
    const rule = qs ? new URLSearchParams(qs).get("rule") : null;
    if (rule) selectedName = rule;
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

  const blankDraft: RuleDraft = { name: "", scheduleExpression: "rate(5 minutes)", description: "", enabled: true };

  async function create(draft: RuleDraft) {
    const ok = await run(() =>
      putEventBridgeRule({
        name: draft.name,
        scheduleExpression: draft.scheduleExpression,
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
      <p>Rules run Lambda functions on a schedule, or whenever you fire them by hand.</p>
      <RcButton variant="primary" onclick={() => (creating = true)}><PlusIcon size={12} />Create your first rule</RcButton>
    </div>
  {:else}
    <div class="layout">
      <RcResizableAside storageKey="tarn-eventbridge-list-width">
        <RuleList {rules} selectedName={creating ? null : (selected?.name ?? null)} onselect={select} />
      </RcResizableAside>
      {#if creating}
        <RcPanel title="New rule" description="Schedule first, then add targets once it exists.">
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
  .error button { width: 20px; height: 20px; border-radius: 6px; font-size: 14px; line-height: 1; }

  .blank { display: flex; flex-direction: column; align-items: center; gap: 4px; padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-bottom: 12px; font-size: 12px; color: var(--text-secondary); }

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
