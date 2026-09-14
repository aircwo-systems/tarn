<script lang="ts">
  let {
    label,
    value,
    sub = "",
    tone,
  }: {
    label: string;
    value: string | number;
    sub?: string;
    tone?: "green" | "amber" | "red";
  } = $props();
</script>

<div class="rc-stat">
  <span class="label">{label}</span>
  <span class="value" style:color={tone ? `var(--accent-${tone})` : undefined}>{value}</span>
  {#if sub}<span class="sub">{sub}</span>{/if}
</div>

<style>
  .rc-stat { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
  .label { font-size: 10.5px; letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-tertiary); }
  .value {
    font-family: var(--font-mono, ui-monospace, monospace); font-size: 20px; font-weight: 600;
    letter-spacing: -0.02em; font-variant-numeric: tabular-nums; color: var(--text-primary);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .sub { font-size: 11px; color: var(--text-tertiary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
</style>
