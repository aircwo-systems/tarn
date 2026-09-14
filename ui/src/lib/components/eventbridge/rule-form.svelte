<script lang="ts" module>
  export interface RuleDraft {
    name: string;
    scheduleExpression: string;
    description: string;
    enabled: boolean;
  }
</script>

<script lang="ts">
  import { untrack } from "svelte";
  import RcButton from "$lib/components/rack/rc-button.svelte";
  import ScheduleField from "./schedule-field.svelte";
  import { describeSchedule } from "$lib/eventbridge-schedule";

  let {
    initial,
    creating = false,
    submitLabel,
    onsubmit,
    oncancel,
  }: {
    initial: RuleDraft;
    /** shows the name field */
    creating?: boolean;
    submitLabel: string;
    onsubmit: (draft: RuleDraft) => Promise<void>;
    oncancel?: () => void;
  } = $props();

  // Snapshot once; the section remounts this form when the rule changes.
  let draft = $state<RuleDraft>(untrack(() => ({ ...initial })));
  let busy = $state(false);

  const nameValid = $derived(/^[\w.-]{1,64}$/.test(draft.name.trim()));
  const scheduleValid = $derived(describeSchedule(draft.scheduleExpression).kind !== "invalid");
  const dirty = $derived(
    creating ||
      draft.scheduleExpression.trim() !== initial.scheduleExpression ||
      draft.description.trim() !== initial.description ||
      draft.enabled !== initial.enabled,
  );
  const canSubmit = $derived(dirty && scheduleValid && (!creating || nameValid) && !busy);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    busy = true;
    try {
      await onsubmit({ ...draft, name: draft.name.trim(), scheduleExpression: draft.scheduleExpression.trim(), description: draft.description.trim() });
    } finally {
      busy = false;
    }
  }

  function reset() {
    draft = { ...initial };
  }
</script>

<form class="rule-form" onsubmit={submit}>
  {#if creating}
    <label class="field">
      <span>Name</span>
      <!-- svelte-ignore a11y_autofocus -->
      <input bind:value={draft.name} spellcheck="false" placeholder="nightly-cleanup" autofocus class:invalid={draft.name.length > 0 && !nameValid} />
      {#if draft.name.length > 0 && !nameValid}<small>Letters, numbers, . _ - only (max 64)</small>{/if}
    </label>
  {/if}

  <div class="field">
    <label for="rule-schedule">Schedule</label>
    <ScheduleField id="rule-schedule" bind:value={draft.scheduleExpression} />
  </div>

  <label class="field">
    <span>Description <em>optional</em></span>
    <input bind:value={draft.description} placeholder="What this rule is for" />
  </label>

  <label class="toggle">
    <input type="checkbox" bind:checked={draft.enabled} />
    <span class="switch" aria-hidden="true"></span>
    <span>{draft.enabled ? "Enabled — runs on schedule" : "Disabled — only fires manually"}</span>
  </label>

  {#if dirty}
    <div class="actions">
      {#if creating}
        {#if oncancel}<RcButton variant="ghost" small onclick={oncancel}>Cancel</RcButton>{/if}
      {:else}
        <RcButton variant="ghost" small onclick={reset}>Discard</RcButton>
      {/if}
      <RcButton variant="primary" small type="submit" disabled={!canSubmit}>{busy ? "Saving…" : submitLabel}</RcButton>
    </div>
  {/if}
</form>

<style>
  .rule-form { display: flex; flex-direction: column; gap: 14px; }
  .field { display: flex; flex-direction: column; gap: 6px; }
  .field > span, .field > label { font-size: 11px; color: var(--text-secondary); }
  em { font-style: normal; color: var(--text-tertiary); margin-left: 4px; }
  .field > input {
    height: 34px; padding: 0 12px; border-radius: 8px; border: 1px solid var(--border-subtle); background: var(--bg-app);
    font-size: 12.5px; color: var(--text-primary); outline: none; transition: border-color 120ms ease;
  }
  .field > input:hover { border-color: var(--border-default); }
  .field > input:focus { border-color: var(--border-focus); }
  .field > input.invalid { border-color: color-mix(in srgb, var(--accent-red) 45%, transparent); }
  small { font-size: 10.5px; color: var(--accent-red); }

  .toggle { display: inline-flex; align-items: center; gap: 10px; font-size: 11.5px; color: var(--text-secondary); cursor: pointer; width: fit-content; }
  .toggle input { position: absolute; opacity: 0; pointer-events: none; }
  .switch {
    position: relative; width: 28px; height: 16px; border-radius: 8px; background: var(--bg-element);
    border: 1px solid var(--border-default); transition: background 160ms ease, border-color 160ms ease;
  }
  .switch::after {
    content: ""; position: absolute; top: 2px; left: 2px; width: 10px; height: 10px; border-radius: 5px;
    background: var(--text-tertiary); transition: transform 200ms var(--ease-snappy), background 160ms ease;
  }
  .toggle input:checked + .switch {
    background: color-mix(in srgb, var(--accent-green) 18%, transparent);
    border-color: color-mix(in srgb, var(--accent-green) 45%, transparent);
  }
  .toggle input:checked + .switch::after { transform: translateX(12px); background: var(--accent-green); }
  .toggle input:focus-visible + .switch { outline: 1px solid var(--border-focus); outline-offset: 2px; }

  .actions { display: flex; justify-content: flex-end; gap: 6px; animation: in 200ms var(--ease-snappy) both; }
  @keyframes in { from { opacity: 0; transform: translateY(-3px); } }
  @media (prefers-reduced-motion: reduce) { .actions { animation: none; } .switch::after { transition: none; } }
</style>
