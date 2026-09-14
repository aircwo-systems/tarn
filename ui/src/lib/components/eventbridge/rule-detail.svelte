<script lang="ts">
  import { ArrowUpRightIcon, CheckIcon, CopyIcon, PauseIcon, PlayIcon, TrashIcon } from "phosphor-svelte";
  import { fly } from "svelte/transition";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import RcButton from "$lib/components/rack/rc-button.svelte";
  import RcTonePill from "$lib/components/rack/rc-tone-pill.svelte";
  import RuleTimeline from "./rule-timeline.svelte";
  import RuleTargets from "./rule-targets.svelte";
  import RuleForm, { type RuleDraft } from "./rule-form.svelte";
  import { describeSchedule } from "$lib/eventbridge-schedule";
  import type { EventBridgeFireResult, EventBridgeRuleSummary, FunctionSummary } from "$lib/types";

  let {
    rule,
    functions,
    onsave,
    ontoggle,
    onfire,
    ondelete,
    onaddtarget,
    onremovetarget,
  }: {
    rule: EventBridgeRuleSummary;
    functions: FunctionSummary[];
    onsave: (draft: RuleDraft) => Promise<void>;
    ontoggle: () => Promise<void>;
    onfire: () => Promise<EventBridgeFireResult | null>;
    ondelete: () => Promise<void>;
    onaddtarget: (target: { id: string; arn: string; input?: string; inputPath?: string }) => Promise<boolean>;
    onremovetarget: (id: string) => Promise<void>;
  } = $props();

  const enabled = $derived(rule.state === "ENABLED");
  const schedule = $derived(describeSchedule(rule.scheduleExpression));
  const targetCount = $derived(rule.targets?.length ?? 0);

  let firing = $state(false);
  let toggling = $state(false);
  let lastFire = $state<EventBridgeFireResult | null>(null);
  let confirmDelete = $state(false);
  let copied = $state(false);

  async function fire() {
    firing = true;
    try {
      lastFire = await onfire();
    } finally {
      firing = false;
    }
  }

  async function toggle() {
    toggling = true;
    try {
      await ontoggle();
    } finally {
      toggling = false;
    }
  }

  async function copyArn() {
    await navigator.clipboard.writeText(rule.arn);
    copied = true;
    setTimeout(() => (copied = false), 1600);
  }

  // Form keys on the saved values so a successful save re-seeds it.
  const initial = $derived<RuleDraft>({
    name: rule.name,
    scheduleExpression: rule.scheduleExpression,
    description: rule.description ?? "",
    enabled,
  });
  const formKey = $derived(`${initial.scheduleExpression}|${initial.description}|${initial.enabled}`);
</script>

