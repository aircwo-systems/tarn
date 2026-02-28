<script lang="ts">
	import { GlobeHemisphereWest } from 'phosphor-svelte';
	import { TableRow, TableCell } from '$lib/components/ui/table';
	import Badge from '$lib/components/ui/badge/badge.svelte';
	import ResourceTable from './resource-table.svelte';
	import ArnCell from './arn-cell.svelte';
	import GatewayDetailsPanel from './gateway-details-panel.svelte';
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

<div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
	<ResourceTable
		title="API Gateways"
		count={gateways.length}
		loading={dashboard.loading && !dashboard.data}
		empty={gateways.length === 0}
		emptyMessage="No API Gateways created yet."
		emptyIcon={GlobeHemisphereWest}
		columns={['Name', 'Protocol', 'Routes', 'Integrations', 'Stages', 'Default Stage', 'Invoke URL', 'Route Keys']}
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
					<Badge variant="secondary">{gateway.protocolType}</Badge>
				</TableCell>
				<TableCell class="font-mono text-text-muted">{gateway.routes}</TableCell>
				<TableCell class="font-mono text-text-muted">{gateway.integrations}</TableCell>
				<TableCell class="font-mono text-text-muted">{gateway.stages}</TableCell>
				<TableCell class="font-mono text-text-muted">{gateway.defaultStage}</TableCell>
				<TableCell class="font-mono text-text-faint text-xs break-all">{gateway.invokeUrl}</TableCell>
				<TableCell class="text-xs text-text-faint">
					{#if gateway.routeKeys?.length}
						{gateway.routeKeys.slice(0, 3).join(' · ')}
						{#if gateway.routeKeys.length > 3}
							<span class="text-text-muted"> +{gateway.routeKeys.length - 3} more</span>
						{/if}
					{:else}
						--
					{/if}
				</TableCell>
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
			<p class="px-3 py-5 text-sm text-text-faint">Click a gateway row to inspect routes, URLs, and gateway attributes.</p>
		</section>
	{/if}
</div>
