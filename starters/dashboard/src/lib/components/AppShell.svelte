<script lang="ts">
  import type { Snippet } from "svelte";
  import { asset } from "$app/paths";
  import { afterNavigate } from "$app/navigation";
  import Sidebar, { type NavigationItem } from "./Sidebar.svelte";
  import { getShell } from "$lib/shell.svelte";
  let {
    children,
    navigation,
    settingsHref,
  }: {
    children: Snippet;
    navigation: readonly NavigationItem[];
    settingsHref?: string;
  } = $props();
  const shell = getShell();
  let stage = $state<HTMLElement | null>(null);
  afterNavigate(({ to }) => {
    if (!to?.url.hash) stage?.scrollTo({ top: 0, left: 0, behavior: "auto" });
  });
</script>

<svelte:head><link rel="icon" href={asset("/favicon.svg")} /></svelte:head>
<svelte:window
  onkeydown={(event) => {
    if (
      (event.metaKey || event.ctrlKey) &&
      event.key.toLowerCase() === "b" &&
      !(
        event.target instanceof HTMLElement &&
        event.target.closest("input, textarea, [contenteditable='true']")
      )
    ) {
      event.preventDefault();
      shell.toggle();
    }
    if (event.key === "Escape") shell.state.mobileOpen = false;
  }}
/>
<a class="skip-link" href="#main">Skip to main content</a>
<div class="shell">
  <Sidebar
    {navigation}
    {settingsHref}
    bind:collapsed={shell.state.collapsed}
    bind:mobileOpen={shell.state.mobileOpen}
  />
  <main class="main-stage" id="main" tabindex="-1" bind:this={stage}>
    {@render children()}
  </main>
</div>

<style>
  .shell {
    display: flex;
    width: 100%;
    height: 100dvh;
    overflow: hidden;
  }
  .main-stage {
    flex: 1;
    min-width: 0;
    overflow-y: auto;
    background: var(--surface);
    margin: var(--stage-inset) var(--stage-inset) var(--stage-inset) 0;
    border-radius: var(--radius-stage);
    border: 1px solid var(--line);
    padding: 20px 24px;
  }
  .skip-link {
    position: fixed;
    top: -100px;
    left: 12px;
    z-index: 50;
    padding: 12px 16px;
    background: var(--accent-soft);
    color: var(--accent);
    border-radius: 6px;
  }
  .skip-link:focus {
    top: 12px;
  }
  @media (max-width: 640px) {
    .shell {
      flex-direction: column;
    }
    .main-stage {
      margin: 6px;
      padding: 16px;
    }
  }
</style>
