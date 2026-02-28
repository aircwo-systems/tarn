<script lang="ts">
	import { Sun, Moon } from 'phosphor-svelte';
	import { getUISettings, setThemeMode } from '$lib/state.svelte';

	const settings = getUISettings();
	const dark = $derived(settings.resolvedTheme === 'dark');

	function toggle() {
		setThemeMode(dark ? 'light' : 'dark');
	}
</script>

<button
	type="button"
	onclick={toggle}
	class="flex items-center justify-center h-8 w-8 rounded-md text-text-muted hover:text-text hover:bg-bg-surface transition-colors"
	aria-label={dark ? 'Switch to light mode' : 'Switch to dark mode'}
>
	{#if dark}
		<Sun size={15} />
	{:else}
		<Moon size={15} />
	{/if}
</button>
