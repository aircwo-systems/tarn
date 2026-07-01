<script lang="ts">
  import { CaretDownIcon, CheckIcon, CopySimpleIcon } from "phosphor-svelte";
  import { Tabs, TabsList, TabsTrigger } from "$lib/components/ui/tabs";
  import VirtualizedCode from "$lib/components/common/virtualized-code.svelte";

  let {
    raw,
    formatted = null,
    formattedHtml = null,
    formattedLabel = "Formatted",
    rawLabel = "Raw",
    formattedOpenByDefault = true,
    rawOpenByDefault = false,
    variant = "expanders",
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
    variant?: "expanders" | "tabs";
    formattedContentClass?: string;
    rawContentClass?: string;
    formattedMaxHeightClass?: string;
    rawMaxHeightClass?: string;
  } = $props();

  let formattedExpanded = $state(true);
  let rawExpanded = $state(false);
  let copiedContent = $state(false);
  let activeView = $state<"formatted" | "raw">("formatted");

  const hasFormatted = $derived(
    !!((formattedHtml && formattedHtml.trim()) || (formatted && formatted.trim())),
  );

  $effect(() => {
    raw;
    formatted;
    formattedHtml;
    formattedExpanded = formattedOpenByDefault;
    rawExpanded = rawOpenByDefault;
    activeView = hasFormatted ? "formatted" : "raw";
    copiedContent = false;
  });

  async function copyCurrent() {
    const content =
      variant === "tabs" && activeView === "raw" ? raw : (formatted ?? raw);
    if (!content.trim()) return;

    try {
      await navigator.clipboard.writeText(content);
      copiedContent = true;
      setTimeout(() => {
        copiedContent = false;
      }, 1400);
    } catch (error) {
      console.error("Failed to copy formatted content", error);
    }
  }

  const lineTabTriggerClass =
    "rounded-none border-0 bg-transparent px-0 shadow-none data-active:border-transparent dark:data-active:border-transparent data-active:bg-transparent dark:data-active:bg-transparent";
</script>

{#if variant === "tabs"}
  <div class="overflow-hidden rounded-md border border-border/70">
    {#if hasFormatted}
      <div class="border-b border-border/70 bg-background/35 px-3 py-2">
        <Tabs bind:value={activeView}>
          <TabsList variant="line" class="gap-3">
            <TabsTrigger value="formatted" class={lineTabTriggerClass}>
              {formattedLabel}
            </TabsTrigger>
            <TabsTrigger value="raw" class={lineTabTriggerClass}>
              {rawLabel}
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </div>
    {/if}
    <div class="group relative">
      <button
        type="button"
        class="absolute right-2 top-2 z-10 inline-flex h-8 items-center gap-1.5 rounded-md border border-border/80 bg-background/90 px-2 text-[11px] text-muted-foreground opacity-0 shadow-sm backdrop-blur-sm transition-all duration-200 hover:bg-background-subtle hover:text-foreground group-hover:opacity-100 focus-visible:opacity-100"
        onclick={() => void copyCurrent()}
        title={copiedContent ? "Copied" : "Copy"}
        aria-label={copiedContent ? "Copied content" : "Copy content"}
      >
        <span class="relative flex h-3.5 w-3.5 items-center justify-center">
          <span
            class={`absolute transition-all duration-200 ${copiedContent ? "scale-75 opacity-0" : "scale-100 opacity-100"}`}
          >
            <CopySimpleIcon size={13} />
          </span>
          <span
            class={`absolute transition-all duration-200 ${copiedContent ? "scale-100 opacity-100" : "scale-75 opacity-0"}`}
          >
            <CheckIcon size={13} />
          </span>
        </span>
        <span>{copiedContent ? "Copied" : "Copy"}</span>
      </button>
      {#if !hasFormatted || activeView === "raw"}
        <VirtualizedCode
          text={raw}
          contentClass={rawContentClass}
          maxHeightClass={rawMaxHeightClass}
        />
      {:else}
        <VirtualizedCode
          text={formatted ?? raw}
          html={formattedHtml}
          contentClass={formattedContentClass}
          maxHeightClass={formattedMaxHeightClass}
        />
      {/if}
    </div>
  </div>
{:else}
  {#if hasFormatted}
    <div class="mb-2 overflow-hidden rounded-md border border-border/70">
      <button
        type="button"
        onclick={() => (formattedExpanded = !formattedExpanded)}
        class="flex w-full items-center justify-between bg-background/35 px-3 py-2 text-[11px] text-muted-foreground transition-colors hover:bg-background-subtle hover:text-foreground"
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
        <div class="group relative border-t border-border">
          <button
            type="button"
            class="absolute right-2 top-2 z-10 inline-flex h-8 items-center gap-1.5 rounded-md border border-border/80 bg-background/90 px-2 text-[11px] text-muted-foreground opacity-0 shadow-sm backdrop-blur-sm transition-all duration-200 hover:bg-background-subtle hover:text-foreground group-hover:opacity-100 focus-visible:opacity-100"
            onclick={() => void copyCurrent()}
            title={copiedContent ? "Copied" : "Copy formatted"}
            aria-label={copiedContent ? "Copied formatted content" : "Copy formatted content"}
          >
            <span class="relative flex h-3.5 w-3.5 items-center justify-center">
              <span
                class={`absolute transition-all duration-200 ${copiedContent ? "scale-75 opacity-0" : "scale-100 opacity-100"}`}
              >
                <CopySimpleIcon size={13} />
              </span>
              <span
                class={`absolute transition-all duration-200 ${copiedContent ? "scale-100 opacity-100" : "scale-75 opacity-0"}`}
              >
                <CheckIcon size={13} />
              </span>
            </span>
            <span>{copiedContent ? "Copied" : "Copy"}</span>
          </button>
          <VirtualizedCode
            text={formatted ?? raw}
            html={formattedHtml}
            contentClass={formattedContentClass}
            maxHeightClass={formattedMaxHeightClass}
          />
        </div>
      {/if}
    </div>
  {/if}

  <div class="rounded-md border border-border overflow-hidden">
    <button
      type="button"
      onclick={() => (rawExpanded = !rawExpanded)}
      class="flex w-full items-center justify-between bg-background/35 px-3 py-2 text-[11px] text-muted-foreground transition-colors hover:bg-background-subtle hover:text-foreground"
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
      <div class="border-t border-border">
        <VirtualizedCode
          text={raw}
          contentClass={rawContentClass}
          maxHeightClass={rawMaxHeightClass}
        />
      </div>
    {/if}
  </div>
{/if}
