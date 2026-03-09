<script lang="ts">
	import { ArrowsInSimpleIcon, ArrowsOutSimpleIcon, CaretDownIcon, CaretUpIcon } from 'phosphor-svelte';
	import GatewayDetailsPanel from '$lib/components/topology/gateway-details-panel.svelte';
	import { getDashboard, getDashboardFilters, getVisibleInfra, matchesTagFilter } from '$lib/state.svelte';
	import type { InfraProbe, RequestTrace } from '$lib/types';
	import TopologyComponentsView from './TopologyComponentsView.svelte';
	import TopologyConnectionView from './TopologyConnectionView.svelte';

	const dashboard = getDashboard();
	const filters = getDashboardFilters();

	const gateways = $derived((dashboard.data?.gateways ?? []).filter((gw) => matchesTagFilter(gw.tags, filters.tagFilter)));
	const functions = $derived((dashboard.data?.functions ?? []).filter((fn) => matchesTagFilter(fn.tags, filters.tagFilter)));
	const queues = $derived((dashboard.data?.queues ?? []).filter((q) => matchesTagFilter(q.tags, filters.tagFilter)));
	const secrets = $derived((dashboard.data?.secrets ?? []).filter((s) => matchesTagFilter(s.tags, filters.tagFilter)));
	const buckets = $derived(dashboard.data?.buckets ?? []);
	const eventSourceMappings = $derived(dashboard.data?.eventSourceMappings ?? []);
	const infra = $derived(getVisibleInfra(dashboard.data?.infrastructure ?? []));
	const infraConnections = $derived(dashboard.data?.connections ?? []);
	const recentTraces = $derived(dashboard.data?.recentTraces ?? []);

	let viewMode = $state<'components' | 'connections'>('components');
	let selectedGatewayId = $state('');
	let canvasExpanded = $state(false);
	let infraOrderIds = $state<string[]>([]);
	let infraOrderHydrated = $state(false);

	const INFRA_ORDER_STORAGE_KEY = 'openstack-ui-topology-infra-order-v1';

	function infraNodeId(probe: InfraProbe): string {
		return `${probe.kind}-${probe.host}-${probe.port}`;
	}

	// Infra nodes ordered for the toolbar — mirrors what TopologyConnectionView computes
	const infraNodesForToolbar = $derived(
		(() => {
			const visible = infra.slice(0, 4).map((probe) => ({ id: infraNodeId(probe), probe }));
			if (visible.length === 0) return [] as { id: string; label: string }[];
			const byId = new Map(visible.map((e) => [e.id, e.probe]));
			const orderedIds = [
				...infraOrderIds.filter((id) => byId.has(id)),
				...visible.map((e) => e.id).filter((id) => !infraOrderIds.includes(id))
			];
			return orderedIds.map((id) => ({ id, label: byId.get(id)!.name.slice(0, 13) }));
		})()
	);

	const resourceCount = $derived(
		gateways.length + functions.length + queues.length + buckets.length + secrets.length + infra.length
	);

	const connectionCount = $derived(infraConnections.length + eventSourceMappings.length);

	const selectedGateway = $derived(gateways.find((gw) => gw.apiId === selectedGatewayId) ?? null);

	$effect(() => {
		if (selectedGatewayId && !gateways.some((gw) => gw.apiId === selectedGatewayId)) {
			selectedGatewayId = '';
		}
	});

	$effect(() => {
		const visibleIds = infra.slice(0, 4).map((probe) => infraNodeId(probe));

		if (typeof window !== 'undefined' && !infraOrderHydrated) {
			infraOrderHydrated = true;
			try {
				const raw = localStorage.getItem(INFRA_ORDER_STORAGE_KEY);
				if (raw) {
					const parsed = JSON.parse(raw);
					if (Array.isArray(parsed)) {
						infraOrderIds = parsed.filter((v): v is string => typeof v === 'string');
					}
				}
			} catch {
				infraOrderIds = [];
			}
		}

		const normalized = [
			...infraOrderIds.filter((id) => visibleIds.includes(id)),
			...visibleIds.filter((id) => !infraOrderIds.includes(id))
		];

		if (normalized.length !== infraOrderIds.length || normalized.some((id, i) => id !== infraOrderIds[i])) {
			infraOrderIds = normalized;
		}
	});

	$effect(() => {
		if (typeof window === 'undefined' || !infraOrderHydrated) return;
		localStorage.setItem(INFRA_ORDER_STORAGE_KEY, JSON.stringify(infraOrderIds));
	});

	function moveInfraNode(id: string, direction: -1 | 1) {
		const index = infraOrderIds.indexOf(id);
		if (index === -1) return;
		const nextIndex = index + direction;
		if (nextIndex < 0 || nextIndex >= infraOrderIds.length) return;
		const next = [...infraOrderIds];
		[next[index], next[nextIndex]] = [next[nextIndex], next[index]];
		infraOrderIds = next;
	}

	function openGateway(apiId: string) {
		selectedGatewayId = apiId;
	}

	function closeGatewayPanel() {
		selectedGatewayId = '';
	}
