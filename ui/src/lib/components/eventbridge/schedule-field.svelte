<script lang="ts">
  import { describeSchedule, SCHEDULE_PRESETS } from "$lib/eventbridge-schedule";

  let { value = $bindable(""), id }: { value: string; id?: string } = $props();

  const info = $derived(describeSchedule(value));
</script>

<div class="schedule-field">
  <div class="input" class:invalid={info.kind === "invalid"}>
    <input {id} bind:value spellcheck="false" placeholder="rate(5 minutes)" aria-describedby="{id}-reading" />
    <span id="{id}-reading" class="reading">{info.label}</span>
  </div>
  <div class="presets">
    {#each SCHEDULE_PRESETS as preset (preset.expr)}
      <button type="button" class:active={value.trim() === preset.expr} onclick={() => (value = preset.expr)}>
        {preset.label}
      </button>
    {/each}
  </div>
</div>

<style>
  .schedule-field { display: flex; flex-direction: column; gap: 8px; }
  .input {
    display: flex; align-items: center; gap: 10px; height: 34px; padding: 0 12px; border-radius: 8px;
    border: 1px solid var(--border-subtle); background: var(--bg-app); transition: border-color 120ms ease;
  }
  .input:hover { border-color: var(--border-default); }
  .input:focus-within { border-color: var(--border-focus); }
  .input.invalid { border-color: color-mix(in srgb, var(--accent-red) 45%, transparent); }
  input {
    flex: 1; min-width: 0; background: transparent; border: 0; outline: none;
    font: 12.5px var(--font-mono, ui-monospace, monospace); color: var(--text-primary);
  }
  .reading { flex-shrink: 0; font-size: 11px; color: var(--text-tertiary); }
  .invalid .reading { color: var(--accent-red); }
  .presets { display: flex; flex-wrap: wrap; gap: 4px; }
  .presets button {
    height: 22px; padding: 0 8px; border-radius: 8px; border: 1px solid transparent;
    font-size: 11px; color: var(--text-tertiary); background: var(--bg-element);
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease;
  }
  .presets button:hover { color: var(--text-primary); background: var(--bg-element-hover); }
  .presets button.active {
    color: var(--accent-green); border-color: color-mix(in srgb, var(--accent-green) 45%, transparent);
    background: color-mix(in srgb, var(--accent-green) 10%, transparent);
  }
</style>
