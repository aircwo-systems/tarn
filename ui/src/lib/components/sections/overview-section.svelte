<script lang="ts">
	import {
		WarningIcon,
		GlobeHemisphereWestIcon,
		LightningIcon,
		ChatCircleIcon,
		KeyIcon,
		HardDriveIcon,
		ArrowsClockwiseIcon,
		MagnifyingGlassIcon,
		XIcon
	} from 'phosphor-svelte';
	import LedDot from '$lib/components/common/led-dot.svelte';
	import TopologyCanvas from '$lib/components/topology/topology-canvas.svelte';
	import ActiveComponents from './active-components.svelte';
	import {
		getDashboard,
		getDashboardFilters,
		setDashboardTagFilter,
		getUISettings,
		getVisibleInfra,
		matchesTagFilter
	} from '$lib/state.svelte';

	let { onNavigate = (_tab: string) => {} }: { onNavigate?: (tab: string) => void } = $props();

	const dashboard = getDashboard();
	const filters = getDashboardFilters();
	const uiSettings = getUISettings();

	let tagDraft = $state(filters.tagFilter);

	$effect(() => {
		tagDraft = filters.tagFilter;
	});

	function applyFilter() {
		setDashboardTagFilter(tagDraft);
	}

	function clearFilter() {
		tagDraft = '';
		setDashboardTagFilter('');
	}

	const filteredGateways = $derived(
		(dashboard.data?.gateways ?? []).filter((g) => matchesTagFilter(g.tags, filters.tagFilter))
	);
	const filteredFunctions = $derived(
		(dashboard.data?.functions ?? []).filter((f) => matchesTagFilter(f.tags, filters.tagFilter))
	);
	const filteredQueues = $derived(
		(dashboard.data?.queues ?? []).filter((q) => matchesTagFilter(q.tags, filters.tagFilter))
	);
	const filteredSecrets = $derived(
		(dashboard.data?.secrets ?? []).filter((s) => matchesTagFilter(s.tags, filters.tagFilter))
	);

	const buckets = $derived(dashboard.data?.buckets ?? []);
	const eventMappings = $derived(dashboard.data?.eventSourceMappings ?? []);
	const visibleInfra = $derived(getVisibleInfra(dashboard.data?.infrastructure ?? []));
	const infraConnected = $derived(visibleInfra.filter((p) => p.status === 'connected').length);

	const connectionStatus = $derived(
		dashboard.error ? 'error' : dashboard.loading ? 'loading' : dashboard.data ? 'ok' : 'idle'
	);
</script>

