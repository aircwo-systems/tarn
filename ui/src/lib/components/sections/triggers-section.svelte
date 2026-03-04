<script lang="ts">
	import { ArrowsClockwise, ChatCircle, GlobeHemisphereWest, Lightning } from 'phosphor-svelte';
	import { TableCell, TableRow } from '$lib/components/ui/table';
	import Badge from '$lib/components/ui/badge/badge.svelte';
	import ArnCell from '$lib/components/common/arn-cell.svelte';
	import ResourceTable from '$lib/components/common/resource-table.svelte';
	import { getDashboard, getDashboardFilters, matchesTagFilter } from '$lib/state.svelte';

	type TriggerRow = {
		id: string;
		type: 'SQS' | 'API';
		sourceName: string;
		sourceArn: string;
		targetName: string;
		targetArn: string;
		state: string;
		detail: string;
		lastResult?: string;
	};

	const dashboard = getDashboard();
	const filters = getDashboardFilters();

	const gateways = $derived(
		(dashboard.data?.gateways ?? []).filter((gateway) => matchesTagFilter(gateway.tags, filters.tagFilter))
	);
	const functions = $derived(
		(dashboard.data?.functions ?? []).filter((fn) => matchesTagFilter(fn.tags, filters.tagFilter))
	);
	const queues = $derived(
		(dashboard.data?.queues ?? []).filter((queue) => matchesTagFilter(queue.tags, filters.tagFilter))
	);
	const mappings = $derived(dashboard.data?.eventSourceMappings ?? []);
	const connections = $derived(dashboard.data?.connections ?? []);

	const functionsByName = $derived(new Map(functions.map((fn) => [fn.name, fn])));
	const queuesByName = $derived(new Map(queues.map((queue) => [queue.name, queue])));
	const gatewaysByID = $derived(new Map(gateways.map((gateway) => [gateway.apiId, gateway])));

	const filteredMappings = $derived(
		mappings.filter((mapping) => {
			if (!filters.tagFilter.trim()) {
				return true;
			}
			return functionsByName.has(mapping.functionName) || queuesByName.has(mapping.queueName);
		})
	);

	const sqsTriggers = $derived<TriggerRow[]>(
		filteredMappings.map((mapping) => {
			const queue = queuesByName.get(mapping.queueName);
			const fn = functionsByName.get(mapping.functionName);
			return {
				id: mapping.uuid,
				type: 'SQS',
				sourceName: mapping.queueName,
				sourceArn: queue?.arn ?? queue?.url ?? mapping.queueName,
				targetName: mapping.functionName,
				targetArn: fn?.arn ?? mapping.functionName,
				state: mapping.state,
				detail: `Batch ×${mapping.batchSize}`,
				lastResult: mapping.lastResult
			};
		})
	);

	const apiTriggers = $derived<TriggerRow[]>(
		connections
			.filter((connection) => connection.targetKind === 'apigw-lambda' || connection.targetKind === 'apigw-sqs')
			.flatMap((connection) => {
				const gateway = gatewaysByID.get(connection.sourceFunction);
				if (!gateway) return [] as TriggerRow[];

				if (connection.targetKind === 'apigw-lambda') {
					const targetName = connection.targetId || connection.targetName;
					const fn = functionsByName.get(targetName);
					return [{
						id: `api-${connection.source}-${targetName}`,
						type: 'API',
						sourceName: gateway.name,
						sourceArn: gateway.apiEndpoint || gateway.invokeUrl || gateway.arn,
						targetName,
						targetArn: fn?.arn ?? targetName,
						state: 'Configured',
						detail: `Integration AWS_PROXY · Stage ${gateway.defaultStage}`
					}];
				}

				const targetName = connection.targetId || connection.targetName;
				const queue = queuesByName.get(targetName);
				return [{
					id: `api-${connection.source}-${targetName}`,
					type: 'API',
					sourceName: gateway.name,
					sourceArn: gateway.apiEndpoint || gateway.invokeUrl || gateway.arn,
					targetName,
					targetArn: queue?.arn ?? queue?.url ?? targetName,
					state: 'Configured',
					detail: `Integration AWS(SQS) · Stage ${gateway.defaultStage}`
				}];
			})
	);

	const triggerRows = $derived<TriggerRow[]>([...sqsTriggers, ...apiTriggers]);
	const sqsCount = $derived(sqsTriggers.length);
	const apiCount = $derived(apiTriggers.length);

	function stateBadgeVariant(state: string): 'default' | 'amber' | 'destructive' | 'secondary' | 'outline' {
		const normalized = state.toLowerCase();
		if (normalized === 'enabled' || normalized === 'active' || normalized === 'configured') return 'default';
		if (normalized === 'creating' || normalized === 'updating' || normalized === 'pending') return 'amber';
		if (normalized.includes('fail') || normalized === 'disabled') return 'destructive';
		if (normalized === 'unknown') return 'secondary';
		return 'outline';
	}
</script>

<div class="space-y-4">
	<div class="rounded-lg border border-border bg-bg-raised px-4 py-3">
		<div class="flex flex-wrap items-center justify-between gap-3">
			<div>
				<h2 class="text-sm font-semibold text-text">Triggers</h2>
				<p class="text-[10px] text-text-faint font-mono">
					Visualizing event sources wired to functions and APIs
				</p>
			</div>
			<div class="flex items-center gap-2 text-xs">
				<span class="inline-flex items-center gap-1 rounded-full border border-border px-2 py-1 text-text-muted">
					<ChatCircle size={12} class="text-amber" />
					SQS {sqsCount}
				</span>
				<span class="inline-flex items-center gap-1 rounded-full border border-border px-2 py-1 text-text-muted">
					<GlobeHemisphereWest size={12} class="text-blue" />
					API {apiCount}
				</span>
			</div>
		</div>
	</div>

	<ResourceTable
		title="Trigger Mappings"
		count={triggerRows.length}
		loading={dashboard.loading && !dashboard.data}
		empty={triggerRows.length === 0}
		emptyMessage="No triggers configured yet."
		emptyIcon={ArrowsClockwise}
		columns={['Type', 'Source', 'Target', 'State', 'Details']}
	>
		{#each triggerRows as trigger}
			<TableRow>
				<TableCell>
					<Badge variant={trigger.type === 'SQS' ? 'amber' : 'secondary'}>
						{trigger.type}
					</Badge>
				</TableCell>
				<TableCell>
					<ArnCell name={trigger.sourceName} arn={trigger.sourceArn} />
				</TableCell>
				<TableCell>
					{#if trigger.type === 'SQS'}
						<div class="flex items-start gap-2">
							<Lightning size={13} class="mt-[2px] text-accent" />
							<ArnCell name={trigger.targetName} arn={trigger.targetArn} />
						</div>
					{:else}
						<div class="space-y-0.5 max-w-[22rem]">
							<p class="text-xs font-medium text-text">{trigger.targetName}</p>
							<p class="font-mono text-[11px] text-text-faint">{trigger.targetArn}</p>
						</div>
					{/if}
				</TableCell>
				<TableCell>
					<Badge variant={stateBadgeVariant(trigger.state)}>{trigger.state}</Badge>
				</TableCell>
				<TableCell>
					<div class="space-y-0.5">
						<p class="text-xs text-text-muted">{trigger.detail}</p>
						{#if trigger.lastResult}
							<p class="font-mono text-[11px] text-text-faint">{trigger.lastResult}</p>
						{/if}
					</div>
				</TableCell>
			</TableRow>
		{/each}
	</ResourceTable>
</div>
