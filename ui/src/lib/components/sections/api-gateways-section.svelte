<script lang="ts">
	import { GlobeHemisphereWestIcon } from 'phosphor-svelte';
	import { TableRow, TableCell } from '$lib/components/ui/table';
	import Badge from '$lib/components/ui/badge/badge.svelte';
	import ResourceTable from '$lib/components/common/resource-table.svelte';
	import ArnCell from '$lib/components/common/arn-cell.svelte';
	import GatewayDetailsPanel from '$lib/components/topology/gateway-details-panel.svelte';
	import { getDashboard, getDashboardFilters, matchesTagFilter } from '$lib/state.svelte';

	const dashboard = getDashboard();
	const filters = getDashboardFilters();
	const gateways = $derived(
		(dashboard.data?.gateways ?? []).filter((gateway) => matchesTagFilter(gateway.tags, filters.tagFilter))
	);
	let selectedGatewayId = $state('');
	const selectedGateway = $derived(gateways.find((gateway) => gateway.apiId === selectedGatewayId) ?? null);

	$effect(() => {
		if (selectedGatewayId && !gateways.some((gateway) => gateway.apiId === selectedGatewayId)) {
			selectedGatewayId = '';
		}
	});

	function selectGateway(apiId: string) {
		selectedGatewayId = apiId;
	}

	function closeGatewayPanel() {
		selectedGatewayId = '';
	}

	function onGatewayRowKeydown(event: KeyboardEvent, apiId: string) {
		if (event.key === 'Enter' || event.key === ' ') {
			event.preventDefault();
			selectGateway(apiId);
		}
	}
</script>

<div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_26rem]">
	<ResourceTable
		title="API Gateways"
		count={gateways.length}
		loading={dashboard.loading && !dashboard.data}
		empty={gateways.length === 0}
		emptyMessage="No API Gateways created yet."
		emptyIcon={GlobeHemisphereWestIcon}
		columns={['Name', 'Type', 'Stage', 'Routes', 'Integrations']}
	>
		{#each gateways as gateway}
			<TableRow
				class={`cursor-pointer focus-within:bg-bg-surface/60 ${gateway.apiId === selectedGatewayId ? 'bg-bg-surface/60' : ''}`}
				role="button"
				tabindex="0"
				aria-label={`Open details for API Gateway ${gateway.name}`}
				onclick={() => selectGateway(gateway.apiId)}
				onkeydown={(event: KeyboardEvent) => onGatewayRowKeydown(event, gateway.apiId)}
			>
				<TableCell><ArnCell name={gateway.name} arn={gateway.arn} /></TableCell>
				<TableCell>
					<div class="flex items-center gap-1.5">
						<Badge variant="secondary">{gateway.protocolType}</Badge>
						{#if gateway.version === 'v1'}
							<Badge variant="outline" class="text-[10px] px-1 py-0 font-mono">v1</Badge>
						{:else}
							<Badge variant="outline" class="text-[10px] px-1 py-0 font-mono">v2</Badge>
						{/if}
					</div>
				</TableCell>
				<TableCell class="font-mono text-xs text-text-muted">
					{gateway.defaultStage || '—'}
				</TableCell>
				<TableCell class="font-mono text-text-muted">{gateway.routes}</TableCell>
				<TableCell class="font-mono text-text-muted">{gateway.integrations}</TableCell>
			</TableRow>
		{/each}
	</ResourceTable>

	{#if selectedGateway}
		<GatewayDetailsPanel gateway={selectedGateway} onClose={closeGatewayPanel} />
	{:else}
		<section class="rounded-lg border border-border bg-bg-raised">
			<div class="border-b border-border px-3 py-2">
				<h3 class="text-sm font-semibold text-text">Gateway Details</h3>
			</div>
			<p class="px-3 py-5 text-sm text-text-faint">Select a gateway to inspect routes, integrations, and request templates.</p>
		</section>
	{/if}
</div>
