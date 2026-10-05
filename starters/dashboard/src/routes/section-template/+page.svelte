<script lang="ts">
  import { page } from "$app/state";
  import { resolve } from "$app/paths";
  import PageHeader from "$lib/components/PageHeader.svelte";
  import SectionSkeleton from "$lib/components/SectionSkeleton.svelte";
  import ResourceBrowserExample from "$lib/demo/components/ResourceBrowserExample.svelte";
  import { product } from "$lib/config";
  let example = $state<"resource" | "skeleton">("resource");
  // Query choices apply after hydration so this route can still be prerendered.
  $effect(() => {
    example =
      page.url.searchParams.get("example") === "skeleton"
        ? "skeleton"
        : "resource";
  });
</script>

<svelte:head
  ><title>Layout examples | {product.name}</title><meta
    name="description"
    content="A working resource browser with a collapsible submenu and a generic section skeleton."
  /></svelte:head
>
<PageHeader
  title="Layout examples"
  description="Choose a starting point for your next section."
  dataLabel={example === "resource" ? "Sample data" : "Suggested layout"}
/>
<nav class="example-switcher" aria-label="Layout examples">
  <a
    href="{resolve('/section-template')}?example=resource"
    aria-current={example === "resource" ? "page" : undefined}
    >Resource browser</a
  >
  <a
    href="{resolve('/section-template')}?example=skeleton"
    aria-current={example === "skeleton" ? "page" : undefined}
    >Section skeleton</a
  >
</nav>
{#if example === "resource"}
  <ResourceBrowserExample />
{:else}
  <div class="draft-note">
    <span class="draft-tag">Recommendation</span>
    <p>
      This is a layout sketch. Choose the content, fields and actions when you
      know what the section needs to do.
    </p>
  </div>
  <SectionSkeleton />
  <div class="guidance">
    <h2>Adapt the structure</h2>
    <p>
      Use the toolbar for the section’s primary controls. Add a summary only
      when it helps someone make a decision. Let the main area carry the task,
      with details alongside it when useful.
    </p>
    <p>
      The placeholders are static. They describe space and hierarchy; they are
      not final content or a loading state.
    </p>
  </div>
{/if}

<style>
  .example-switcher {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    padding: 16px 0;
    border-bottom: 1px solid var(--line);
  }
  .example-switcher a {
    display: inline-flex;
    align-items: center;
    min-height: 32px;
    padding: 6px 12px;
    border: 1px solid transparent;
    border-radius: var(--radius-control);
    color: var(--ink-secondary);
    font-size: 12px;
    text-decoration: none;
  }
  .example-switcher a:hover {
    color: var(--ink);
    background: var(--surface-hover);
  }
  .example-switcher a[aria-current="page"] {
    color: var(--ink);
    background: var(--surface-raised);
    border-color: var(--line-strong);
  }
  .draft-note {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 16px 0 4px;
  }
  .draft-tag {
    font: 10px var(--font-data);
    border: 1px solid var(--line-strong);
    border-radius: 5px;
    padding: 3px 6px;
    color: var(--ink-secondary);
    flex-shrink: 0;
  }
  .draft-note p,
  .guidance p {
    color: var(--ink-secondary);
    font-size: 12px;
    line-height: 1.7;
  }
  .guidance {
    border-top: 1px solid var(--line);
    margin-top: 28px;
    padding-top: 20px;
    max-width: 680px;
  }
  .guidance p {
    margin-top: 8px;
  }
  @media (max-width: 640px) {
    .example-switcher a {
      min-height: 44px;
    }
    .draft-note {
      align-items: flex-start;
      flex-direction: column;
      gap: 8px;
    }
  }
</style>
