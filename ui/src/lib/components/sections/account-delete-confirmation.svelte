<script lang="ts">
  import { TrashIcon, WarningCircleIcon } from "phosphor-svelte";
  import { onMount } from "svelte";

  let {
    accountId,
    label,
    busy = false,
    onconfirm,
    oncancel,
  }: {
    accountId: string;
    label: string;
    busy?: boolean;
    onconfirm: () => void;
    oncancel: () => void;
  } = $props();

  const HOLD_MS = 2000;
  type HoldInput = { kind: "pointer"; pointerId: number } | { kind: "keyboard"; key: " " | "Enter" };
  let input: HoldInput | null = null;
  let frame = 0;
  let deadline: ReturnType<typeof setTimeout> | undefined;
  let progress = $state(0);
  let holding = $state(false);
  let submitted = $state(false);
  let button: HTMLButtonElement;
  const disabled = $derived(busy || submitted);
  const remaining = $derived(((HOLD_MS * (1 - progress)) / 1000).toFixed(1));

  function cancelHold() {
    cancelAnimationFrame(frame);
    clearTimeout(deadline);
    input = null;
    holding = false;
    progress = 0;
  }

  function completeHold() {
    if (!input) return;
    if (document.hidden || disabled || document.activeElement !== button) {
      cancelHold();
      return;
    }
    cancelAnimationFrame(frame);
    clearTimeout(deadline);
    input = null;
    progress = 1;
    holding = false;
    submitted = true;
    onconfirm();
  }

  function startHold(source: HoldInput) {
    if (disabled || input) return;
    input = source;
    holding = true;
    const started = performance.now();
    // Enforce the hold duration even when the browser delays animation frames.
    deadline = setTimeout(completeHold, HOLD_MS);
    function advance(now: number) {
      if (document.hidden || disabled || document.activeElement !== button) {
        cancelHold();
        return;
      }
      progress = Math.min((now - started) / HOLD_MS, 1);
      if (progress < 1) {
        frame = requestAnimationFrame(advance);
        return;
      }
      completeHold();
    }
    frame = requestAnimationFrame(advance);
  }

  function pointerDown(event: PointerEvent) {
    if (event.button !== 0 || !event.isPrimary) return;
    button.focus({ preventScroll: true });
    startHold({ kind: "pointer", pointerId: event.pointerId });
  }

  function pointerEnd(event: PointerEvent) {
    if (input?.kind === "pointer" && input.pointerId === event.pointerId) cancelHold();
  }

  function pointerMove(event: PointerEvent) {
    if (input?.kind !== "pointer" || input.pointerId !== event.pointerId) return;
    // Touch pointers can be captured implicitly, so pointerleave alone is insufficient.
    const rect = button.getBoundingClientRect();
    if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) cancelHold();
  }

  function keyDown(event: KeyboardEvent) {
    if (event.key !== " " && event.key !== "Enter") return;
    event.preventDefault();
    if (event.repeat || event.altKey || event.ctrlKey || event.metaKey) return;
    startHold({ kind: "keyboard", key: event.key });
  }

  function keyUp(event: KeyboardEvent) {
    if (input?.kind !== "keyboard" || input.key !== event.key) return;
    event.preventDefault();
    cancelHold();
  }

  function escape(event: KeyboardEvent) {
    if (event.key !== "Escape" || disabled) return;
    event.preventDefault();
    cancelHold();
    oncancel();
  }

  function visibilityChanged() {
    if (document.hidden) cancelHold();
  }

  onMount(() => {
    button.focus({ preventScroll: true });
    return cancelHold;
  });
</script>

<svelte:window onblur={cancelHold} onpointerup={pointerEnd} onpointercancel={pointerEnd} onkeydown={escape} onkeyup={keyUp} />
<svelte:document onvisibilitychange={visibilityChanged} />

