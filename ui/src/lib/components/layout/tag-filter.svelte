<script lang="ts">
  import { MagnifyingGlassIcon } from "phosphor-svelte";
  import {
    getDashboardFilters,
    setDashboardTagFilter,
  } from "$lib/state.svelte";
  import { parseFilterTokens, mergeFilterTokens } from "$lib/filter-utils";

  let {
    onToggleSidebar,
  }: {
    onToggleSidebar: () => void;
  } = $props();

  const filters = getDashboardFilters();

  let tagDraft = $state("");
  let tagTokens = $state<string[]>(parseFilterTokens(filters.tagFilter));

  $effect(() => {
    const next = parseFilterTokens(filters.tagFilter);
    if (next.length !== tagTokens.length || next.some((t, i) => t !== tagTokens[i])) {
      tagTokens = next;
    }
    tagDraft = "";
  });

  function applyFilter() {
    tagTokens = mergeFilterTokens(tagTokens, tagDraft);
    tagDraft = "";
    setDashboardTagFilter(tagTokens.join(" "));
  }

  function clearFilter() {
    tagDraft = "";
    tagTokens = [];
    setDashboardTagFilter("");
  }

  function removeToken(token: string) {
    tagTokens = tagTokens.filter((t) => t !== token);
    setDashboardTagFilter(tagTokens.join(" "));
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === "Enter") {
      event.preventDefault();
      applyFilter();
      return;
    }
    if (event.key === "Backspace" && !tagDraft && tagTokens.length > 0) {
      event.preventDefault();
      removeToken(tagTokens[tagTokens.length - 1]);
    }
  }
</script>

<div
  class="flex min-w-1/2 shrink-0 items-center gap-1.5 rounded-[5px] border border-border bg-background/80 px-2 py-1"
  style="min-height:28px"
>
  <MagnifyingGlassIcon size={12} class="shrink-0 text-muted-foreground/50" />
  <div class="flex min-w-0 flex-1 flex-wrap items-center gap-1">
    {#each tagTokens as token (token)}
      <button
        type="button"
        class="inline-flex h-5 items-center gap-1 rounded bg-muted px-1.5 text-[10px] text-foreground/85"
        onclick={() => removeToken(token)}
        aria-label={`Remove filter ${token}`}
        title={`Remove filter ${token}`}
      >
        <span>{token}</span>
        <span class="text-muted-foreground/60">×</span>
      </button>
    {/each}
    <input
      type="text"
      placeholder={tagTokens.length > 0 ? "Add filter..." : "Filter topology by tag or type..."}
      bind:value={tagDraft}
      onkeydown={handleKeydown}
      class="min-w-[88px] flex-1 bg-transparent text-[11px] text-foreground outline-none placeholder:text-muted-foreground/40"
    />
  </div>
  {#if tagDraft.trim()}
    <button onclick={applyFilter} class="text-[10px] text-primary hover:text-primary/80">add</button>
  {/if}
  {#if tagDraft || tagTokens.length > 0}
    <button onclick={clearFilter} class="text-[10px] text-muted-foreground/40 hover:text-muted-foreground">✕</button>
  {/if}
</div>
