<script lang="ts">
  import { GearIcon } from "phosphor-svelte";
  import TarnLogo from "$lib/components/common/tarn-logo.svelte";
  import ThemeToggle from "$lib/components/layout/theme-toggle.svelte";
  import type { Component } from "svelte";
  import { onMount } from "svelte";
  import type { LogPulse } from "$lib/log-pulse.svelte";

  export interface NavItem {
    id: string;
    label: string;
    icon: Component<any>;
    count: number | null;
    /** Recent-activity sparkline + error pill (used by Logs). */
    pulse?: LogPulse;
  }

  export interface NavSection {
    id: string;
    label: string;
    items: NavItem[];
  }

  let {
    navSections,
    activeTab,
    sidebarCollapsed = $bindable(false),
    pollingIntervalSeconds: _pollingIntervalSeconds,
    activeAccountId = "000000000000",
    onSetTab,
    onOpenSettings,
  }: {
    navSections: NavSection[];
    activeTab: string;
    sidebarCollapsed: boolean;
    pollingIntervalSeconds?: number;
    activeAccountId?: string;
    onSetTab: (tab: string) => void;
    onOpenSettings: () => void;
  } = $props();

  const isNonDefaultAccount = $derived(activeAccountId !== "000000000000");

  let contentEl = $state<HTMLElement | null>(null);
  const itemEls = new Map<string, HTMLElement>();

  // ─── Zero-Lag Precision Motion Pills ───
  let activePillTop = $state(0);
  let activePillHeight = $state(0);
  let activePillVisible = $state(false);
  let activePillAnimate = $state(false);

  let hoverPillTop = $state(0);
  let hoverPillHeight = $state(0);
  let hoverPillVisible = $state(false);

  function registerItem(node: HTMLElement, id: string) {
    itemEls.set(id, node);
    if (id === activeTab) {
      requestAnimationFrame(() => syncActivePill(false));
    }
    return {
      update(newId: string) {
        if (newId !== id) {
          itemEls.delete(id);
          itemEls.set(newId, node);
        }
      },
      destroy() {
        itemEls.delete(id);
      },
    };
  }

  function syncActivePill(animate = true) {
    if (!contentEl) return;
    const el = itemEls.get(activeTab);
    if (!el) {
      activePillVisible = false;
      return;
    }
    const cRect = contentEl.getBoundingClientRect();
    const elRect = el.getBoundingClientRect();
    activePillTop = Math.round(elRect.top - cRect.top + contentEl.scrollTop);
    activePillHeight = Math.round(elRect.height);
    activePillVisible = true;
    activePillAnimate = animate;
  }

  function handleItemPointerEnter(id: string) {
    if (id === activeTab || !contentEl) {
      hoverPillVisible = false;
      return;
    }
    const el = itemEls.get(id);
    if (!el) return;
    const cRect = contentEl.getBoundingClientRect();
    const elRect = el.getBoundingClientRect();
    hoverPillTop = Math.round(elRect.top - cRect.top + contentEl.scrollTop);
    hoverPillHeight = Math.round(elRect.height);
    hoverPillVisible = true;
  }

  function handleContentPointerLeave() {
    hoverPillVisible = false;
  }

  function handleItemClick(id: string) {
    hoverPillVisible = false;
    onSetTab(id);
  }

  $effect(() => {
    const _tab = activeTab;
    const _col = sidebarCollapsed;
    const _w = width;
    requestAnimationFrame(() => {
      syncActivePill(true);
    });
  });

  // ─── Resizable / Collapsible Edge ───
  const DEFAULT_WIDTH = 256;
  const MIN_WIDTH = 180;
  const MAX_WIDTH = 520;
  const COLLAPSE_THRESHOLD = 110;
  const WIDTH_KEY = "tarn_sidebar_width";

  const storedWidth = Number(localStorage.getItem(WIDTH_KEY));
  let width = $state(
    storedWidth >= MIN_WIDTH && storedWidth <= MAX_WIDTH ? storedWidth : DEFAULT_WIDTH
  );
  let resizing = $state(false);
  let releaseToCollapse = $state(false);
  let handleY = $state<number | null>(null);
  let dragMoved = false;
  let dragStartX = 0;
  let dragStartWidth = 0;

  function startResize(e: PointerEvent) {
    if (e.button !== 0) return;
    e.preventDefault();
    resizing = true;
    releaseToCollapse = false;
    dragMoved = false;
    dragStartX = e.clientX;
    dragStartWidth = sidebarCollapsed ? 0 : width;
    document.body.classList.add("is-resizing");
    window.addEventListener("pointermove", onResizeMove);
    window.addEventListener("pointerup", stopResize);
  }

  function onResizeMove(e: PointerEvent) {
    const next = dragStartWidth + (e.clientX - dragStartX);
    if (Math.abs(e.clientX - dragStartX) > 3) dragMoved = true;
    releaseToCollapse = next < COLLAPSE_THRESHOLD;
    if (releaseToCollapse) return;
    if (sidebarCollapsed) sidebarCollapsed = false;
    width = Math.round(Math.max(MIN_WIDTH, Math.min(MAX_WIDTH, next)));
    syncActivePill(false);
  }

  function stopResize() {
    resizing = false;
    document.body.classList.remove("is-resizing");
    window.removeEventListener("pointermove", onResizeMove);
    window.removeEventListener("pointerup", stopResize);
    if (releaseToCollapse) {
      sidebarCollapsed = true;
    } else {
      localStorage.setItem(WIDTH_KEY, String(width));
    }
    releaseToCollapse = false;
    requestAnimationFrame(() => syncActivePill(false));
  }

  function resetWidth() {
    width = DEFAULT_WIDTH;
    localStorage.setItem(WIDTH_KEY, String(width));
    sidebarCollapsed = false;
    requestAnimationFrame(() => syncActivePill(true));
  }

  function trackHandle(e: PointerEvent) {
    if (resizing) return;
    handleY = Math.max(48, Math.min(window.innerHeight - 48, e.clientY));
  }

  function onKeydown(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "b") {
      e.preventDefault();
      sidebarCollapsed = !sidebarCollapsed;
    }
  }

  onMount(() => {
    requestAnimationFrame(() => {
      syncActivePill(false);
    });
    const onResize = () => syncActivePill(false);
    window.addEventListener("resize", onResize);
    window.addEventListener("keydown", onKeydown);
    return () => {
      window.removeEventListener("resize", onResize);
      window.removeEventListener("keydown", onKeydown);
      window.removeEventListener("pointermove", onResizeMove);
      window.removeEventListener("pointerup", stopResize);
      document.body.classList.remove("is-resizing");
    };
  });
