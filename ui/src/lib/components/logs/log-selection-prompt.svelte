<script lang="ts">
  import { FunnelIcon, PlusIcon } from "phosphor-svelte";
  import { fly } from "svelte/transition";
  import { onDestroy, onMount } from "svelte";

  let {
    onFilter,
    onAddToFilter,
    activeFilter = "",
  }: {
    onFilter: (text: string) => void;
    onAddToFilter?: (text: string) => void;
    activeFilter?: string;
  } = $props();

  let visible = $state(false);
  let selectedText = $state("");
  let x = $state(0);
  let y = $state(0);
  let placement = $state<"top" | "bottom">("top");
  let promptEl = $state<HTMLDivElement | null>(null);

  const hasActiveFilter = $derived(Boolean(activeFilter && activeFilter.trim().length > 0));

  const previewText = $derived(
    selectedText.length > 24 ? selectedText.slice(0, 22) + "…" : selectedText,
  );

  function hidePrompt() {
    visible = false;
  }

  function handleSelection() {
    const sel = window.getSelection();
    if (!sel || sel.isCollapsed || sel.rangeCount === 0) {
      hidePrompt();
      return;
    }

    const rawText = sel.toString().trim();
    if (!rawText || rawText.length > 300) {
      hidePrompt();
      return;
    }

    const cleanText = rawText.replace(/\r?\n\s*/g, " ").trim();
    if (!cleanText) {
      hidePrompt();
      return;
    }

    // Do not prompt if the selection matches the currently active filter exactly
    if (activeFilter && cleanText.toLowerCase() === activeFilter.trim().toLowerCase()) {
      hidePrompt();
      return;
    }

    const anchorNode = sel.anchorNode;
    const focusNode = sel.focusNode;
    const anchorEl = anchorNode instanceof Element ? anchorNode : anchorNode?.parentElement;
    const focusEl = focusNode instanceof Element ? focusNode : focusNode?.parentElement;

    if (!anchorEl || !focusEl) {
      hidePrompt();
      return;
    }

    // Don't trigger if user is selecting inside an input, textarea, or button
    if (anchorEl.closest("input, textarea, button") || focusEl.closest("input, textarea, button")) {
      hidePrompt();
      return;
    }

    // Ensure selection originated from inside a log content container
    const isInsideLog = anchorEl.closest(".log-stream, .rc-detail, .rc-panel, [data-log-selectable]");
    const isEndInsideLog = focusEl.closest(".log-stream, .rc-detail, .rc-panel, [data-log-selectable]");
    if (!isInsideLog || !isEndInsideLog) {
      hidePrompt();
      return;
    }

    const range = sel.getRangeAt(0);
    const rect = range.getBoundingClientRect();
    if (rect.width === 0 && rect.height === 0) {
      hidePrompt();
      return;
    }

    if (rect.bottom < 0 || rect.top > window.innerHeight) {
      hidePrompt();
      return;
    }

    const promptH = 36;
    let top = rect.top - promptH - 8;
    let place: "top" | "bottom" = "top";
    if (top < 45) {
      top = rect.bottom + 8;
      place = "bottom";
    }

    let left = rect.left + rect.width / 2;
    const minLeft = hasActiveFilter ? 165 : 115;
    const maxLeft = window.innerWidth - (hasActiveFilter ? 165 : 115);
    left = Math.max(minLeft, Math.min(maxLeft, left));

    selectedText = cleanText;
    x = Math.round(left);
    y = Math.round(top);
    placement = place;
    visible = true;
  }

  function onMouseUp() {
    // Delay slightly to let browser complete selection bounding calculations
    setTimeout(handleSelection, 20);
  }

  function onMouseDown(e: MouseEvent) {
    if (promptEl && promptEl.contains(e.target as Node)) {
      return;
    }
    if (visible) {
      hidePrompt();
    }
  }

  function onScroll() {
    if (visible) {
      hidePrompt();
    }
  }

  function onKeyDown(e: KeyboardEvent) {
    if (e.key === "Escape" && visible) {
      hidePrompt();
    }
  }

  function onFilterClick(e: MouseEvent) {
    e.preventDefault();
    e.stopPropagation();
    const textToFilter = selectedText;
    hidePrompt();
    window.getSelection()?.removeAllRanges();
    onFilter(textToFilter);
  }

  function onAddToFilterClick(e: MouseEvent) {
    e.preventDefault();
    e.stopPropagation();
    const textToFilter = selectedText;
    hidePrompt();
    window.getSelection()?.removeAllRanges();
    if (onAddToFilter) {
      onAddToFilter(textToFilter);
    } else {
      onFilter(textToFilter);
    }
  }

  onMount(() => {
    document.addEventListener("mouseup", onMouseUp);
    document.addEventListener("mousedown", onMouseDown);
    window.addEventListener("scroll", onScroll, { capture: true, passive: true });
    window.addEventListener("keydown", onKeyDown);
  });

  onDestroy(() => {
    document.removeEventListener("mouseup", onMouseUp);
    document.removeEventListener("mousedown", onMouseDown);
    window.removeEventListener("scroll", onScroll, { capture: true });
    window.removeEventListener("keydown", onKeyDown);
  });
