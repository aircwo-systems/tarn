<script lang="ts">
	import { onMount } from 'svelte';
	import NavRail from '$lib/components/nav-rail.svelte';
	import DashboardFilterBar from '$lib/components/dashboard-filter-bar.svelte';
	import OverviewSection from '$lib/components/overview-section.svelte';
	import APIGatewaysSection from '$lib/components/api-gateways-section.svelte';
	import FunctionsSection from '$lib/components/functions-section.svelte';
	import QueuesSection from '$lib/components/queues-section.svelte';
	import SecretsSection from '$lib/components/secrets-section.svelte';
	import TriggersSection from '$lib/components/triggers-section.svelte';
	import StorageSection from '$lib/components/storage-section.svelte';
	import LogsSection from '$lib/components/logs-section.svelte';
	import { getDashboard } from '$lib/state.svelte';

	const dashboard = getDashboard();
	const validTabs = ['overview', 'gateways', 'functions', 'queues', 'secrets', 'triggers', 'storage', 'logs'];

	let activeTab = $state('overview');
	let logsInitialGroup = $state('');

	function readHash() {
		const raw = window.location.hash.replace('#', '');
		const [tab, qs] = raw.split('?');
		if (validTabs.includes(tab)) {
			activeTab = tab;
		}
		if (tab === 'logs' && qs) {
			const params = new URLSearchParams(qs);
			logsInitialGroup = params.get('group') ?? '';
		}
	}

	function setTab(tab: string) {
		activeTab = tab;
		window.location.hash = tab;
	}

	onMount(() => {
		readHash();
		window.addEventListener('hashchange', readHash);
		return () => window.removeEventListener('hashchange', readHash);
	});
</script>

<svelte:head>
	<title>Rack Console — OpenStack</title>
</svelte:head>

<div class="flex min-h-screen bg-bg">
	<NavRail {activeTab} onTabChange={setTab} />

	<main class="flex-1 min-w-0 px-4 py-4 md:px-6 md:py-5 pb-20 md:pb-5 space-y-4">
		<DashboardFilterBar />
		{#if activeTab === 'overview'}
			<OverviewSection />
		{:else if activeTab === 'gateways'}
			<APIGatewaysSection />
		{:else if activeTab === 'functions'}
			<FunctionsSection />
		{:else if activeTab === 'queues'}
			<QueuesSection />
		{:else if activeTab === 'secrets'}
			<SecretsSection />
		{:else if activeTab === 'triggers'}
			<TriggersSection />
		{:else if activeTab === 'storage'}
			<StorageSection />
		{:else if activeTab === 'logs'}
			<LogsSection initialGroup={logsInitialGroup} />
		{/if}
	</main>
</div>
