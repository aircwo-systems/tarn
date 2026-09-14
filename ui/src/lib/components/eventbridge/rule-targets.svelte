<script lang="ts">
  import { ArrowUpRightIcon, CaretRightIcon, PencilSimpleIcon, PlusIcon, XIcon } from "phosphor-svelte";
  import { slide } from "svelte/transition";
  import RcButton from "$lib/components/rack/rc-button.svelte";
  import TargetSettings from "./target-settings.svelte";
  import { functionNameFromArn, targetService } from "$lib/eventbridge-schedule";
  import { timeAgo } from "$lib/utils";
  import type { EventBridgeRuleSummary, EventBridgeTargetSummary, FunctionSummary } from "$lib/types";

  let {
    rule,
    functions,
    onadd,
    onremove,
  }: {
    rule: EventBridgeRuleSummary;
    functions: FunctionSummary[];
    onadd: (target: { id: string; arn: string; input?: string; inputPath?: string }) => Promise<boolean>;
    onremove: (id: string) => Promise<void>;
  } = $props();

  const targets = $derived(rule.targets ?? []);

  let adding = $state(false);
  let busy = $state(false);
  let removing = $state<string | null>(null);
  let fnName = $state("");
  let input = $state("");
  let inputPath = $state("");
  let expanded = $state<string | null>(null);
  /** id of the target being edited; null when adding */
  let editing = $state<string | null>(null);

  // Functions not already targeted come first; they're the likely pick.
  const candidates = $derived(
    functions.filter((fn) => !targets.some((t) => functionNameFromArn(t.arn) === fn.name)),
  );

  function nextId(): string {
    let n = targets.length + 1;
    while (targets.some((t) => t.id === `t${n}`)) n++;
    return `t${n}`;
  }

  function open() {
    fnName = candidates[0]?.name ?? functions[0]?.name ?? "";
    input = "";
    inputPath = "";
    editing = null;
    adding = true;
  }

  // PutTargets replaces the whole target, so only offer edit where the form covers every field.
  function editable(t: EventBridgeTargetSummary) {
    return !!functionNameFromArn(t.arn) && !t.inputTemplate && !t.inputPathsMap && !t.taskDefinition;
  }

  function edit(t: EventBridgeTargetSummary) {
    fnName = functionNameFromArn(t.arn) ?? "";
    input = t.input ? prettyJson(t.input) : "";
    inputPath = t.inputPath ?? "";
    editing = t.id;
    adding = true;
  }

  function prettyJson(raw: string) {
    try {
      return JSON.stringify(JSON.parse(raw), null, 2);
    } catch {
      return raw;
    }
  }

  const inputError = $derived.by(() => {
    if (!input.trim()) return "";
    try {
      JSON.parse(input);
      return "";
    } catch {
      return "Not valid JSON";
    }
  });

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (!fnName || inputError) return;
    const fn = functions.find((f) => f.name === fnName);
    busy = true;
    const ok = await onadd({
      id: editing ?? nextId(),
      arn: fn?.arn ?? fnName,
      input: input.trim() || undefined,
      inputPath: inputPath.trim() || undefined,
    });
    busy = false;
    if (ok) adding = false;
  }

  async function remove(id: string) {
    removing = id;
    await onremove(id);
    removing = null;
  }

  function resultTone(result?: string): "ok" | "err" | undefined {
    if (!result) return undefined;
    return /error|fail/i.test(result) ? "err" : "ok";
  }
</script>

