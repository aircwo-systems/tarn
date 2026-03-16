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

<svelte:head>
	<link rel="icon" href="/favicon.svg" type="image/svg+xml" />
</svelte:head>

{@render children()}