<!-- Full-bleed header: same bg as sidebar, visually anchored to it -->
<div class="-mx-4 md:-mx-6 -mt-4 md:-mt-5 mb-4 border-b border-border bg-bg-raised">
	<!-- Row 1: connection status + tag filter + polling -->
	<div class="flex flex-wrap items-center gap-x-3 gap-y-1.5 px-4 md:px-6 py-2.5">
		<!-- Connection -->
		<div class="flex items-center gap-2 shrink-0 min-w-0">
			<LedDot
				color={connectionStatus === 'ok' ? 'green' : connectionStatus === 'loading' ? 'amber' : 'red'}
				size="md"
			/>
			<span
				class="text-xs font-mono text-text-faint truncate max-w-[14rem]"
				title={dashboard.data?.config.endpoint}
			>
				{dashboard.data?.config.endpoint ?? 'connecting...'}
			</span>
			{#if dashboard.data?.config.region}
				<span class="text-text-faint/40 hidden sm:inline">·</span>
				<span class="text-[10px] font-mono text-text-faint hidden sm:inline">
					{dashboard.data.config.region}
				</span>
			{/if}
		</div>

		<div class="h-3.5 w-px bg-border shrink-0 hidden sm:block"></div>

		<!-- Tag filter (inline, no card) -->
		<div class="flex items-center gap-1.5 flex-1 min-w-[10rem] max-w-[20rem]">
			<MagnifyingGlassIcon size={12} class="text-text-faint shrink-0" />
			<input
				type="text"
				placeholder="Filter by tag..."
				bind:value={tagDraft}
				onkeydown={(e) => {
					if (e.key === 'Enter') applyFilter();
				}}
				class="flex-1 min-w-0 bg-transparent text-xs text-text outline-none placeholder:text-text-faint"
			/>
			{#if tagDraft}
				<button
					type="button"
					onclick={clearFilter}
					class="text-text-faint hover:text-text shrink-0 transition-colors"
					aria-label="Clear filter"
				>
					<XIcon size={11} />
				</button>
			{/if}
			{#if tagDraft !== filters.tagFilter}
				<button
					type="button"
					onclick={applyFilter}
					class="shrink-0 text-[10px] font-medium text-accent hover:text-accent/70 px-1 transition-colors"
				>
					Apply
				</button>
			{/if}
		</div>

		{#if filters.tagFilter}
			<span
				class="shrink-0 inline-flex items-center rounded-full border border-accent-strong bg-accent/10 px-2 py-0.5 text-[10px] font-mono text-accent"
			>
				{filters.tagFilter}
			</span>
		{/if}

		<!-- Polling indicator -->
		<div class="ml-auto shrink-0 flex items-center gap-1.5">
			<LedDot color="green" />
			<span class="text-[10px] font-mono text-text-faint">
				{uiSettings.pollingIntervalSeconds}s
			</span>
		</div>
	</div>

	<!-- Row 2: resource count pills (clickable navigation) -->
	<div class="flex flex-wrap items-center gap-1.5 px-4 md:px-6 pb-2.5">
		<button
			type="button"
			onclick={() => onNavigate('gateways')}
			class="inline-flex items-center gap-1.5 rounded-md border border-border bg-bg-surface/50 px-2.5 py-1 text-[11px] hover:border-border-strong hover:bg-bg-surface transition-colors"
		>
			<GlobeHemisphereWestIcon size={11} class="text-blue shrink-0" />
			<span class="font-mono text-text">{filteredGateways.length}</span>
			<span class="text-text-faint">Gateways</span>
		</button>

		<button
			type="button"
			onclick={() => onNavigate('functions')}
			class="inline-flex items-center gap-1.5 rounded-md border border-border bg-bg-surface/50 px-2.5 py-1 text-[11px] hover:border-border-strong hover:bg-bg-surface transition-colors"
		>
			<LightningIcon size={11} class="text-accent shrink-0" />
			<span class="font-mono text-text">{filteredFunctions.length}</span>
			<span class="text-text-faint">Functions</span>
		</button>

		<button
			type="button"
			onclick={() => onNavigate('queues')}
			class="inline-flex items-center gap-1.5 rounded-md border border-border bg-bg-surface/50 px-2.5 py-1 text-[11px] hover:border-border-strong hover:bg-bg-surface transition-colors"
		>
			<ChatCircleIcon size={11} class="text-amber shrink-0" />
			<span class="font-mono text-text">{filteredQueues.length}</span>
			<span class="text-text-faint">Queues</span>
		</button>

		<button
			type="button"
			onclick={() => onNavigate('secrets')}
			class="inline-flex items-center gap-1.5 rounded-md border border-border bg-bg-surface/50 px-2.5 py-1 text-[11px] hover:border-border-strong hover:bg-bg-surface transition-colors"
		>
			<KeyIcon size={11} class="text-blue/80 shrink-0" />
			<span class="font-mono text-text">{filteredSecrets.length}</span>
			<span class="text-text-faint">Secrets</span>
		</button>

		<button
			type="button"
			onclick={() => onNavigate('storage')}
			class="inline-flex items-center gap-1.5 rounded-md border border-border bg-bg-surface/50 px-2.5 py-1 text-[11px] hover:border-border-strong hover:bg-bg-surface transition-colors"
		>
			<HardDriveIcon size={11} class="text-accent/80 shrink-0" />
			<span class="font-mono text-text">{buckets.length}</span>
			<span class="text-text-faint">Buckets</span>
		</button>

		{#if eventMappings.length > 0}
			<button
				type="button"
				onclick={() => onNavigate('triggers')}
				class="inline-flex items-center gap-1.5 rounded-md border border-border bg-bg-surface/50 px-2.5 py-1 text-[11px] hover:border-border-strong hover:bg-bg-surface transition-colors"
			>
				<ArrowsClockwiseIcon size={11} class="text-amber/80 shrink-0" />
				<span class="font-mono text-text">{eventMappings.length}</span>
				<span class="text-text-faint">Triggers</span>
			</button>
		{/if}

		{#if visibleInfra.length > 0}
			<span
				class="inline-flex items-center gap-1.5 rounded-md border border-border bg-bg-surface/50 px-2.5 py-1 text-[11px]"
			>
				<LedDot color={infraConnected > 0 ? 'green' : 'red'} />
				<span class="font-mono text-text">{infraConnected}/{visibleInfra.length}</span>
				<span class="text-text-faint">Infra</span>
			</span>
		{/if}

		{#if dashboard.data?.services?.length}
			<span class="ml-auto text-[10px] font-mono text-text-faint hidden lg:block">
				{dashboard.data.services.join(' · ')}
			</span>
		{/if}
	</div>
</div>

<!-- Warnings (if any) -->
{#if dashboard.data?.warnings?.length}
	<div class="rounded-lg border border-red/20 bg-red-muted px-4 py-3">
		<div class="flex items-center gap-2 mb-2">
			<WarningIcon size={14} class="text-red" />
			<h3 class="text-sm font-semibold text-red">Warnings</h3>
		</div>
		<ul class="space-y-1 text-xs text-red/80 pl-5 list-disc">
			{#each dashboard.data.warnings as warning (warning)}
				<li>{warning}</li>
			{/each}
		</ul>
	</div>
{/if}

<div class="flex flex-col gap-4 overflow-hidden" style="height: calc(100vh - 6.5rem)">
	<div class="flex-1 min-h-0">
		<TopologyCanvas {onNavigate} />
	</div>
	<div class="shrink-0">
		<ActiveComponents />
	</div>
</div>