</script>

<div class="rounded-lg border border-border bg-bg-raised overflow-hidden">
	<!-- Toolbar -->
	<div class="flex items-center justify-between px-3 py-2 border-b border-border gap-3">
		<h3 class="text-xs font-mono uppercase tracking-wider text-text-muted">Topology</h3>

		<div class="inline-flex rounded-md border border-border bg-bg-surface p-0.5">
			<button
				type="button"
				class={`px-2.5 py-1 text-[10px] font-mono uppercase tracking-wide rounded-sm transition ${viewMode === 'components' ? 'bg-bg-overlay text-text' : 'text-text-faint hover:text-text-muted'}`}
				onclick={() => (viewMode = 'components')}
			>
				Component View
			</button>
			<button
				type="button"
				class={`px-2.5 py-1 text-[10px] font-mono uppercase tracking-wide rounded-sm transition ${viewMode === 'connections' ? 'bg-bg-overlay text-text' : 'text-text-faint hover:text-text-muted'}`}
				onclick={() => (viewMode = 'connections')}
			>
				Connection View
			</button>
		</div>

		<div class="flex items-center gap-2">
			<span class="text-[10px] text-text-faint font-mono">
				{viewMode === 'components' ? `${resourceCount} resources` : `${connectionCount} links`}
			</span>
			<button
				type="button"
				class="inline-flex h-7 w-7 items-center justify-center rounded-md border border-border text-text-muted transition-colors hover:bg-bg-overlay hover:text-text"
				aria-label={canvasExpanded ? 'Collapse canvas' : 'Expand canvas'}
				title={canvasExpanded ? 'Collapse canvas' : 'Expand canvas'}
				onclick={() => (canvasExpanded = !canvasExpanded)}
			>
				{#if canvasExpanded}
					<ArrowsInSimpleIcon size={13} />
				{:else}
					<ArrowsOutSimpleIcon size={13} />
				{/if}
			</button>
		</div>
	</div>

	<!-- Infra order controls (connection view only, when >1 infra node) -->
	{#if viewMode === 'connections' && infraNodesForToolbar.length > 1}
		<div class="border-b border-border px-3 py-2">
			<div class="flex flex-wrap items-center gap-2">
				<span class="text-[10px] font-mono uppercase tracking-wide text-text-faint">Infra Order</span>
				{#each infraNodesForToolbar as node, index (node.id)}
					<div class="inline-flex items-center gap-1 rounded-md border border-border bg-bg-surface px-1.5 py-1">
						<span class="text-[10px] text-text-muted">{node.label}</span>
						<div class="inline-flex gap-0.5">
							<button
								type="button"
								class="inline-flex h-5 w-5 items-center justify-center rounded border border-border text-text-faint disabled:opacity-40"
								disabled={index === 0}
								onclick={() => moveInfraNode(node.id, -1)}
								aria-label={`Move ${node.label} up`}
							>
								<CaretUpIcon size={10} />
							</button>
							<button
								type="button"
								class="inline-flex h-5 w-5 items-center justify-center rounded border border-border text-text-faint disabled:opacity-40"
								disabled={index === infraNodesForToolbar.length - 1}
								onclick={() => moveInfraNode(node.id, 1)}
								aria-label={`Move ${node.label} down`}
							>
								<CaretDownIcon size={10} />
							</button>
						</div>
					</div>
				{/each}
			</div>
		</div>
	{/if}

	<!-- Canvas + detail panel -->
	<div class="flex flex-col lg:flex-row">
		<div class="relative min-w-0 flex-1 overflow-x-auto">
			{#if viewMode === 'components'}
				<TopologyComponentsView
					{gateways}
					{functions}
					{queues}
					{secrets}
					{buckets}
					{infra}
					{canvasExpanded}
					onGatewayClick={openGateway}
				/>
			{:else}
				<TopologyConnectionView
					{gateways}
					{functions}
					{queues}
					{secrets}
					{buckets}
					{infra}
					{eventSourceMappings}
					{infraConnections}
					{infraOrderIds}
					{recentTraces}
					{canvasExpanded}
					onGatewayClick={openGateway}
				/>
			{/if}
		</div>

		{#if selectedGateway}
			<div class="w-full border-t border-border bg-bg-surface/40 p-3 lg:w-[22rem] lg:border-l lg:border-t-0">
				<GatewayDetailsPanel gateway={selectedGateway} onClose={closeGatewayPanel} />
			</div>
		{/if}
	</div>
</div>
