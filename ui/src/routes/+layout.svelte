<script lang="ts">
	import '../app.css';
	import { onMount, onDestroy } from 'svelte';
	import { initUISettings, startPolling, stopPolling } from '$lib/state.svelte';

	let { children } = $props();

	onMount(() => {
		initUISettings();
		startPolling();
	});

	onDestroy(() => {
		stopPolling();
	});
</script>

{@render children()}