<div class="targets">
  {#if targets.length === 0 && !adding}
    <div class="empty">
      <p>Nothing runs when this rule fires.</p>
      <RcButton variant="primary" small onclick={open} disabled={functions.length === 0}>
        <PlusIcon size={11} />Add a function
      </RcButton>
    </div>
  {:else}
    <ul>
      {#each targets as t (t.id)}
        {@const fn = functionNameFromArn(t.arn)}
        {@const tone = resultTone(t.lastResult)}
        {@const open = expanded === t.id}
        <li class:err={tone === "err"} class:open transition:slide={{ duration: 180 }}>
          <div class="row">
          <button
            type="button"
            class="expand"
            aria-expanded={open}
            aria-label="{open ? 'Hide' : 'Show'} settings for {t.id}"
            onclick={() => (expanded = open ? null : t.id)}
          ><CaretRightIcon size={10} weight="bold" /></button>
          <span class="service">{targetService(t.arn)}</span>
          <span class="body">
            {#if fn}
              <a class="name" href="#functions?fn={encodeURIComponent(fn)}">{fn}<ArrowUpRightIcon size={10} /></a>
            {:else}
              <span class="name" title={t.arn}>{t.arn}</span>
            {/if}
            <span class="meta">
              <span class="id">{t.id}</span>
              {#if t.lastInvokedAt}<span>· {timeAgo(t.lastInvokedAt)}</span>{/if}
              {#if t.lastResult}<span class="result" data-tone={tone}>· {t.lastResult}</span>{/if}
            </span>
          </span>
          <button
            type="button"
            class="remove"
            title="Remove target {t.id}"
            aria-label="Remove target {t.id}"
            disabled={removing === t.id}
            onclick={() => remove(t.id)}
          ><XIcon size={12} /></button>
          </div>
          {#if open}
            <div class="panel" transition:slide={{ duration: 200 }}>
              <TargetSettings target={t} />
              {#if editable(t)}
                <div class="panel-actions">
                  <RcButton small onclick={() => edit(t)}><PencilSimpleIcon size={11} />Edit input</RcButton>
                </div>
              {/if}
            </div>
          {/if}
        </li>
      {/each}
    </ul>

    {#if adding}
      <form class="add" onsubmit={submit} transition:slide={{ duration: 200 }}>
        <label>
          <span>Function</span>
          <select bind:value={fnName}>
            {#if editing && !candidates.some((f) => f.name === fnName)}<option value={fnName}>{fnName}</option>{/if}
            {#each candidates as fn (fn.name)}<option value={fn.name}>{fn.name}</option>{/each}
            {#if candidates.length < functions.length}
              <optgroup label="Already targeted">
                {#each functions.filter((f) => !candidates.includes(f) && !(editing && f.name === fnName)) as fn (fn.name)}<option value={fn.name}>{fn.name}</option>{/each}
              </optgroup>
            {/if}
          </select>
        </label>
        <label>
          <span>Input <em>optional constant JSON</em></span>
          <textarea rows="3" bind:value={input} spellcheck="false" placeholder={'{"source": "schedule"}'} class:invalid={!!inputError}></textarea>
          {#if inputError}<small>{inputError}</small>{/if}
        </label>
        <label>
          <span>InputPath <em>optional</em></span>
          <input bind:value={inputPath} spellcheck="false" placeholder="$.detail" />
        </label>
        <div class="form-actions">
          <RcButton variant="ghost" small onclick={() => (adding = false)}>Cancel</RcButton>
          <RcButton variant="primary" small type="submit" disabled={busy || !fnName || !!inputError}>
            {busy ? "Saving…" : editing ? `Save ${editing}` : "Add target"}
          </RcButton>
        </div>
      </form>
    {:else}
      <button type="button" class="add-row" onclick={open} disabled={functions.length === 0}>
        <PlusIcon size={11} />Add target
      </button>
    {/if}
  {/if}
</div>

<style>
  .targets { display: flex; flex-direction: column; gap: 6px; }
  .empty { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 4px 0; }
  .empty p { font-size: 11.5px; color: var(--text-tertiary); }
  ul { display: flex; flex-direction: column; gap: 2px; }
  li { border-radius: 8px; transition: background 120ms ease; }
  .row { display: flex; align-items: center; gap: 10px; padding: 7px 10px 7px 4px; }
  li:hover:not(.open) { background: var(--bg-element-hover); }
  li.open { background: var(--bg-element); }
  .expand {
    display: grid; place-items: center; width: 18px; height: 18px; border-radius: 6px; flex-shrink: 0; color: var(--text-tertiary);
    transition: transform 200ms var(--ease-snappy), color 120ms ease;
  }
  .expand:hover { color: var(--text-primary); }
  .open .expand { transform: rotate(90deg); color: var(--text-primary); }
  .panel { padding: 4px 12px 12px 32px; }
  .panel-actions { display: flex; justify-content: flex-end; margin-top: 10px; }
  li.err { background: color-mix(in srgb, var(--accent-red) 5%, transparent); }
  .service { flex-shrink: 0; min-width: 44px; font-size: 10px; letter-spacing: 0.04em; text-transform: uppercase; color: var(--text-tertiary); }
  .body { display: flex; flex-direction: column; min-width: 0; flex: 1; }
  .name {
    display: inline-flex; align-items: center; gap: 4px; min-width: 0; max-width: 100%;
    font: 12px var(--font-mono, ui-monospace, monospace); color: var(--text-primary); text-decoration: none;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  a.name :global(svg) { color: var(--text-tertiary); opacity: 0; transition: opacity 120ms ease; }
  a.name:hover :global(svg) { opacity: 1; }
  .meta { display: flex; gap: 5px; font-size: 10.5px; color: var(--text-tertiary); overflow: hidden; white-space: nowrap; }
  .id { font-family: var(--font-mono, ui-monospace, monospace); }
  .result[data-tone="err"] { color: var(--accent-red); }
  .remove {
    display: grid; place-items: center; width: 24px; height: 24px; border-radius: 8px; flex-shrink: 0;
    color: var(--text-tertiary); opacity: 0; transition: opacity 120ms ease, color 120ms ease, background 120ms ease;
  }
  li:hover .remove, .remove:focus-visible { opacity: 1; }
  .remove:hover { color: var(--accent-red); background: color-mix(in srgb, var(--accent-red) 10%, transparent); }

  .add-row {
    display: flex; align-items: center; gap: 6px; height: 30px; padding: 0 10px; border-radius: 8px;
    font-size: 11.5px; color: var(--text-tertiary); transition: color 120ms ease, background 120ms ease;
  }
  .add-row:hover:not(:disabled) { color: var(--text-primary); background: var(--bg-element-hover); }
  .add-row:disabled { opacity: 0.5; }

  .add { display: flex; flex-direction: column; gap: 10px; margin-top: 6px; padding: 12px; border-radius: 10px; background: var(--bg-app); }
  label { display: flex; flex-direction: column; gap: 5px; }
  label > span { font-size: 11px; color: var(--text-secondary); }
  em { font-style: normal; color: var(--text-tertiary); margin-left: 4px; }
  select, input, textarea {
    width: 100%; padding: 6px 10px; border-radius: 8px; border: 1px solid var(--border-subtle); background: var(--bg-stage);
    font: 12px var(--font-mono, ui-monospace, monospace); color: var(--text-primary); outline: none; resize: vertical;
    transition: border-color 120ms ease;
  }
  select, input { height: 30px; padding-block: 0; }
  select:focus, input:focus, textarea:focus { border-color: var(--border-focus); }
  textarea.invalid { border-color: color-mix(in srgb, var(--accent-red) 45%, transparent); }
  small { font-size: 10.5px; color: var(--accent-red); }
  .form-actions { display: flex; justify-content: flex-end; gap: 6px; }
</style>