</script>

{#if visible}
  <div
    bind:this={promptEl}
    class="log-selection-prompt"
    style:top="{y}px"
    style:left="{x}px"
    role="tooltip"
    aria-label="Filter selection prompt"
    transition:fly={{ y: placement === "top" ? 4 : -4, duration: 140 }}
  >
    {#if hasActiveFilter}
      <div class="prompt-pill">
        <span class="prompt-badge" title={selectedText}>"{previewText}"</span>
        <div class="prompt-actions">
          <button
            type="button"
            class="log-selection-btn btn-action"
            onclick={onFilterClick}
            title={`Replace filter with "${selectedText}"`}
          >
            <FunnelIcon size={12} weight="fill" class="prompt-icon" />
            <span class="prompt-label">Filter by selection</span>
          </button>
          <div class="prompt-divider" aria-hidden="true"></div>
          <button
            type="button"
            class="log-selection-btn btn-add"
            onclick={onAddToFilterClick}
            title={`Add "${selectedText}" to current filter`}
          >
            <PlusIcon size={12} weight="bold" class="prompt-icon-add" />
            <span class="prompt-label-add">Add to filter</span>
          </button>
        </div>
      </div>
    {:else}
      <button
        type="button"
        class="log-selection-btn single-btn"
        onclick={onFilterClick}
        title={`Filter logs for "${selectedText}"`}
      >
        <FunnelIcon size={12} weight="fill" class="prompt-icon" />
        <span class="prompt-label">Filter by selection</span>
        <span class="prompt-badge">"{previewText}"</span>
      </button>
    {/if}
  </div>
{/if}

<style>
  .log-selection-prompt {
    position: fixed;
    transform: translateX(-50%);
    z-index: 9999;
    pointer-events: auto;
    user-select: none;

    /* Theme tokens — Dark mode defaults */
    --prompt-bg: #18181b;
    --prompt-border: rgba(255, 255, 255, 0.16);
    --prompt-shadow: 0 4px 18px -2px rgba(0, 0, 0, 0.5), 0 2px 6px -1px rgba(0, 0, 0, 0.35);
    --prompt-divider: rgba(255, 255, 255, 0.16);
    --prompt-focus: #3b82f6;

    --prompt-label: #d4d4d8;
    --prompt-label-hover: #ffffff;
    --prompt-btn-hover-bg: rgba(255, 255, 255, 0.08);

    --prompt-funnel-icon: #fbbf24;
    --prompt-funnel-border-hover: rgba(245, 158, 11, 0.4);

    --prompt-add-label: #34d399;
    --prompt-add-label-hover: #6ee7b7;
    --prompt-add-icon: #34d399;
    --prompt-add-bg-hover: rgba(16, 185, 129, 0.14);
    --prompt-add-border-hover: rgba(16, 185, 129, 0.4);

    --prompt-badge-bg: rgba(245, 158, 11, 0.15);
    --prompt-badge-border: rgba(245, 158, 11, 0.28);
    --prompt-badge-text: #fbbf24;
  }

  /* Theme tokens — Light mode (strictly WCAG AA and AAA contrast compliant) */
  :global(html.light) .log-selection-prompt,
  :global(.light) .log-selection-prompt,
  :global([data-theme="light"]) .log-selection-prompt {
    --prompt-bg: #ffffff;
    --prompt-border: rgba(0, 0, 0, 0.14);
    --prompt-shadow: 0 4px 18px -2px rgba(0, 0, 0, 0.15), 0 2px 6px -1px rgba(0, 0, 0, 0.08);
    --prompt-divider: rgba(0, 0, 0, 0.12);
    --prompt-focus: #2563eb;

    --prompt-label: #374151;
    --prompt-label-hover: #111827;
    --prompt-btn-hover-bg: rgba(0, 0, 0, 0.05);

    --prompt-funnel-icon: #92400e;
    --prompt-funnel-border-hover: rgba(146, 64, 14, 0.35);

    --prompt-add-label: #065f46;
    --prompt-add-label-hover: #064e3b;
    --prompt-add-icon: #065f46;
    --prompt-add-bg-hover: #ecfdf5;
    --prompt-add-border-hover: #6ee7b7;

    --prompt-badge-bg: #fef3c7;
    --prompt-badge-border: #fde68a;
    --prompt-badge-text: #92400e;
  }

  .prompt-pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 4px 3px 6px;
    background: var(--prompt-bg);
    border: 1px solid var(--prompt-border);
    border-radius: 8px;
    box-shadow: var(--prompt-shadow);
    backdrop-filter: blur(8px);
  }

  .prompt-actions {
    display: inline-flex;
    align-items: center;
    gap: 2px;
  }

  .prompt-divider {
    width: 1px;
    height: 15px;
    background: var(--prompt-divider);
    margin: 0 2px;
  }

  .log-selection-btn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 4px 7px;
    background: transparent;
    color: var(--prompt-label-hover);
    border: 1px solid transparent;
    border-radius: 6px;
    font-family: inherit;
    font-size: 11.5px;
    cursor: pointer;
    white-space: nowrap;
    outline: none;
    transition:
      background 120ms ease,
      border-color 120ms ease,
      transform 100ms ease,
      box-shadow 120ms ease;
  }

  .log-selection-btn:focus-visible {
    outline: 2px solid var(--prompt-focus);
    outline-offset: 1px;
  }

  .log-selection-btn.single-btn {
    padding: 5px 9px 5px 8px;
    background: var(--prompt-bg);
    border: 1px solid var(--prompt-border);
    border-radius: 7px;
    box-shadow: var(--prompt-shadow);
    backdrop-filter: blur(8px);
  }

  .log-selection-btn.btn-action:hover,
  .log-selection-btn.single-btn:hover {
    background: var(--prompt-btn-hover-bg);
    border-color: var(--prompt-funnel-border-hover);
  }

  .log-selection-btn.btn-add {
    border: 1px solid transparent;
  }

  .log-selection-btn.btn-add:hover {
    background: var(--prompt-add-bg-hover);
    border-color: var(--prompt-add-border-hover);
  }

  .log-selection-btn:active {
    transform: translateY(0);
  }

  :global(.prompt-icon) {
    color: var(--prompt-funnel-icon);
    flex-shrink: 0;
  }

  :global(.prompt-icon-add) {
    color: var(--prompt-add-icon);
    flex-shrink: 0;
    transition: color 100ms ease;
  }

  .prompt-label {
    font-weight: 500;
    color: var(--prompt-label);
    transition: color 100ms ease;
  }

  .log-selection-btn:hover .prompt-label {
    color: var(--prompt-label-hover);
  }

  .prompt-label-add {
    font-weight: 600;
    color: var(--prompt-add-label);
    transition: color 100ms ease;
  }

  .log-selection-btn.btn-add:hover .prompt-label-add {
    color: var(--prompt-add-label-hover);
  }

  .log-selection-btn.btn-add:hover :global(.prompt-icon-add) {
    color: var(--prompt-add-label-hover);
  }

  .prompt-badge {
    font-family: var(--font-mono, monospace);
    font-size: 11px;
    font-weight: 600;
    color: var(--prompt-badge-text);
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    background: var(--prompt-badge-bg);
    border: 1px solid var(--prompt-badge-border);
    padding: 1px 5px;
    border-radius: 4px;
  }
</style>
