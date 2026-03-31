<script lang="ts">
  import { CaretDownIcon } from "phosphor-svelte";

  let {
    raw,
    formatted = null,
    formattedHtml = null,
    formattedLabel = "Formatted",
    rawLabel = "Raw",
    formattedOpenByDefault = true,
    rawOpenByDefault = false,
    formattedContentClass = "text-[12px] text-foreground",
    rawContentClass = "text-[11px] text-muted-foreground",
    formattedMaxHeightClass = "max-h-[55vh]",
    rawMaxHeightClass = "max-h-[40vh]",
  }: {
    raw: string;
    formatted?: string | null;
    formattedHtml?: string | null;
    formattedLabel?: string;
    rawLabel?: string;
    formattedOpenByDefault?: boolean;
    rawOpenByDefault?: boolean;
    formattedContentClass?: string;
    rawContentClass?: string;
    formattedMaxHeightClass?: string;
    rawMaxHeightClass?: string;
  } = $props();

  let formattedExpanded = $state(true);
  let rawExpanded = $state(false);

  const hasFormatted = $derived(
    !!((formattedHtml && formattedHtml.trim()) || (formatted && formatted.trim())),
  );

  $effect(() => {
    // Reset panel open states when content changes.
    raw;
    formatted;
    formattedHtml;
    formattedExpanded = formattedOpenByDefault;
    rawExpanded = rawOpenByDefault;
  });
</script>

{#if hasFormatted}
  <div class="mb-2 rounded-md border border-border overflow-hidden">
    <button
      type="button"
      onclick={() => (formattedExpanded = !formattedExpanded)}
      class="flex items-center justify-between w-full bg-muted px-3 py-2 text-[11px] text-muted-foreground hover:text-foreground hover:bg-muted/80 transition-colors"
    >
      <span class="flex items-center gap-2 font-medium">
        <span class="h-1.5 w-1.5 rounded-full bg-accent/80 shrink-0"></span>
        {formattedLabel}
      </span>
      <CaretDownIcon
        size={11}
        class="transition-transform duration-150 {formattedExpanded ? 'rotate-180' : ''}"
      />
    </button>
    {#if formattedExpanded}
      {#if formattedHtml}
        <div
          class={`border-t border-border bg-[var(--code-bg)] px-3 py-3 font-mono leading-relaxed whitespace-pre-wrap break-all overflow-y-auto ${formattedContentClass} ${formattedMaxHeightClass}`}
          >{@html formattedHtml}</div
        >
      {:else}
        <pre
          class={`border-t border-border bg-[var(--code-bg)] px-3 py-3 font-mono leading-relaxed whitespace-pre-wrap break-all overflow-y-auto ${formattedContentClass} ${formattedMaxHeightClass}`}>{formatted}</pre
        >
      {/if}
    {/if}
  </div>
{/if}

<div class="rounded-md border border-border overflow-hidden">
  <button
    type="button"
    onclick={() => (rawExpanded = !rawExpanded)}
    class="flex items-center justify-between w-full bg-muted px-3 py-2 text-[11px] text-muted-foreground hover:text-foreground hover:bg-muted/80 transition-colors"
  >
    <span class="flex items-center gap-2 font-medium">
      <span class="h-1.5 w-1.5 rounded-full bg-text-faint/50 shrink-0"></span>
      {rawLabel}
    </span>
    <CaretDownIcon
      size={11}
      class="transition-transform duration-150 {rawExpanded ? 'rotate-180' : ''}"
    />
  </button>
  {#if rawExpanded}
    <pre
      class={`border-t border-border bg-[var(--code-bg)] px-3 py-3 font-mono leading-relaxed whitespace-pre-wrap break-all overflow-y-auto ${rawContentClass} ${rawMaxHeightClass}`}>{raw}</pre
    >
  {/if}
</div>
