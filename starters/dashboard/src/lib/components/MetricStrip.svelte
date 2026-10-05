<script lang="ts" module>
  export interface Metric {
    label: string;
    value: string | number;
    unit?: string;
    description: string;
    trend?: readonly number[];
  }
</script>

<script lang="ts">
  import Sparkline from "./Sparkline.svelte";
  let { metrics }: { metrics: readonly Metric[] } = $props();
</script>

<dl class="strip">
  {#each metrics as metric (metric.label)}
    <div class="metric">
      <dt class="eyebrow">{metric.label}</dt>
      <dd>
        <span class="value">{metric.value}</span>{#if metric.unit}<span
            class="unit">{metric.unit}</span
          >{/if}
      </dd>
      <p>{metric.description}</p>
      {#if metric.trend}<Sparkline
          height={18}
          values={metric.trend}
          label={`${metric.label}: ${metric.trend.join(", ")}`}
        />{/if}
    </div>
  {/each}
</dl>

<style>
  .strip {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
    border-bottom: 1px solid var(--line);
    margin: 0 0 16px;
  }
  .metric {
    padding: 14px 16px 12px;
    border-right: 1px solid var(--line);
  }
  .metric:first-child {
    padding-left: 0;
  }
  .metric:last-child {
    border-right: 0;
  }
  dd {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    margin: var(--space-2) 0 var(--space-1);
  }
  .value {
    font: 600 20px/1.2 var(--font-data);
    letter-spacing: -0.02em;
  }
  .unit {
    color: var(--ink-tertiary);
    font: 11px var(--font-data);
  }
  p {
    font-size: 10.5px;
    color: var(--ink-tertiary);
    margin-bottom: 8px;
  }
  @media (max-width: 640px) {
    .strip {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .metric {
      padding: var(--space-4) var(--space-3);
    }
    .metric:nth-child(2n) {
      border-right: 0;
    }
    .metric:nth-child(n + 3) {
      border-top: 1px solid var(--line);
    }
    .metric:nth-child(odd) {
      padding-left: 0;
    }
    .value {
      font-size: 24px;
    }
  }
</style>
