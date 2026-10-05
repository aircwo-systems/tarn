<script lang="ts" module>
  export type Tone = "success" | "warning" | "danger" | "info" | "neutral";
</script>

<script lang="ts">
  let { label, tone = "neutral" }: { label: string; tone?: Tone } = $props();
</script>

<span class="badge" data-tone={tone}
  ><span class="dot" aria-hidden="true"></span>{label}</span
>

<style>
  .badge {
    --tone: var(--ink-secondary);
    --tint: var(--surface-raised);
    display: inline-flex;
    align-items: center;
    gap: 4px;
    white-space: nowrap;
    font: 11px/1.5 var(--font-ui);
    color: var(--tone);
    background: var(--tint);
    padding: 2px 8px;
    border: 1px solid color-mix(in srgb, var(--tone) 35%, transparent);
    border-radius: 8px;
  }
  .dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--tone);
  }
  [data-tone="success"] {
    --tone: var(--success);
    --tint: var(--success-soft);
  }
  [data-tone="warning"] {
    --tone: var(--warning);
    --tint: var(--warning-soft);
  }
  [data-tone="danger"] {
    --tone: var(--danger);
    --tint: var(--danger-soft);
  }
  [data-tone="info"] {
    --tone: var(--info);
    --tint: var(--info-soft);
  }
</style>
