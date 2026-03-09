<script lang="ts">
  export let x: number;
  export let y: number;
  export let label: string;
  export let color: string;
  export let width = 36;
  export let status: 'active' | 'inactive' = 'active';
</script>

<g class="node">
  <rect {x} {y} {width} height="20" rx="4" 
        stroke={color} class="fill-bg-overlay" stroke-width="0.75" />
  <circle cx={x + 9} cy={y + 10} r="2" fill={color} />
  <text x={x + 15} y={y + 13} font-size="7">{label}</text>
</g>