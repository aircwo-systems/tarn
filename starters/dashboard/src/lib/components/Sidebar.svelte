<script lang="ts" module>
  import type { CpuIcon } from "phosphor-svelte";
  export interface NavigationItem {
    href: string;
    label: string;
    icon: typeof CpuIcon;
    group: string;
    count?: number;
    description?: string;
    details?: readonly { label: string; value: string | number }[];
  }
</script>

<script lang="ts">
  import { onMount } from "svelte";
  import { page } from "$app/state";

  import {
    SquaresFourIcon,
    SunIcon,
    MoonIcon,
    GearIcon,
  } from "phosphor-svelte";
  import { product } from "$lib/config";
  import { getShell } from "$lib/shell.svelte";

  let {
    navigation,
    collapsed = $bindable(false),
    mobileOpen = $bindable(false),
    settingsHref,
  }: {
    navigation: readonly NavigationItem[];
    collapsed?: boolean;
    mobileOpen?: boolean;
    settingsHref?: string;
  } = $props();
  const groups = $derived(
    [...new Set(navigation.map((item) => item.group))].map((label) => ({
      label,
      items: navigation.filter((item) => item.group === label),
    })),
  );
  const activeHref = $derived(
    navigation.find(
      (item) =>
        (item.href.replace(/\/$/, "") || "/") ===
        (page.url.pathname.replace(/\/$/, "") || "/"),
    )?.href,
  );
  let contentEl = $state<HTMLElement | null>(null);
  const itemEls = new Map<string, HTMLElement>();
  let activePillTop = $state(0);
  let activePillHeight = $state(0);
  let activePillVisible = $state(false);
  let activePillAnimate = $state(false);
  let hoverPillTop = $state(0);
  let hoverPillHeight = $state(0);
  let hoverPillVisible = $state(false);
  const shell = getShell();

  function registerItem(node: HTMLElement, href: string) {
    itemEls.set(href, node);
    return {
      destroy() {
        itemEls.delete(href);
      },
    };
  }
  function syncActivePill(animate = true) {
    const el = activeHref ? itemEls.get(activeHref) : undefined;
    if (!contentEl || !el) {
      activePillVisible = false;
      return;
    }
    activePillTop = Math.round(
      el.getBoundingClientRect().top -
        contentEl.getBoundingClientRect().top +
        contentEl.scrollTop,
    );
    activePillHeight = Math.round(el.getBoundingClientRect().height);
    activePillVisible = true;
    activePillAnimate = animate;
  }
  function handleItemPointerEnter(href: string) {
    const el = itemEls.get(href);
    if (href === activeHref || !contentEl || !el) {
      hoverPillVisible = false;
      return;
    }
    hoverPillTop = Math.round(
      el.getBoundingClientRect().top -
        contentEl.getBoundingClientRect().top +
        contentEl.scrollTop,
    );
    hoverPillHeight = Math.round(el.getBoundingClientRect().height);
    hoverPillVisible = true;
  }
  $effect(() => {
    activeHref;
    collapsed;
    width;
    mobileOpen;
    const frame = requestAnimationFrame(() => syncActivePill(true));
    return () => cancelAnimationFrame(frame);
  });

  const DEFAULT_WIDTH = 320;
  const RAIL_WIDTH = 56;
  const MIN_WIDTH = 180;
  const MAX_WIDTH = 520;
  const COLLAPSE_THRESHOLD = 110;
  let width = $state(DEFAULT_WIDTH);
  let resizing = $state(false);
  let releaseToCollapse = $state(false);
  let handleY = $state<number | null>(null);
  let dragMoved = false;
  let dragStartX = 0;
  let dragStartWidth = 0;

  function persistWidth() {
    try {
      localStorage.setItem(product.sidebarStorageKey, String(width));
    } catch {
      /* Width changes still work without storage. */
    }
  }
  function startResize(event: PointerEvent) {
    if (event.button !== 0) return;
    event.preventDefault();
    resizing = true;
    releaseToCollapse = false;
    dragMoved = false;
    dragStartX = event.clientX;
    dragStartWidth = collapsed ? RAIL_WIDTH : width;
    document.body.classList.add("dashboard-resizing");
    window.addEventListener("pointermove", onResizeMove);
    window.addEventListener("pointerup", stopResize);
    window.addEventListener("pointercancel", stopResize);
    window.addEventListener("blur", stopResize);
  }
  function onResizeMove(event: PointerEvent) {
    const next = dragStartWidth + event.clientX - dragStartX;
    if (Math.abs(event.clientX - dragStartX) > 3) dragMoved = true;
    releaseToCollapse = next < COLLAPSE_THRESHOLD;
    if (releaseToCollapse) return;
    if (collapsed) collapsed = false;
    width = Math.round(Math.max(MIN_WIDTH, Math.min(MAX_WIDTH, next)));
    syncActivePill(false);
  }
  function stopResize() {
    if (!resizing) return;
    resizing = false;
    document.body.classList.remove("dashboard-resizing");
    window.removeEventListener("pointermove", onResizeMove);
    window.removeEventListener("pointerup", stopResize);
    window.removeEventListener("pointercancel", stopResize);
    window.removeEventListener("blur", stopResize);
    if (releaseToCollapse) collapsed = true;
    else persistWidth();
    releaseToCollapse = false;
  }
  function saveSidebarWidth(next: number) {
    width = Math.max(MIN_WIDTH, Math.min(MAX_WIDTH, next));
    persistWidth();
  }
  function resetWidth() {
    width = DEFAULT_WIDTH;
    collapsed = false;
    persistWidth();
  }
  function trackHandle(event: PointerEvent) {
    if (!resizing)
      handleY = Math.max(48, Math.min(window.innerHeight - 48, event.clientY));
  }
  function toggleTheme() {
    shell.setTheme(shell.state.theme === "dark" ? "light" : "dark");
  }
  onMount(() => {
    try {
      const stored = Number(localStorage.getItem(product.sidebarStorageKey));
      if (stored >= MIN_WIDTH && stored <= MAX_WIDTH) width = stored;
    } catch {
      /* Use the default width. */
    }
    const observer = new ResizeObserver(() => syncActivePill(false));
    if (contentEl) observer.observe(contentEl);
    for (const item of itemEls.values()) observer.observe(item);
    syncActivePill(false);
    return () => {
      observer.disconnect();
      stopResize();
    };
  });
