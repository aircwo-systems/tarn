<script lang="ts">
  import type { Snippet } from "svelte";

  let {
    selected = false,
    title,
    sub = "",
    mono = false,
    onclick,
    trailing,
  }: {
    selected?: boolean;
    title: string;
    sub?: string;
    mono?: boolean;
    onclick?: () => void;
    trailing?: Snippet;
  } = $props();
</script>

<button type="button" class="rc-list-row" class:selected aria-pressed={selected} {onclick}>
  <span class="main">
    <span class="title" class:mono title={title}>{title}</span>
    {#if sub}<span class="sub">{sub}</span>{/if}
  </span>
  {#if trailing}<span class="trailing">{@render trailing()}</span>{/if}
</button>

<style>
  .rc-list-row {
    position: relative; display: flex; align-items: center; gap: 10px; width: 100%;
    padding: 7px 10px; border-radius: 8px; border: 1px solid transparent; text-align: left;
    transition: background 120ms ease, border-color 120ms ease, padding-left 220ms var(--ease-snappy), transform 120ms ease;
  }
  .rc-list-row:hover { background: var(--bg-element-hover); }
  .rc-list-row:active { transform: scale(0.99); }
  .rc-list-row.selected { padding-left: 18px; background: var(--bg-element); border-color: var(--border-default); }
  .rc-list-row.selected::before {
    content: ""; position: absolute; left: 6px; top: 10px; bottom: 10px; width: 2.5px;
    border-radius: 2px; background: var(--text-primary); opacity: 0.9;
  }
  .rc-list-row:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 1px; }
  .main { display: flex; flex-direction: column; gap: 1px; flex: 1; min-width: 0; }
  .title { font-size: 12.5px; color: var(--text-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; transition: color 120ms ease; }
  .title.mono { font-family: var(--font-mono, ui-monospace, monospace); font-size: 12px; }
  .rc-list-row:hover .title, .selected .title { color: var(--text-primary); }
  .sub { font-size: 10.5px; color: var(--text-tertiary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .trailing { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }
  @media (prefers-reduced-motion: reduce) { .rc-list-row { transition: none; } }
</style>
