<script lang="ts" module>
  import type { Tone } from "./StatusBadge.svelte";

  export interface ResourceMenuItem {
    id: string;
    label: string;
    description?: string;
    badge?: string;
    tone?: Tone;
  }
</script>

<script lang="ts">
  import type { Snippet } from "svelte";
  import { SidebarSimpleIcon } from "phosphor-svelte";

  let {
    label,
    items,
    selectedId,
    onselect,
    children,
    collapsed = $bindable(false),
    emptyMessage = "No matching resources.",
  }: {
    label: string;
    items: readonly ResourceMenuItem[];
    selectedId: string | null;
    onselect: (id: string) => void;
    children: Snippet;
    collapsed?: boolean;
    emptyMessage?: string;
  } = $props();

  const id = $props.id();
  const activeIndex = $derived(
    items.findIndex((item) => item.id === selectedId),
  );
  let tabs = $state<HTMLDivElement | null>(null);

  $effect(() => {
    if (collapsed) {
      tabs
        ?.querySelectorAll<HTMLButtonElement>("[role='tab']")
        [activeIndex]?.scrollIntoView({ block: "nearest", inline: "nearest" });
    }
  });

  function move(event: KeyboardEvent, index: number) {
    let next: number;
    switch (event.key) {
      case "ArrowRight":
      case "ArrowDown":
        next = (index + 1) % items.length;
        break;
      case "ArrowLeft":
      case "ArrowUp":
        next = (index - 1 + items.length) % items.length;
        break;
      case "Home":
        next = 0;
        break;
      case "End":
        next = items.length - 1;
        break;
      default:
        return;
    }
    const item = items[next];
    if (!item) return;
    event.preventDefault();
    onselect(item.id);
    tabs?.querySelectorAll<HTMLButtonElement>("[role='tab']")[next]?.focus();
  }
</script>