</script>

<aside
  class="sidebar"
  class:collapsed={collapsed && !mobileOpen}
  class:mobile-open={mobileOpen}
  class:resizing
  style:--sidebar-w="{width}px"
  aria-label="Workspace navigation"
>
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div
    class="sidebar-resizer"
    role="separator"
    aria-orientation="vertical"
    aria-label="Resize sidebar"
    aria-valuenow={collapsed ? RAIL_WIDTH : width}
    aria-valuemin={RAIL_WIDTH}
    aria-valuemax={MAX_WIDTH}
    tabindex="0"
    title="Drag to resize, double click to reset, arrow keys to adjust"
    onpointerdown={startResize}
    onpointermove={trackHandle}
    ondblclick={resetWidth}
    onkeydown={(event) => {
      if (event.key === "ArrowLeft") {
        event.preventDefault();
        if (width - 10 < MIN_WIDTH) collapsed = true;
        else if (!collapsed) saveSidebarWidth(width - 10);
      } else if (event.key === "ArrowRight") {
        event.preventDefault();
        if (collapsed) collapsed = false;
        else saveSidebarWidth(width + 10);
      } else if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        resetWidth();
      }
    }}
  >
    <div class="resizer-line"></div>
    <div
      class="resizer-pill-handle"
      style:top={handleY === null ? undefined : `${handleY}px`}
    ></div>
    <div
      class="resizer-width-badge"
      class:collapse-hint={releaseToCollapse}
      style:top={handleY === null ? undefined : `${handleY}px`}
    >
      {releaseToCollapse ? "Release to collapse" : `${width}px`}
    </div>
  </div>
  <div class="sidebar-inner">
    <div class="sidebar-header">
      <div class="brand-row">
        <div class="brand-meta">
          <div class="brand-icon" aria-hidden="true">
            <SquaresFourIcon size={22} weight="duotone" />
          </div>
          <div class="brand-titles">
            <span class="brand-sub">{product.name}</span><span
              class="brand-title">{product.subtitle}</span
            >
          </div>
        </div>
      </div>
    </div>
    <nav
      class="sidebar-content"
      aria-label="Main navigation"
      bind:this={contentEl}
      onpointerleave={() => (hoverPillVisible = false)}
    >
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
      {#each groups as group (group.label)}
        {@const count = group.items.reduce(
          (sum, item) => sum + (item.count ?? 0),
          0,
        )}
        <div class="nav-section">
          <div class="nav-section-title">
            <span>{group.label}</span>{#if count}<span class="nav-section-count"
                >{count}</span
              >{/if}
          </div>
          {#each group.items as item (item.href)}
            {@const active = item.href === activeHref}
            <a
              class="nav-item"
              class:active
              href={item.href}
              use:registerItem={item.href}
              aria-current={active ? "page" : undefined}
              aria-label={item.label}
              title={collapsed ? item.label : undefined}
              onfocus={() => handleItemPointerEnter(item.href)}
              onblur={() => (hoverPillVisible = false)}
              onclick={() => {
                hoverPillVisible = false;
                mobileOpen = false;
              }}
              onpointerenter={() => handleItemPointerEnter(item.href)}
            >
              <span class="nav-item-left"
                ><item.icon
                  size={15}
                  weight={active ? "bold" : "regular"}
                  class="nav-item-icon"
                  aria-hidden="true"
                /><span class="nav-item-label">{item.label}</span></span
              >
              {#if item.count !== undefined}<span class="nav-item-right"
                  ><span class="nav-badge">{item.count}</span></span
                >{/if}
              {#if item.description || item.details?.length}
                <span class="nav-item-information">
                  <span class="nav-information-inner">
                    {#if item.description}<span class="nav-description"
                        >{item.description}</span
                      >{/if}
                    {#if item.details?.length}
                      <span class="nav-details">
                        {#each item.details as detail (detail.label)}
                          <span class="nav-detail"
                            ><span>{detail.label}</span><b>{detail.value}</b
                            ></span
                          >
                        {/each}
                      </span>
                    {/if}
                  </span>
                </span>
              {/if}
            </a>
          {/each}
        </div>
      {/each}
    </nav>
    <div class="sidebar-footer">
      <div class="footer-left">
        <span class="account-pill-btn"
          ><span class="account-label">{product.environment}</span></span
        >
      </div>
      <div class="footer-actions">
        <button
          class="footer-btn"
          onclick={toggleTheme}
          aria-label={shell.state.theme === "dark"
            ? "Switch to light mode"
            : "Switch to dark mode"}
          title={shell.state.theme === "dark"
            ? "Switch to light mode"
            : "Switch to dark mode"}
          >{#if shell.state.theme === "dark"}<SunIcon
              size={14}
              aria-hidden="true"
            />{:else}<MoonIcon size={14} aria-hidden="true" />{/if}</button
        >{#if settingsHref}<a
            class="footer-btn"
            href={settingsHref}
            aria-label="Settings"
            title="Settings"
            aria-current={page.url.pathname.replace(/\/$/, "") ===
            settingsHref.replace(/\/$/, "")
              ? "page"
              : undefined}><GearIcon size={14} aria-hidden="true" /></a
          >{/if}
      </div>
    </div>
  </div>
</aside>

<style>
  .sidebar {
    width: var(--sidebar-w, 320px);
    height: 100dvh;
    background: var(--canvas);
    color: var(--ink);
    display: flex;
    flex-direction: column;
    user-select: none;
    flex-shrink: 0;
    position: relative;
    z-index: 20;
    font-family: var(
      --font-ui,
      -apple-system,
      BlinkMacSystemFont,
      "Segoe UI",
      Roboto,
      sans-serif
    );
    transition:
      width 200ms var(--ease-out),
      background-color 150ms ease;
  }

  .sidebar.resizing {
    transition: none;
  }

  .sidebar.collapsed {
    width: 56px;
  }

  .sidebar-inner {
    width: 100%;
    min-width: 0;
    container: sidebar / inline-size;
    height: 100%;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    flex-shrink: 0;
    transition: opacity 140ms ease;
  }

  .sidebar.collapsed .sidebar-inner {
    width: 56px;
  }

  :global(body.dashboard-resizing) {
    cursor: col-resize !important;
    user-select: none !important;
    -webkit-user-select: none !important;
  }

  .sidebar-resizer {
    position: absolute;
    top: 0;
    bottom: 0;
    right: -12px;
    width: 24px;
    cursor: col-resize;
    z-index: 40;
    touch-action: none;
  }

  .sidebar-resizer:focus-visible {
    outline: none;
  }

  .sidebar-resizer:focus-visible .resizer-line {
    background: var(--focus);
    width: 2px;
  }

  .sidebar-resizer:focus-visible .resizer-pill-handle {
    opacity: 1;
    border-color: var(--focus);
  }

  .resizer-line {
    position: absolute;
    top: 23px;
    bottom: 23px;
    left: 12px;
    width: 1px;
    border-radius: 1px;
    background: transparent;
    pointer-events: none;
    transition: background 140ms ease;
  }

  .sidebar-resizer:hover .resizer-line {
    background: var(--line-strong);
  }

  .sidebar.resizing .resizer-line {
    background: var(--focus);
  }

  .resizer-pill-handle {
    position: absolute;
    left: 10px;
    top: 50%;
    transform: translateY(-50%) scale(0.95);
    width: 4px;
    height: 44px;
    border-radius: 9999px;
    background: var(--ink-tertiary);
    opacity: 0;
    pointer-events: none;
    transition:
      opacity 140ms ease,
      transform 140ms ease,
      background 100ms ease;
    z-index: 5;
  }

  .sidebar-resizer:hover .resizer-pill-handle,
  .sidebar.resizing .resizer-pill-handle {
    opacity: 1;
    transform: translateY(-50%) scale(1);
  }

  .sidebar-resizer:hover .resizer-pill-handle {
    background: var(--ink-secondary);
  }

  .sidebar.resizing .resizer-pill-handle {
    background: var(--ink);
    box-shadow: 0 0 6px rgba(255, 255, 255, 0.35);
  }

  .resizer-width-badge {
    position: absolute;
    left: 24px;
    top: 50%;
    transform: translateY(-50%);
    background: var(--nav-active);
    border: 1px solid var(--line-strong);
    color: var(--ink);
    font-family: var(--font-data, monospace);
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
    color: var(--danger);
  }

  .sidebar-header {
    padding: 12px 10px 8px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    border-bottom: 1px solid var(--line);
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
    color: var(--ink);
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
    color: var(--ink-tertiary);
    text-transform: uppercase;
  }

  .brand-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--ink);
    letter-spacing: -0.01em;
    white-space: nowrap;
  }

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
    background: var(--line);
    border-radius: 2px;
  }

  .nav-hover-pill {
    position: absolute;
    top: 0;
    left: 8px;
    right: 8px;
    border-radius: 5px;
    background: var(--hover-fill);
    border: 1px solid var(--line);
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
    background: var(--nav-active);
    border: 1px solid var(--line-strong);
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
    content: "";
    position: absolute;
    left: 0;
    top: 6px;
    bottom: 6px;
    width: 2.5px;
    border-radius: 2px 0 0 2px;
    background: var(--ink);
    opacity: 0.9;
  }

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
    color: var(--ink-tertiary);
    text-transform: uppercase;
    padding: 4px 8px 3px;
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .nav-section-count {
    font-family: var(--font-data, monospace);
    font-size: 10px;
    color: var(--ink-tertiary);
    font-weight: 500;
  }

  .nav-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    padding: 5px 8px;
    border-radius: 5px;
    color: var(--ink-secondary);
    text-decoration: none;
    cursor: pointer;
    font-size: 12.5px;
    font-weight: 450;
    position: relative;
    background: transparent;
    border: 1px solid transparent;
    transition:
      color 100ms ease,
      transform 80ms ease;
    outline: none;
    width: 100%;
    text-align: left;
    z-index: 3;
  }

  .nav-item:focus-visible {
    box-shadow: 0 0 0 1.5px var(--focus);
  }

  .nav-item:active {
    transform: scale(0.985);
  }

  .nav-item:hover {
    color: var(--ink);
  }

  .nav-item.active {
    color: var(--ink);
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
    color: var(--ink-tertiary);
    flex-shrink: 0;
    transition: color 100ms ease;
  }

  :global(.sidebar .nav-item:hover .nav-item-icon) {
    color: var(--ink-secondary);
  }

  :global(.sidebar .nav-item.active .nav-item-icon) {
    color: var(--ink);
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
    font-family: var(--font-data, monospace);
    font-size: 11px;
    font-weight: 500;
    color: var(--ink-tertiary);
    padding: 0 4px;
    font-variant-numeric: tabular-nums;
    transition: color 100ms ease;
  }

  .nav-item:hover .nav-badge {
    color: var(--ink-secondary);
  }

  .nav-item.active .nav-badge {
    color: var(--ink);
    font-weight: 500;
  }

  .sidebar-footer {
    padding: 8px 10px 10px;
    border-top: 1px solid var(--line);
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
    position: relative;
    z-index: 10;
    background: var(--canvas);
    transition:
      background-color 150ms ease,
      border-color 150ms ease;
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
    background: var(--surface-raised);
    border: 1px solid var(--line);
    color: var(--ink-secondary);
    font-size: 11px;
    font-family: var(--font-data, monospace);
    transition:
      background 100ms ease,
      border-color 100ms ease,
      color 100ms ease;
    min-width: 0;
    max-width: 90px;
  }

  .account-pill-btn:hover {
    background: var(--nav-active);
    border-color: var(--line-strong);
    color: var(--ink);
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
    color: var(--ink-tertiary) !important;
    display: flex !important;
    align-items: center !important;
    justify-content: center !important;
    cursor: pointer !important;
    padding: 0 !important;
    transition:
      color 100ms ease,
      background-color 100ms ease,
      border-color 100ms ease !important;
    flex-shrink: 0 !important;
  }

  :global(.sidebar .footer-btn.active) {
    color: var(--ink);
    background: var(--hover-fill);
  }
  :global(.sidebar .footer-btn[aria-current="page"]) {
    color: var(--ink) !important;
    position: relative;
  }
  :global(.sidebar.collapsed .footer-btn[aria-current="page"]::after) {
    content: "";
    position: absolute;
    left: 50%;
    bottom: 2px;
    transform: translateX(-50%);
    width: 16px;
    height: 3px;
    border-radius: 999px;
    background: var(--ink);
  }

  :global(.sidebar .footer-btn:hover) {
    color: var(--ink) !important;
    background: var(--surface-raised) !important;
    border-color: var(--line) !important;
  }

  .account-pill-btn {
    max-width: 180px;
  }
  .sidebar-footer a {
    text-decoration: none;
  }
  .nav-item-information {
    display: grid;
    grid-template-rows: 0fr;
    flex-basis: 100%;
    overflow: hidden;
    transition: grid-template-rows 220ms var(--ease-out);
  }
  .nav-information-inner {
    min-height: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    gap: 8px;
    font-weight: 400;
  }
  .nav-description {
    color: var(--ink-secondary);
    font-size: 11px;
    line-height: 1.5;
  }
  .nav-details {
    display: flex;
    flex-direction: column;
    gap: 4px;
    border-top: 1px solid var(--line);
    padding-top: 6px;
  }
  .nav-detail {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    font-size: 10.5px;
    color: var(--ink-tertiary);
  }
  .nav-detail b {
    color: var(--ink);
    font: 10.5px var(--font-data);
  }
  @container sidebar (min-width: 296px) {
    .nav-item {
      padding: 9px 10px;
    }
    .nav-item-information {
      grid-template-rows: 1fr;
      padding-left: 23px;
    }
    .nav-information-inner {
      padding-top: 7px;
    }
    .nav-section {
      gap: 5px;
    }
  }
  .sidebar.collapsed .brand-row,
  .sidebar.collapsed .brand-meta {
    justify-content: center;
    padding: 0;
  }
  .sidebar.collapsed .sidebar-header {
    padding: 18px 8px 14px;
  }
  .sidebar.collapsed .brand-titles,
  .sidebar.collapsed .nav-section-title,
  .sidebar.collapsed .nav-item-label,
  .sidebar.collapsed .nav-item-right,
  .sidebar.collapsed .nav-item-information,
  .sidebar.collapsed .footer-left {
    display: none;
  }
  .sidebar.collapsed .sidebar-content {
    padding: 10px 8px;
    gap: 12px;
  }
  .sidebar.collapsed .nav-section + .nav-section {
    border-top: 1px solid var(--line);
    padding-top: 12px;
  }
  .sidebar.collapsed .nav-item {
    width: 40px;
    height: 52px;
    margin: 0 auto;
    padding: 0;
    justify-content: center;
  }
  .sidebar.collapsed .nav-item-left {
    justify-content: center;
  }
  :global(.sidebar.collapsed .nav-item .nav-item-icon) {
    width: 18px;
    height: 18px;
  }
  .sidebar.collapsed .nav-active-pill {
    display: none;
  }
  .sidebar.collapsed .nav-hover-pill {
    left: 8px;
    right: 8px;
  }
  .sidebar.collapsed .nav-item.active::after {
    content: "";
    position: absolute;
    bottom: 3px;
    left: 50%;
    transform: translateX(-50%);
    width: 16px;
    height: 3px;
    border-radius: 999px;
    background: var(--ink);
  }
  .sidebar.collapsed .sidebar-footer {
    justify-content: center;
    padding: 10px 8px;
  }
  .sidebar.collapsed .footer-actions {
    flex-direction: column;
    gap: 8px;
  }
  :global(.sidebar.collapsed .footer-btn) {
    width: 40px !important;
    height: 40px !important;
  }
  @media (max-width: 640px) {
    .sidebar,
    .sidebar.collapsed {
      display: none;
      width: 100%;
      height: auto;
    }
    .sidebar.mobile-open {
      display: flex;
    }
    .sidebar-inner {
      width: 100%;
      min-width: 0;
      max-height: 50dvh;
    }
    .sidebar-content {
      gap: 8px;
    }
    .nav-item {
      min-height: 44px;
    }
    .nav-item-information {
      display: none;
    }
    .sidebar-resizer {
      display: none;
    }
    :global(.sidebar .footer-btn) {
      width: 44px !important;
      height: 44px !important;
    }
  }
</style>