</script>

<aside
  class="sidebar"
  class:collapsed={sidebarCollapsed}
  class:resizing
  style:--sidebar-w="{width}px"
  aria-label="Rack navigation"
>
  {#if !sidebarCollapsed}
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <div
      class="sidebar-resizer"
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize sidebar"
      aria-valuenow={width}
      aria-valuemin={180}
      aria-valuemax={420}
      tabindex="0"
      title="Drag to resize sidebar (double click to reset, arrow keys to adjust)"
      onpointerdown={startResize}
      onpointermove={trackHandle}
      ondblclick={resetWidth}
      onkeydown={(e) => {
        if (e.key === "ArrowLeft") {
          e.preventDefault();
          width = Math.max(180, width - 10);
          saveSidebarWidth(width);
        } else if (e.key === "ArrowRight") {
          e.preventDefault();
          width = Math.min(420, width + 10);
          saveSidebarWidth(width);
        } else if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          resetWidth();
        }
      }}
    >
      <div class="resizer-line"></div>
      <div class="resizer-pill-handle" style:top={handleY === null ? undefined : `${handleY}px`}></div>
      <div
        class="resizer-width-badge"
        class:collapse-hint={releaseToCollapse}
        style:top={handleY === null ? undefined : `${handleY}px`}
      >
        {releaseToCollapse ? "Release to collapse" : `${width}px`}
      </div>
    </div>
  {/if}

  <div class="sidebar-inner">
    <!-- Header & Brand -->
    <div class="sidebar-header">
      <div class="brand-row">
        <div class="brand-meta">
          <div class="brand-icon" aria-hidden="true">
            <TarnLogo class="h-[22px] w-[22px] shrink-0" color="currentColor" />
          </div>
          <div class="brand-titles">
            <span class="brand-sub">Tarn</span>
            <span class="brand-title">Rack Console</span>
          </div>
        </div>
      </div>
    </div>

    <!-- Navigation Scroll List -->
    <nav
      class="sidebar-content"
      bind:this={contentEl}
      onpointerleave={handleContentPointerLeave}
    >
      <!-- Zero-Lag Motion Indicators from prototype -->
      <div
        class="nav-hover-pill"
        style:transform="translate3d(0, {hoverPillTop}px, 0)"
        style:height="{hoverPillHeight}px"
        style:opacity={hoverPillVisible ? 1 : 0}
        aria-hidden="true"
      ></div>
      <div
        class="nav-active-pill"
        class:no-transition={!activePillAnimate}
        style:transform="translate3d(0, {activePillTop}px, 0)"
        style:height="{activePillHeight}px"
        style:opacity={activePillVisible ? 1 : 0}
        aria-hidden="true"
      ></div>

      {#each navSections as section (section.id)}
        {@const sectionTotal = section.items.reduce((acc, it) => acc + (it.count ?? 0), 0)}
        <div class="nav-section">
          <div class="nav-section-title">
            <span>{section.label}</span>
            {#if sectionTotal > 0}
              <span class="nav-section-count">{sectionTotal}</span>
            {/if}
          </div>

          {#each section.items as item (item.id)}
            {@const Icon = item.icon}
            {@const active = item.id === activeTab}
            <button
              type="button"
              use:registerItem={item.id}
              onclick={() => handleItemClick(item.id)}
              onpointerenter={() => handleItemPointerEnter(item.id)}
              class="nav-item"
              class:active
              aria-current={active ? "page" : undefined}
            >
              <div class="nav-item-left">
                <Icon
                  size={15}
                  class="nav-item-icon"
                  weight={active ? "bold" : "regular"}
                />
                <span class="nav-item-label">{item.label}</span>
              </div>

              {#if item.pulse}
                {@const pulse = item.pulse}
                {@const peak = Math.max(1, ...pulse.bars)}
                <div class="nav-item-right">
                  <span
                    class="nav-pulse"
                    class:live={Date.now() - pulse.lastEventAt < 10_000}
                    title="Log activity, last 60s"
                    aria-hidden="true"
                  >
                    {#each pulse.bars as count, b (b)}
                      <span
                        class="nav-pulse-bar"
                        data-severity={pulse.severity[b]}
                        style:height="{count === 0 ? 1 : 3 + Math.sqrt(count / peak) * 9}px"
                      ></span>
                    {/each}
                  </span>
                  {#if pulse.recentErrors > 0}
                    <span class="status-pill error" title="{pulse.recentErrors} errors in the last 5 minutes">err</span>
                  {:else if pulse.recentWarnings > 0}
                    <span class="status-pill warning" title="{pulse.recentWarnings} warnings in the last 5 minutes">wrn</span>
                  {/if}
                </div>
              {:else if item.count !== null && item.count > 0}
                <div class="nav-item-right">
                  <span class="nav-badge">{item.count}</span>
                </div>
              {/if}
            </button>
          {/each}
        </div>
      {/each}
    </nav>

    <!-- Footer Dock -->
    <div class="sidebar-footer">
      <div class="footer-left">
        {#if isNonDefaultAccount}
          <button
            type="button"
            onclick={onOpenSettings}
            title="Account: {activeAccountId} (Click to switch)"
            class="account-pill-btn"
          >
            <span class="account-label">{activeAccountId}</span>
          </button>
        {/if}
      </div>

      <div class="footer-actions">
        <ThemeToggle class="footer-btn" />
        <button
          type="button"
          onclick={onOpenSettings}
          class="footer-btn"
          class:active={activeTab === "settings"}
          title="Console Settings"
          aria-label="Settings"
        >
          <GearIcon size={14} />
        </button>
      </div>
    </div>
  </div>
</aside>

{#if sidebarCollapsed}
  <button
    type="button"
    class="collapsed-edge-zone"
    title="Click or drag to expand sidebar"
    aria-label="Expand sidebar"
    onclick={() => {
      if (!dragMoved) sidebarCollapsed = false;
    }}
    onpointerdown={startResize}
  >
    <span class="collapsed-edge-pill" aria-hidden="true">
      <svg width="8" height="10" viewBox="0 0 8 10" fill="none">
        <path d="M2.5 2L5.5 5L2.5 8" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
    </span>
  </button>
{/if}

<style>
  /* ─── Ultra-Smooth Modern Sidebar (Matching rack_console prototype) ─── */
  .sidebar {
    width: var(--sidebar-w, 256px);
    height: 100vh;
    background: var(--bg-sidebar);
    color: var(--text-primary);
    display: flex;
    flex-direction: column;
    user-select: none;
    flex-shrink: 0;
    position: relative;
    z-index: 20;
    font-family: var(--font-ui-sans, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif);
    transition: width 200ms var(--ease-snappy), background-color 150ms ease;
  }

  /* Instant tracking while dragging */
  .sidebar.resizing {
    transition: none;
  }

  .sidebar.collapsed {
    width: 0;
    overflow: hidden;
  }

  .sidebar-inner {
    width: var(--sidebar-w, 256px);
    min-width: 180px;
    height: 100%;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    flex-shrink: 0;
    transition: opacity 140ms ease;
  }

  .sidebar.collapsed .sidebar-inner {
    opacity: 0;
    pointer-events: none;
  }

  :global(body.is-resizing) {
    cursor: col-resize !important;
    user-select: none !important;
    -webkit-user-select: none !important;
  }

  /* ─── Resizer Edge ─── */
  .sidebar-resizer {
    position: absolute;
    top: 0;
    bottom: 0;
    right: -24px;
    width: 42px;
    cursor: col-resize;
    z-index: 40;
    touch-action: none;
  }

  .sidebar-resizer:focus-visible {
    outline: none;
  }

  .sidebar-resizer:focus-visible .resizer-line {
    background: var(--border-focus);
    width: 2px;
  }

  .sidebar-resizer:focus-visible .resizer-pill-handle {
    opacity: 1;
    border-color: var(--border-focus);
  }

  /* 1px line flush with the stage's rounded corner tangents (8px margin + 14px radius + 1px) */
  .resizer-line {
    position: absolute;
    top: 23px;
    bottom: 23px;
    left: 17px;
    width: 1px;
    border-radius: 1px;
    background: transparent;
    pointer-events: none;
    transition: background 140ms ease;
  }

  .sidebar-resizer:hover .resizer-line {
    background: rgba(255, 255, 255, 0.16);
  }

  .sidebar.resizing .resizer-line {
    background: rgba(255, 255, 255, 0.3);
  }

  :global(.light) .sidebar-resizer:hover .resizer-line {
    background: rgba(0, 0, 0, 0.12);
  }

  :global(.light) .sidebar.resizing .resizer-line {
    background: rgba(0, 0, 0, 0.22);
  }

  .resizer-pill-handle {
    position: absolute;
    left: 29px;
    top: 50%;
    transform: translateY(-50%) scale(0.95);
    width: 4px;
    height: 44px;
    border-radius: 9999px;
    background: var(--text-tertiary);
    opacity: 0;
    pointer-events: none;
    transition: opacity 140ms ease, transform 140ms ease, background 100ms ease;
    z-index: 5;
  }

  .sidebar-resizer:hover .resizer-pill-handle,
  .sidebar.resizing .resizer-pill-handle {
    opacity: 1;
    transform: translateY(-50%) scale(1);
  }

  .sidebar-resizer:hover .resizer-pill-handle {
    background: var(--text-secondary);
  }

  .sidebar.resizing .resizer-pill-handle {
    background: var(--text-primary);
    box-shadow: 0 0 6px rgba(255, 255, 255, 0.35);
  }

  :global(.light) .sidebar.resizing .resizer-pill-handle {
    box-shadow: 0 0 4px rgba(0, 0, 0, 0.25);
  }

  .resizer-width-badge {
    position: absolute;
    left: 40px;
    top: 50%;
    transform: translateY(-50%);
    background: var(--bg-element);
    border: 1px solid var(--border-default);
    color: var(--text-primary);
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 10.5px;
    padding: 2px 7px;
    border-radius: 4px;
    pointer-events: none;
    white-space: nowrap;
    box-shadow: 0 4px 14px rgba(0, 0, 0, 0.35);
    opacity: 0;
    transition: opacity 120ms ease;
    z-index: 10;
  }

  .sidebar.resizing .resizer-width-badge {
    opacity: 1;
  }

  .resizer-width-badge.collapse-hint {
    color: var(--accent-red);
  }

  /* ─── Collapsed Edge Zone ─── */
  .collapsed-edge-zone {
    position: fixed;
    top: 23px;
    bottom: 23px;
    left: 2px;
    width: 4px;
    padding: 0;
    border: none;
    border-radius: 9999px;
    background: transparent;
    cursor: e-resize;
    z-index: 35;
    touch-action: none;
    transition: background 120ms ease;
  }

  .collapsed-edge-zone:hover {
    background: rgba(255, 255, 255, 0.16);
  }

  :global(.light) .collapsed-edge-zone:hover {
    background: rgba(0, 0, 0, 0.12);
  }

  .collapsed-edge-pill {
    position: absolute;
    left: 1px;
    top: 50%;
    transform: translateY(-50%);
    width: 14px;
    height: 36px;
    border-radius: 0 6px 6px 0;
    background: var(--bg-sidebar);
    border: 1px solid var(--border-default);
    border-left: none;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--text-secondary);
    opacity: 0;
    transition: opacity 120ms ease;
    box-shadow: 2px 0 8px rgba(0, 0, 0, 0.25);
  }

  .collapsed-edge-zone:hover .collapsed-edge-pill {
    opacity: 1;
  }

  /* ─── Header ─── */
  .sidebar-header {
    padding: 12px 10px 8px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    border-bottom: 1px solid var(--border-subtle);
    flex-shrink: 0;
  }

  .brand-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 2px 4px;
  }

  .brand-meta {
    display: flex;
    align-items: center;
    gap: 9px;
    min-width: 0;
  }

  .brand-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    color: var(--text-primary);
  }

  .brand-titles {
    display: flex;
    flex-direction: column;
    line-height: 1.15;
    min-width: 0;
  }

  .brand-sub {
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.08em;
    color: var(--text-tertiary);
    text-transform: uppercase;
  }

  .brand-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    letter-spacing: -0.01em;
    white-space: nowrap;
  }

  /* ─── Navigation Scroll Container ─── */
  .sidebar-content {
    flex: 1;
    overflow-y: auto;
    padding: 6px 8px;
    display: flex;
    flex-direction: column;
    gap: 16px;
    position: relative;
    scrollbar-width: thin;
  }

  .sidebar-content::-webkit-scrollbar {
    width: 4px;
  }

  .sidebar-content::-webkit-scrollbar-thumb {
    background: var(--border-subtle);
    border-radius: 2px;
  }

  /* ─── Zero-Lag Precision Motion Pills (from rack_console prototype) ─── */
  .nav-hover-pill {
    position: absolute;
    top: 0;
    left: 8px;
    right: 8px;
    border-radius: 5px;
    background: var(--bg-element-hover);
    border: 1px solid var(--border-subtle);
    opacity: 0;
    pointer-events: none;
    will-change: transform, height, opacity;
    transform: translate3d(0, 0, 0);
    transition:
      transform 130ms cubic-bezier(0.16, 1, 0.3, 1),
      height 120ms ease,
      opacity 80ms ease;
    z-index: 1;
  }

  .nav-active-pill {
    position: absolute;
    top: 0;
    left: 8px;
    right: 8px;
    border-radius: 5px;
    background: var(--bg-element);
    border: 1px solid var(--border-default);
    pointer-events: none;
    will-change: transform, height;
    transform: translate3d(0, 0, 0);
    transition:
      transform 220ms cubic-bezier(0.16, 1, 0.3, 1),
      height 180ms ease;
    z-index: 2;
  }

  .nav-active-pill.no-transition {
    transition: none !important;
  }

  .nav-active-pill::before {
    content: '';
    position: absolute;
    left: 0;
    top: 6px;
    bottom: 6px;
    width: 2.5px;
    border-radius: 2px 0 0 2px;
    background: var(--text-primary);
    opacity: 0.9;
  }

  /* ─── Navigation Section ─── */
  .nav-section {
    display: flex;
    flex-direction: column;
    gap: 1px;
    position: relative;
    z-index: 3;
  }

  .nav-section-title {
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.06em;
    color: var(--text-tertiary);
    text-transform: uppercase;
    padding: 4px 8px 3px;
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .nav-section-count {
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 10px;
    color: var(--text-tertiary);
    font-weight: 500;
  }

  /* ─── Nav Item ─── */
  .nav-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 5px 8px;
    border-radius: 5px;
    color: var(--text-secondary);
    text-decoration: none;
    cursor: pointer;
    font-size: 12.5px;
    font-weight: 450;
    position: relative;
    background: transparent;
    border: 1px solid transparent;
    transition: color 100ms ease, transform 80ms ease;
    outline: none;
    width: 100%;
    text-align: left;
    z-index: 3;
  }

  .nav-item:focus-visible {
    box-shadow: 0 0 0 1.5px var(--border-focus);
  }

  .nav-item:active {
    transform: scale(0.985);
  }

  .nav-item:hover {
    color: var(--text-primary);
  }

  .nav-item.active {
    color: var(--text-primary);
    font-weight: 500;
  }

  .nav-item-left {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  :global(.sidebar .nav-item .nav-item-icon) {
    width: 15px;
    height: 15px;
    color: var(--text-tertiary);
    flex-shrink: 0;
    transition: color 100ms ease;
  }

  :global(.sidebar .nav-item:hover .nav-item-icon) {
    color: var(--text-secondary);
  }

  :global(.sidebar .nav-item.active .nav-item-icon) {
    color: var(--text-primary);
  }

  .nav-item-label {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    line-height: 1.2;
  }

  .nav-item-right {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-left: auto;
  }

  .nav-badge {
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 11px;
    font-weight: 500;
    color: var(--text-tertiary);
    padding: 0 4px;
    font-variant-numeric: tabular-nums;
    transition: color 100ms ease;
  }

  .nav-item:hover .nav-badge {
    color: var(--text-secondary);
  }

  .nav-item.active .nav-badge {
    color: var(--text-primary);
    font-weight: 500;
  }

  /* ─── Activity pulse (Logs) ─── */
  .nav-pulse {
    display: inline-flex;
    align-items: flex-end;
    gap: 1px;
    height: 12px;
    opacity: 0.55;
    transition: opacity 150ms ease;
  }

  .nav-item:hover .nav-pulse,
  .nav-item.active .nav-pulse,
  .nav-pulse.live {
    opacity: 1;
  }

  .nav-pulse-bar {
    width: 2px;
    border-radius: 1px;
    background: var(--text-tertiary);
    transition: height 320ms var(--ease-snappy), background 150ms ease;
  }

  .nav-pulse.live .nav-pulse-bar:last-child {
    background: var(--accent-green);
  }

  .nav-pulse-bar[data-severity="1"] {
    background: var(--accent-amber);
  }

  .nav-pulse-bar[data-severity="2"] {
    background: var(--accent-red);
  }

  .status-pill {
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    font-size: 10px;
    font-weight: 500;
    padding: 1px 4px;
    border-radius: 3px;
    line-height: 12px;
  }

  .status-pill.error {
    background: var(--accent-red-bg);
    color: var(--accent-red);
    border: 1px solid color-mix(in srgb, var(--accent-red) 22%, transparent);
  }

  .status-pill.warning {
    background: var(--accent-amber-bg);
    color: var(--accent-amber);
    border: 1px solid color-mix(in srgb, var(--accent-amber) 22%, transparent);
  }

  /* ─── Footer Dock ─── */
  .sidebar-footer {
    padding: 8px 10px 10px;
    border-top: 1px solid var(--border-subtle);
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
    position: relative;
    z-index: 10;
    background: var(--bg-sidebar);
    transition: background-color 150ms ease, border-color 150ms ease;
    flex-shrink: 0;
  }

  .footer-left {
    display: flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
    flex: 1;
  }

  .account-pill-btn {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 3px 6px;
    border-radius: 5px;
    background: var(--bg-sidebar-subtle);
    border: 1px solid var(--border-subtle);
    color: var(--text-secondary);
    font-size: 11px;
    font-family: var(--font-ui-mono, var(--font-mono, monospace));
    cursor: pointer;
    transition: background 100ms ease, border-color 100ms ease, color 100ms ease;
    min-width: 0;
    max-width: 90px;
  }

  .account-pill-btn:hover {
    background: var(--bg-element);
    border-color: var(--border-default);
    color: var(--text-primary);
  }

  .account-label {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .footer-actions {
    display: flex;
    align-items: center;
    gap: 3px;
    flex-shrink: 0;
  }

  :global(.sidebar .footer-btn) {
    width: 26px !important;
    height: 26px !important;
    border-radius: 5px !important;
    border: 1px solid transparent !important;
    background: transparent !important;
    color: var(--text-tertiary) !important;
    display: flex !important;
    align-items: center !important;
    justify-content: center !important;
    cursor: pointer !important;
    padding: 0 !important;
    transition: color 100ms ease, background-color 100ms ease, border-color 100ms ease !important;
    flex-shrink: 0 !important;
  }

  :global(.sidebar .footer-btn.active) {
    color: var(--text-primary);
    background: var(--bg-element-hover);
  }

  :global(.sidebar .footer-btn:hover) {
    color: var(--text-primary) !important;
    background: var(--bg-sidebar-subtle) !important;
    border-color: var(--border-subtle) !important;
  }
</style>
