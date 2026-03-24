<script lang="ts">
  import { SunIcon, MoonIcon } from "phosphor-svelte";
  import { getUISettings, setThemeMode } from "$lib/state.svelte";

  const settings = getUISettings();
  const dark = $derived(settings.resolvedTheme === "dark");

  function toggle() {
    setThemeMode(dark ? "light" : "dark");
  }
</script>

<button
  type="button"
  onclick={toggle}
  class="flex items-center justify-center h-8 w-8 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
  aria-label={dark ? "Switch to light mode" : "Switch to dark mode"}
>
  {#if dark}
    <SunIcon size={15} />
  {:else}
    <MoonIcon size={15} />
  {/if}
</button>
