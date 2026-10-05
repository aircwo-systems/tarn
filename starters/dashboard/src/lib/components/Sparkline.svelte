<script lang="ts">
  let {
    values,
    label,
    height = 40,
  }: { values: readonly number[]; label: string; height?: number } = $props();
  const peak = $derived(Math.max(1, ...values));
  const points = $derived(
    values
      .map(
        (value, i) =>
          `${(i / Math.max(1, values.length - 1)) * 200},${38 - (value / peak) * 32}`,
      )
      .join(" "),
  );
</script>

<svg
  viewBox="0 0 200 40"
  role="img"
  aria-label={label}
  preserveAspectRatio="none"
  style:height="{height}px"
>
  <path d="M0 39 H200" class="baseline" />
  <polyline
    {points}
    fill="none"
    stroke="currentColor"
    stroke-width="1.5"
    vector-effect="non-scaling-stroke"
  />
</svg>

<style>
  svg {
    display: block;
    width: 100%;
    height: 40px;
    overflow: visible;
    color: var(--accent);
  }
  .baseline {
    stroke: var(--line);
    stroke-width: 1;
  }
</style>