<div id="delete-confirmation-{accountId}" class="confirmation" role="group" aria-labelledby="delete-title-{accountId}" aria-busy={disabled}>
  <div class="warning">
    <span class="warning-icon" aria-hidden="true"><WarningCircleIcon size={18} /></span>
    <div class="copy">
      <h3 id="delete-title-{accountId}">Delete {label === accountId ? "this account" : label}?</h3>
      <p class="account-id">{accountId} <span>· Cannot be undone</span></p>
      <p id="delete-description-{accountId}">Permanently removes its functions, queues, tables, buckets and other data. Shared traces stay.</p>
    </div>
  </div>
  <div class="footer">
    <p id="delete-instructions-{accountId}" class="instructions">
      Hold for 2 seconds. Release to cancel.
      <span class="sr-only">With a keyboard, hold Enter or Space. Press Escape to close.</span>
    </p>
    <div class="actions">
      <button type="button" class="cancel" disabled={disabled} onclick={oncancel}>Cancel</button>
      <button
        bind:this={button}
        type="button"
        class="hold-button"
        class:holding
        disabled={disabled}
        aria-label="Hold to delete account {accountId}"
        aria-describedby="delete-description-{accountId} delete-instructions-{accountId}"
        onpointerdown={pointerDown}
        onpointerup={pointerEnd}
        onpointercancel={pointerEnd}
        onpointerleave={pointerEnd}
        onpointermove={pointerMove}
        onkeydown={keyDown}
        onblur={cancelHold}
        oncontextmenu={(event) => event.preventDefault()}
      >
        <span class="fill" style="transform: scaleX({progress})" aria-hidden="true"></span>
        <span class="button-content" aria-hidden="true">
          <TrashIcon size={14} />
          <span>{disabled ? "Deleting…" : holding ? "Keep holding…" : "Hold to delete"}</span>
          <span class="countdown">{holding ? remaining : "2.0"}s</span>
        </span>
      </button>
    </div>
  </div>
  <span class="sr-only" role="status">{disabled ? "Deleting account." : holding ? "Keep holding for two seconds. Release to cancel." : ""}</span>
</div>

<style>
  .confirmation {
    padding: 14px;
    border: 1px solid color-mix(in srgb, var(--accent-red) 35%, transparent);
    border-radius: 10px;
    background: var(--accent-red-bg);
  }
  .warning { display: flex; align-items: flex-start; gap: 10px; }
  .warning-icon { display: flex; flex-shrink: 0; margin-top: 1px; color: var(--accent-red); }
  .copy { min-width: 0; }
  h3 { font-size: 12.5px; font-weight: 600; color: var(--text-primary); overflow-wrap: anywhere; }
  p { font-size: 11.5px; line-height: 1.6; color: var(--text-secondary); }
  .account-id { margin: 2px 0 6px; font-family: var(--font-mono); font-size: 10.5px; }
  .account-id span { font-family: var(--font-sans); color: var(--accent-red); }
  .footer { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 10px; margin-top: 14px; }
  .instructions { font-size: 11px; }
  .actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-left: auto; }
  button { min-height: 44px; border-radius: 8px; font-size: 11.5px; cursor: pointer; }
  button:focus-visible { outline: 2px solid var(--border-focus); outline-offset: 3px; }
  button:disabled { cursor: not-allowed; }
  .cancel { padding: 0 12px; color: var(--text-secondary); transition: color 120ms ease, background 120ms ease; }
  .cancel:hover:not(:disabled) { color: var(--text-primary); background: var(--bg-element-hover); }
  .cancel:disabled { opacity: 0.5; }
  .hold-button {
    position: relative;
    overflow: hidden;
    isolation: isolate;
    padding: 0 12px;
    border: 1px solid color-mix(in srgb, var(--accent-red) 60%, transparent);
    color: var(--accent-red);
    background: color-mix(in srgb, var(--accent-red) 8%, var(--bg-stage));
    user-select: none;
    -webkit-user-select: none;
    -webkit-touch-callout: none;
    touch-action: manipulation;
    transition: border-color 120ms ease, background 120ms ease;
  }
  .hold-button:hover:not(:disabled), .hold-button.holding { border-color: var(--accent-red); background: color-mix(in srgb, var(--accent-red) 14%, var(--bg-stage)); }
  .fill { position: absolute; inset: 0; z-index: -1; transform-origin: left; background: color-mix(in srgb, var(--accent-red) 20%, transparent); pointer-events: none; }
  .button-content { display: flex; align-items: center; justify-content: center; gap: 7px; min-width: 164px; }
  .countdown { margin-left: 5px; font-family: var(--font-mono); font-size: 10px; font-variant-numeric: tabular-nums; }
  .sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
  @media (prefers-reduced-motion: reduce) {
    button { transition: none; }
    .holding .fill { transform: none !important; }
  }
</style>
