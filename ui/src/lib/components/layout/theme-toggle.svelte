<script lang="ts">
  import { SunIcon, MoonIcon } from "phosphor-svelte";
  import { getUISettings, setThemeMode } from "$lib/state.svelte";

  let { class: className = "" }: { class?: string } = $props();

  const settings = getUISettings();
  const dark = $derived(settings.resolvedTheme === "dark");

  function toggle() {
    setThemeMode(dark ? "light" : "dark");
  }
</script>

<button
  type="button"
  onclick={toggle}
  class={className || "footer-btn"}
  aria-label={dark ? "Switch to light mode" : "Switch to dark mode"}
  title={dark ? "Switch to light mode" : "Switch to dark mode"}
>
  {#if dark}
    <SunIcon size={14} />
  {:else}
    <MoonIcon size={14} />
  {/if}
</button>
