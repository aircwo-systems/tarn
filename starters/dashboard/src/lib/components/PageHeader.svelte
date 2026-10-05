<script lang="ts">
  import type { Snippet } from "svelte";
  import { SidebarSimpleIcon } from "phosphor-svelte";
  import { getShell } from "$lib/shell.svelte";
  let {
    title,
    description = "",
    dataLabel = "Sample data",
    actions,
  }: {
    title: string;
    description?: string;
    dataLabel?: string;
    actions?: Snippet;
  } = $props();
  const shell = getShell();
</script>

<header class="section-header">
  <div class="heading">
    <button
      class="sidebar-toggle"
      onclick={shell.toggle}
      aria-label={shell.state.narrow
        ? "Toggle navigation"
        : shell.state.collapsed
          ? "Expand sidebar"
          : "Collapse sidebar"}
      aria-expanded={shell.state.narrow
        ? shell.state.mobileOpen
        : !shell.state.collapsed}
      title="Toggle sidebar (⌘/Ctrl+B)"
      ><SidebarSimpleIcon
        size={14}
        weight={shell.state.collapsed ? "regular" : "fill"}
        aria-hidden="true"
      /></button
    >
    <div>
      <h1>{title}</h1>
      {#if description}<p>{description}</p>{/if}
    </div>
  </div>
  <div class="actions">
    {#if actions}{@render actions()}{:else if dataLabel}<span class="data-label"
        ><span aria-hidden="true"></span>{dataLabel}</span
      >{/if}
  </div>
</header>

<style>
  .section-header {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    align-items: center;
    gap: 12px 16px;
    border-bottom: 1px solid var(--line);
    padding-bottom: 16px;
  }
  .heading {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .sidebar-toggle {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    border: 1px solid transparent;
    border-radius: 6px;
    color: var(--ink-tertiary);
    background: transparent;
    transition:
      color 120ms,
      background 120ms;
    flex-shrink: 0;
  }
  .sidebar-toggle:hover {
    color: var(--ink);
    background: var(--hover-fill);
    border-color: var(--line);
  }
  h1 {
    font-size: 15px;
    font-weight: 600;
    letter-spacing: -0.01em;
    line-height: 1.4;
  }
  p {
    font-size: 12px;
    color: var(--ink-secondary);
    margin-top: 2px;
  }
  .actions {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }
  .data-label {
    display: flex;
    align-items: center;
    gap: 6px;
    font: 10px var(--font-data);
    color: var(--ink-tertiary);
  }
  .data-label span {
    width: 4px;
    height: 4px;
    background: var(--accent);
    border-radius: 50%;
  }
  @media (max-width: 640px) {
    .sidebar-toggle {
      width: 44px;
      height: 44px;
    }
  }
</style>
