<script lang="ts" module>
  export interface KvItem {
    label: string;
    value: string;
    mono?: boolean;
    dim?: boolean;
  }
</script>

<script lang="ts">
  let { items, labelWidth = "7rem" }: { items: KvItem[]; labelWidth?: string } = $props();
</script>

<dl class="rc-kv" style:--label-w={labelWidth}>
  {#each items as item (item.label)}
    <dt>{item.label}</dt>
    <dd class:mono={item.mono} class:dim={item.dim} title={item.value}>{item.value}</dd>
  {/each}
</dl>

<style>
  .rc-kv { display: grid; grid-template-columns: var(--label-w) minmax(0, 1fr); row-gap: 7px; column-gap: 12px; font-size: 12px; }
  dt { color: var(--text-tertiary); }
  dd { color: var(--text-primary); word-break: break-all; }
  dd.mono { font-family: var(--font-mono, ui-monospace, monospace); font-size: 11.5px; }
  dd.dim { color: var(--text-secondary); }
</style>