<div class="resource-layout" class:collapsed>
  <div class="resource-menu">
    <button
      type="button"
      class="list-toggle"
      aria-label="{collapsed
        ? 'Expand'
        : 'Collapse'} {label.toLowerCase()} list"
      aria-expanded={!collapsed}
      aria-controls="{id}-items"
      onclick={() => (collapsed = !collapsed)}
    >
      <SidebarSimpleIcon
        size={13}
        weight={collapsed ? "fill" : "regular"}
        aria-hidden="true"
      />
      <span>{label}</span><span class="count">{items.length}</span>
    </button>
    <div
      id="{id}-items"
      class="resource-items"
      role="tablist"
      aria-label="{label} switcher"
      aria-orientation={collapsed ? "horizontal" : "vertical"}
      bind:this={tabs}
    >
      {#each items as item, index (item.id)}
        <button
          type="button"
          id="{id}-tab-{index}"
          role="tab"
          class="resource-item"
          class:selected={item.id === selectedId}
          data-tone={item.tone ?? "neutral"}
          aria-selected={item.id === selectedId}
          aria-controls="{id}-detail"
          tabindex={index === (activeIndex < 0 ? 0 : activeIndex) ? 0 : -1}
          onclick={() => onselect(item.id)}
          onkeydown={(event) => move(event, index)}
        >
          <span class="item-heading">
            <span class="status-dot" aria-hidden="true"></span>
            <span class="item-label">{item.label}</span>
            {#if item.badge}<span class="item-badge">{item.badge}</span>{/if}
          </span>
          {#if item.description}<span class="item-description"
              >{item.description}</span
            >{/if}
        </button>
      {:else}
        <p class="empty-message">{emptyMessage}</p>
      {/each}
    </div>
  </div>
  <div
    id="{id}-detail"
    class="resource-detail"
    role="tabpanel"
    aria-labelledby={activeIndex >= 0 ? `${id}-tab-${activeIndex}` : undefined}
    aria-label={activeIndex < 0 ? `${label} details` : undefined}
    tabindex="0"
  >
    {@render children()}
  </div>
</div>

<style>
  .resource-layout {
    display: grid;
    grid-template-columns: 248px minmax(0, 1fr);
    align-items: start;
    gap: 28px;
  }
  .resource-menu,
  .resource-detail {
    min-width: 0;
  }
  .resource-menu {
    border-right: 1px solid var(--line);
    padding-right: 20px;
  }
  .list-toggle {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 30px;
    padding: 0 10px;
    border: 1px solid var(--line);
    border-radius: var(--radius-control);
    background: var(--surface);
    color: var(--ink-secondary);
    font-size: 11.5px;
    white-space: nowrap;
    transition:
      background 120ms,
      color 120ms;
  }
  .list-toggle:hover {
    background: var(--surface-hover);
    color: var(--ink);
  }
  .list-toggle:active {
    background: var(--surface-raised);
  }
  .count {
    padding: 1px 5px;
    border: 1px solid var(--line);
    border-radius: 5px;
    background: var(--surface-raised);
    color: var(--ink-tertiary);
    font: 10px var(--font-data);
  }
  .resource-items {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 8px 4px;
    margin: 4px -4px 0;
    max-height: 480px;
    overflow-y: auto;
  }
  .resource-item {
    --tone: var(--ink-secondary);
    flex-shrink: 0;
    padding: 10px;
    border: 1px solid transparent;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--ink-secondary);
    text-align: left;
    transition:
      background 120ms,
      border-color 120ms,
      color 120ms;
  }
  .resource-item:hover {
    background: var(--surface-hover);
    color: var(--ink);
  }
  .resource-item:active,
  .resource-item.selected {
    background: var(--surface-raised);
    border-color: var(--line-strong);
    color: var(--ink);
  }
  .resource-item.selected .item-label {
    font-weight: 600;
  }
  [data-tone="success"] {
    --tone: var(--success);
  }
  [data-tone="warning"] {
    --tone: var(--warning);
  }
  [data-tone="danger"] {
    --tone: var(--danger);
  }
  [data-tone="info"] {
    --tone: var(--info);
  }
  .item-heading {
    display: flex;
    align-items: center;
    gap: 6px;
    white-space: nowrap;
  }
  .status-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    flex-shrink: 0;
    background: var(--tone);
  }
  .item-label {
    font: 11.5px var(--font-data);
    min-width: 0;
    max-width: 180px;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .item-badge {
    margin-left: auto;
    color: var(--tone);
    font: 10px var(--font-data);
  }
  .item-description {
    display: block;
    margin: 4px 0 0 12px;
    color: var(--ink-tertiary);
    font-size: 11px;
  }
  .empty-message {
    padding: 8px;
    color: var(--ink-tertiary);
    font-size: 12px;
  }
  .collapsed {
    grid-template-columns: minmax(0, 1fr);
    gap: 20px;
  }
  .collapsed .resource-menu {
    display: flex;
    align-items: center;
    gap: 12px;
    border: 0;
    padding: 0;
  }
  .collapsed .list-toggle {
    flex-shrink: 0;
  }
  .collapsed .resource-items {
    flex: 1;
    flex-direction: row;
    gap: 6px;
    min-width: 0;
    margin: 0;
    padding: 6px;
    overflow-x: auto;
    scrollbar-width: none;
    scroll-padding: 6px;
  }
  .collapsed .resource-items::-webkit-scrollbar {
    display: none;
  }
  .collapsed .resource-item {
    height: 30px;
    padding: 0 10px;
    border-color: var(--line);
    background: var(--surface);
  }
  .collapsed .resource-item:hover {
    background: var(--surface-hover);
    border-color: var(--line-strong);
  }
  .collapsed .resource-item.selected {
    background: var(--surface-raised);
    border-color: var(--line-strong);
  }
  .collapsed .item-description {
    display: none;
  }
  @media (max-width: 1000px) {
    .resource-layout {
      grid-template-columns: minmax(0, 1fr);
    }
    .resource-menu {
      border-right: 0;
      border-bottom: 1px solid var(--line);
      padding: 0 0 16px;
    }
  }
  @media (max-width: 640px) {
    .collapsed .resource-menu {
      flex-direction: column;
      align-items: stretch;
      gap: 4px;
    }
    .collapsed .list-toggle {
      align-self: start;
    }
    .list-toggle,
    .resource-item,
    .collapsed .resource-item {
      min-height: 44px;
    }
    .collapsed .resource-items {
      flex: auto;
    }
  }
</style>