<div class="detail">
  <header class="hero">
    <div class="identity">
      <div class="title-row">
        <h1 title={rule.name}>{rule.name}</h1>
        <RcTonePill tone={enabled ? "green" : "neutral"}>{enabled ? "enabled" : "disabled"}</RcTonePill>
      </div>
      <p class="subline">
        <span>{schedule.label}</span><i></i><code>{rule.scheduleExpression || "event pattern"}</code><i></i>
        <span>{targetCount} target{targetCount === 1 ? "" : "s"}</span>
      </p>
    </div>
    <div class="hero-actions">
      <RcButton onclick={toggle} disabled={toggling}>
        {#if enabled}<PauseIcon size={12} />Pause{:else}<PlayIcon size={12} />Resume{/if}
      </RcButton>
      <RcButton variant="primary" onclick={fire} disabled={firing || targetCount === 0} title={targetCount === 0 ? "Add a target first" : "Deliver an event to every target now"}>
        <PlayIcon size={12} weight="fill" />{firing ? "Firing…" : "Fire now"}
      </RcButton>
    </div>
  </header>

  {#if lastFire}
    {@const ok = lastFire.failed === 0}
    <div class="fire-result" class:bad={!ok} in:fly={{ y: -4, duration: 200 }}>
      <span class="mark">{#if ok}<CheckIcon size={12} weight="bold" />{:else}!{/if}</span>
      <span>
        Fired {new Date(lastFire.firedAt).toLocaleTimeString([], { hour12: false })} —
        <strong>{lastFire.successful}/{lastFire.targets}</strong> targets succeeded{lastFire.failed ? `, ${lastFire.failed} failed` : ""}
      </span>
      {#if lastFire.traceId}
        <a href="#xray?trace={encodeURIComponent(lastFire.traceId)}">Open trace<ArrowUpRightIcon size={10} /></a>
      {/if}
      <button type="button" class="dismiss" aria-label="Dismiss" onclick={() => (lastFire = null)}>×</button>
    </div>
  {/if}

  <RcPanel title="Schedule" index={0}>
    <RuleTimeline {rule} />
  </RcPanel>

  <RcPanel title="Targets" description="What receives the event each time the rule fires." index={1}>
    <RuleTargets {rule} {functions} onadd={onaddtarget} onremove={onremovetarget} />
  </RcPanel>

  <RcPanel title="Settings" index={2}>
    {#key formKey}
      <RuleForm {initial} submitLabel="Save changes" onsubmit={onsave} />
    {/key}
  </RcPanel>

  <RcPanel title="Details" index={3}>
    {#snippet actions()}
      <RcButton variant="ghost" small onclick={copyArn}>
        {#if copied}<CheckIcon size={11} />Copied{:else}<CopyIcon size={11} />ARN{/if}
      </RcButton>
    {/snippet}
    <RcKv
      items={[
        { label: "Event bus", value: "default", mono: true },
        { label: "Type", value: schedule.kind === "pattern" ? "Event pattern" : "Scheduled", mono: false },
        { label: "ARN", value: rule.arn, mono: true, dim: true },
      ]}
    />
    <div class="danger">
      {#if confirmDelete}
        <span>Delete <code>{rule.name}</code> and its {targetCount} target{targetCount === 1 ? "" : "s"}?</span>
        <RcButton variant="ghost" small onclick={() => (confirmDelete = false)}>Keep</RcButton>
        <RcButton variant="danger" small onclick={ondelete}><TrashIcon size={11} />Delete</RcButton>
      {:else}
        <RcButton variant="ghost" small onclick={() => (confirmDelete = true)}><TrashIcon size={11} />Delete rule</RcButton>
      {/if}
    </div>
  </RcPanel>
</div>

<style>
  .detail { display: flex; flex-direction: column; gap: 14px; min-width: 0; }

  .hero { display: flex; align-items: flex-end; justify-content: space-between; gap: 16px; flex-wrap: wrap; padding: 4px 2px 10px; }
  .identity { min-width: 0; }
  .title-row { display: flex; align-items: center; gap: 10px; min-width: 0; }
  h1 {
    font: 600 19px var(--font-mono, ui-monospace, monospace); letter-spacing: -0.02em; color: var(--text-primary);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .subline { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 4px; font-size: 11.5px; color: var(--text-tertiary); }
  .subline i { width: 3px; height: 3px; border-radius: 1px; background: var(--border-default); }
  code { font-family: var(--font-mono, ui-monospace, monospace); font-size: 11px; color: var(--text-secondary); }
  .hero-actions { display: flex; gap: 6px; }

  .fire-result {
    --tone: var(--accent-green);
    display: flex; align-items: center; gap: 10px; padding: 8px 12px; border-radius: 10px; font-size: 11.5px;
    color: var(--text-secondary);
    border: 1px solid color-mix(in srgb, var(--tone) 45%, transparent);
    background: color-mix(in srgb, var(--tone) 10%, transparent);
  }
  .fire-result.bad { --tone: var(--accent-red); }
  .fire-result strong { color: var(--text-primary); font-weight: 600; }
  .mark { display: grid; place-items: center; width: 18px; height: 18px; border-radius: 6px; color: var(--tone); font-weight: 700; }
  .fire-result a { display: inline-flex; align-items: center; gap: 3px; margin-left: auto; color: var(--tone); text-decoration: none; }
  .fire-result a:hover { text-decoration: underline; }
  .dismiss { width: 20px; height: 20px; border-radius: 6px; color: var(--text-tertiary); font-size: 14px; line-height: 1; }
  .fire-result:not(:has(a)) .dismiss { margin-left: auto; }
  .dismiss:hover { color: var(--text-primary); background: var(--bg-element-hover); }

  .danger { display: flex; align-items: center; justify-content: flex-end; gap: 6px; margin-top: 14px; font-size: 11.5px; color: var(--text-secondary); }
  .danger span { margin-right: auto; }
  .danger :global([data-variant="ghost"]:hover) { color: var(--accent-red); }
</style>
