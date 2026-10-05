<script lang="ts" module>
  export interface Column {
    key: string;
    label: string;
    hideOnMobile?: boolean;
  }
</script>

<script lang="ts" generics="Row extends { id: string }">
  import type { Snippet } from "svelte";
  let {
    label,
    rows,
    columns,
    cell,
    selectedId,
    onselect,
  }: {
    label: string;
    rows: readonly Row[];
    columns: readonly Column[];
    cell: Snippet<[Row, Column]>;
    selectedId?: string;
    onselect?: (row: Row) => void;
  } = $props();
</script>

<div class="table-wrap">
  <table>
    <caption class="sr-only">{label}</caption>
    <thead
      ><tr
        >{#each columns as column (column.key)}<th
            scope="col"
            class:mobile-hidden={column.hideOnMobile}>{column.label}</th
          >{/each}</tr
      ></thead
    >
    <tbody>
      {#each rows as row (row.id)}
        <tr class:selected={selectedId === row.id}>
          {#each columns as column, index (column.key)}
            <td class:mobile-hidden={column.hideOnMobile}>
              {#if index === 0 && onselect}
                <button
                  class="row-select"
                  onclick={() => onselect?.(row)}
                  aria-pressed={selectedId === row.id}
                  >{@render cell(row, column)}</button
                >
              {:else}{@render cell(row, column)}{/if}
            </td>
          {/each}
        </tr>
      {/each}
    </tbody>
  </table>
</div>

<style>
  .table-wrap {
    overflow-x: auto;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    text-align: left;
  }
  th {
    color: var(--ink-tertiary);
    font: 10px/1.5 var(--font-data);
    text-transform: uppercase;
    letter-spacing: 0.065em;
    padding: var(--space-3) var(--space-3);
    background: var(--surface-raised);
    border-block: 1px solid var(--line);
    white-space: nowrap;
  }
  td {
    padding: 8px var(--space-3);
    font-size: 12px;
    border-bottom: 1px solid var(--line);
    color: var(--ink-secondary);
  }
  th:first-child,
  td:first-child {
    padding-left: var(--space-3);
  }
  tr {
    transition: background var(--duration-fast);
  }
  tbody tr:hover {
    background: var(--surface-raised);
  }
  tr.selected {
    background: var(--nav-active);
  }
  tr.selected td:first-child {
    box-shadow: inset 2px 0 var(--ink);
  }
  .row-select {
    display: block;
    width: 100%;
    text-align: left;
    border: 0;
    background: transparent;
    padding: 4px 0;
    color: var(--ink);
    font-size: inherit;
  }
  @media (max-width: 640px) {
    .mobile-hidden {
      display: none;
    }
    td {
      padding-block: var(--space-3);
    }
    .row-select {
      min-height: 44px;
    }
  }
</style>
