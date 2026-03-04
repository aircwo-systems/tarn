<script lang="ts">
	import { Warning } from 'phosphor-svelte';
	import StatCard from './stat-card.svelte';
	import LedDot from './led-dot.svelte';
	import TopologyCanvas from './topology-canvas.svelte';
	import ActiveComponents from './active-components.svelte';
	import { getDashboard, getDashboardFilters, getUISettings, matchesTagFilter } from '$lib/state.svelte';

	const dashboard = getDashboard();
	const filters = getDashboardFilters();
	const uiSettings = getUISettings();

	const filteredGateways = $derived(
		(dashboard.data?.gateways ?? []).filter((gateway) => matchesTagFilter(gateway.tags, filters.tagFilter))
	);
	const filteredFunctions = $derived(
		(dashboard.data?.functions ?? []).filter((fn) => matchesTagFilter(fn.tags, filters.tagFilter))
	);
	const filteredQueues = $derived(
		(dashboard.data?.queues ?? []).filter((queue) => matchesTagFilter(queue.tags, filters.tagFilter))
	);
	const filteredSecrets = $derived(
		(dashboard.data?.secrets ?? []).filter((secret) => matchesTagFilter(secret.tags, filters.tagFilter))
	);

	const queueMessageTotal = $derived(
		filteredQueues.reduce(
			(sum, q) => sum + q.approxVisible + q.approxInFlight + q.approxDelayed,
			0
		)
	);

	const secretTagTotal = $derived(
		filteredSecrets.reduce((sum, s) => sum + s.tagCount, 0)
	);

	const eventMappings = $derived(dashboard.data?.eventSourceMappings ?? []);
	const eventMappingsEnabled = $derived(eventMappings.filter((m) => m.state === 'Enabled').length);

	const infraConnected = $derived(
		dashboard.data?.infrastructure?.filter((p) => p.status === 'connected').length ?? 0
	);
	const infraTotal = $derived(dashboard.data?.infrastructure?.length ?? 0);
	const infraNames = $derived(
		dashboard.data?.infrastructure
			?.filter((p) => p.status === 'connected')
			.map((p) => p.name)
			.join(', ') ?? ''
	);
</script>

<div class="space-y-4">
	<!-- Hero strip -->
	<div class="flex items-center justify-between gap-4 flex-wrap rounded-lg border border-border bg-bg-raised px-4 py-3">
		<div class="flex items-center gap-3">
			<div class="flex items-center justify-center h-8 w-8 rounded-md bg-accent/10">
				<LedDot color={dashboard.data ? 'green' : dashboard.loading ? 'amber' : 'gray'} size="md" />
			</div>
			<div>
				<h2 class="text-sm font-semibold text-text">Overview</h2>
				<p class="text-[10px] text-text-faint font-mono">
					{dashboard.data?.config.endpoint ?? 'connecting...'}
				</p>
			</div>
		</div>
		<div class="flex items-center gap-2 shrink-0">
			<span class="inline-flex items-center gap-1.5 rounded-full border border-accent-strong bg-accent-muted px-2.5 py-1 text-xs text-accent">
				<LedDot color="green" />
				Polling {uiSettings.pollingIntervalSeconds}s
			</span>
			<span class="inline-flex items-center rounded-full border border-border px-2.5 py-1 text-xs text-text-faint">
				{dashboard.data?.services?.join(' · ') ?? 'apigatewayv2 · lambda · sqs · secretsmanager'}
			</span>
		</div>
	</div>

	<!-- Stat cards -->
	<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-8 gap-3">
		<StatCard
			label="API Gateways"
			value={filteredGateways.length}
			subtitle="{filteredGateways.reduce((sum, gateway) => sum + gateway.routes, 0)} routes total"
			accentColor="blue"
		/>
		<StatCard
			label="Lambda Functions"
			value={filteredFunctions.length}
			subtitle="{filteredFunctions.filter((fn) => fn.state === 'Active').length} active"
			accentColor="accent"
		/>
		<StatCard
			label="SQS Queues"
			value={filteredQueues.length}
			subtitle="{queueMessageTotal} messages across queues"
			accentColor="amber"
		/>
		<StatCard
			label="Secrets"
			value={filteredSecrets.length}
			subtitle="{secretTagTotal} tags total"
			accentColor="default"
		/>
		<StatCard
			label="S3 Buckets"
			value={dashboard.data?.counts.buckets ?? 0}
			subtitle="{dashboard.data?.buckets?.reduce((sum, b) => sum + b.objects, 0) ?? 0} objects total"
			accentColor="accent"
		/>
		<StatCard
			label="Event Mappings"
			value={eventMappings.length}
			subtitle="{eventMappingsEnabled} enabled"
			accentColor={eventMappings.length > 0 ? 'amber' : 'default'}
		/>
		<StatCard
			label="Infrastructure"
			value="{infraConnected}/{infraTotal}"
			subtitle={infraNames || 'none detected'}
			accentColor={infraConnected > 0 ? 'accent' : 'default'}
		/>
		<StatCard
			label="Snapshot Time"
			value={dashboard.data ? new Date(dashboard.data.timestamp).toLocaleTimeString() : '--'}
			subtitle={dashboard.data ? new Date(dashboard.data.timestamp).toLocaleDateString() : 'waiting'}
			accentColor="default"
		/>
	</div>

	<!-- Warnings -->
	{#if dashboard.data?.warnings?.length}
		<div class="rounded-lg border border-red/20 bg-red-muted px-4 py-3">
			<div class="flex items-center gap-2 mb-2">
				<Warning size={14} class="text-red" />
				<h3 class="text-sm font-semibold text-red">Warnings</h3>
			</div>
			<ul class="space-y-1 text-xs text-red/80 pl-5 list-disc">
				{#each dashboard.data.warnings as warning}
					<li>{warning}</li>
				{/each}
			</ul>
		</div>
	{/if}

	<!-- Topology diagram -->
	<TopologyCanvas />

	<!-- Active components -->
	<ActiveComponents />
</div>
