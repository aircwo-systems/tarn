<script lang="ts">
	import { GlobeHemisphereWestIcon, LightningIcon, ChatCircleIcon, KeyIcon, HardDriveIcon, HardDrivesIcon, ArrowsClockwiseIcon } from 'phosphor-svelte';
	import LedDot from '$lib/components/common/led-dot.svelte';
	import Badge from '$lib/components/ui/badge/badge.svelte';
	import { getDashboard, getDashboardFilters, getVisibleInfra, matchesTagFilter } from '$lib/state.svelte';
	import { formatBytes } from '$lib/utils';

	const dashboard = getDashboard();
	const filters = getDashboardFilters();

	const gateways = $derived((dashboard.data?.gateways ?? []).filter((gateway) => matchesTagFilter(gateway.tags, filters.tagFilter)));
	const functions = $derived((dashboard.data?.functions ?? []).filter((fn) => matchesTagFilter(fn.tags, filters.tagFilter)));
	const queues = $derived((dashboard.data?.queues ?? []).filter((queue) => matchesTagFilter(queue.tags, filters.tagFilter)));
	const secrets = $derived((dashboard.data?.secrets ?? []).filter((secret) => matchesTagFilter(secret.tags, filters.tagFilter)));
	const buckets = $derived(dashboard.data?.buckets ?? []);
	const eventMappings = $derived(dashboard.data?.eventSourceMappings ?? []);
	const infra = $derived(getVisibleInfra(dashboard.data?.infrastructure ?? []));
	const totalCount = $derived(gateways.length + functions.length + queues.length + secrets.length + buckets.length + eventMappings.length + infra.length);

	function fnLedColor(state: string): 'green' | 'amber' | 'red' | 'gray' {
		const s = state.toLowerCase();
		if (s === 'active') return 'green';
		if (s === 'pending') return 'amber';
		if (s === 'failed' || s === 'inactive') return 'red';
		return 'gray';
	}
</script>

<div class="rounded-lg border border-border bg-bg-raised overflow-hidden">
	<div class="flex items-center justify-between px-3 py-2 border-b border-border">
		<h3 class="text-xs font-mono uppercase tracking-wider text-text-muted">Active Components</h3>
		<span class="text-[10px] text-text-faint font-mono">{totalCount} total</span>
	</div>

	{#if totalCount === 0 && !dashboard.loading}
		<div class="flex items-center justify-center py-8 text-text-faint">
			<p class="text-xs font-mono">No resources deployed</p>
		</div>
	{:else}
		<div class="grid grid-cols-1 lg:grid-cols-7 divide-y lg:divide-y-0 lg:divide-x divide-border">
			<!-- API Gateway -->
			<div class="p-3">
				<div class="flex items-center gap-2 mb-2">
					<GlobeHemisphereWestIcon size={13} weight="fill" class="text-blue" />
					<span class="text-[10px] font-mono uppercase tracking-wider text-text-muted">API Gateway</span>
					<span class="ml-auto text-[10px] font-mono text-text-faint">{gateways.length}</span>
				</div>
				{#if gateways.length === 0}
					<p class="text-[11px] text-text-faint py-2">No gateways</p>
				{:else}
					<ul class="space-y-1.5">
						{#each gateways as gateway}
							<li class="flex items-center gap-2 group">
								<LedDot color="green" />
								<span class="text-xs text-text truncate flex-1" title={gateway.arn}>{gateway.name}</span>
								<span class="text-[10px] font-mono text-text-faint shrink-0">{gateway.routes} routes</span>
							</li>
						{/each}
					</ul>
				{/if}
			</div>

			<!-- Lambda -->
			<div class="p-3">
				<div class="flex items-center gap-2 mb-2">
					<LightningIcon size={13} weight="fill" class="text-accent" />
					<span class="text-[10px] font-mono uppercase tracking-wider text-text-muted">Lambda</span>
					<span class="ml-auto text-[10px] font-mono text-text-faint">{functions.length}</span>
				</div>
				{#if functions.length === 0}
					<p class="text-[11px] text-text-faint py-2">No functions</p>
				{:else}
					<ul class="space-y-1.5">
						{#each functions as fn}
							<li class="flex items-center gap-2 group">
								<LedDot color={fnLedColor(fn.state)} />
								<span class="text-xs text-text truncate flex-1" title={fn.arn}>{fn.name}</span>
								<Badge variant="secondary" class="text-[10px] px-1.5 py-0">{fn.runtime}</Badge>
								<span class="text-[10px] font-mono text-text-faint shrink-0">{fn.memoryMB}MB</span>
							</li>
						{/each}
					</ul>
				{/if}
			</div>

			<!-- SQS -->
			<div class="p-3">
				<div class="flex items-center gap-2 mb-2">
					<ChatCircleIcon size={13} weight="fill" class="text-amber" />
					<span class="text-[10px] font-mono uppercase tracking-wider text-text-muted">SQS</span>
					<span class="ml-auto text-[10px] font-mono text-text-faint">{queues.length}</span>
				</div>
				{#if queues.length === 0}
					<p class="text-[11px] text-text-faint py-2">No queues</p>
				{:else}
					<ul class="space-y-1.5">
						{#each queues as q}
							<li class="flex items-center gap-2 group">
								<LedDot color="amber" />
								<span class="text-xs text-text truncate flex-1" title={q.url}>{q.name}</span>
								<Badge variant={q.fifo ? 'amber' : 'secondary'} class="text-[10px] px-1.5 py-0">
									{q.fifo ? 'FIFO' : 'Std'}
								</Badge>
								<span class="text-[10px] font-mono text-text-faint shrink-0">{q.approxVisible} msg</span>
							</li>
						{/each}
					</ul>
				{/if}
			</div>

			<!-- Secrets -->
			<div class="p-3">
				<div class="flex items-center gap-2 mb-2">
					<KeyIcon size={13} weight="fill" class="text-blue" />
					<span class="text-[10px] font-mono uppercase tracking-wider text-text-muted">Secrets</span>
					<span class="ml-auto text-[10px] font-mono text-text-faint">{secrets.length}</span>
				</div>
				{#if secrets.length === 0}
					<p class="text-[11px] text-text-faint py-2">No secrets</p>
				{:else}
					<ul class="space-y-1.5">
						{#each secrets as s}
							<li class="flex items-center gap-2 group">
								<LedDot color="green" />
								<span class="text-xs text-text truncate flex-1" title={s.arn}>{s.name}</span>
								<span class="text-[10px] font-mono text-text-faint shrink-0">v{s.versionId.slice(0, 8)}</span>
							</li>
						{/each}
					</ul>
				{/if}
			</div>

			<!-- S3 -->
			<div class="p-3">
				<div class="flex items-center gap-2 mb-2">
					<HardDriveIcon size={13} weight="fill" class="text-accent" />
					<span class="text-[10px] font-mono uppercase tracking-wider text-text-muted">S3</span>
					<span class="ml-auto text-[10px] font-mono text-text-faint">{buckets.length}</span>
				</div>
				{#if buckets.length === 0}
					<p class="text-[11px] text-text-faint py-2">No buckets</p>
				{:else}
					<ul class="space-y-1.5">
						{#each buckets as bucket}
							<li class="flex items-center gap-2 group">
								<LedDot color="green" />
								<span class="text-xs text-text truncate flex-1">{bucket.name}</span>
								<span class="text-[10px] font-mono text-text-faint shrink-0">{bucket.objects} obj</span>
							</li>
						{/each}
					</ul>
				{/if}
			</div>

			<!-- Event Mappings -->
			<div class="p-3">
				<div class="flex items-center gap-2 mb-2">
					<ArrowsClockwiseIcon size={13} weight="fill" class="text-amber" />
					<span class="text-[10px] font-mono uppercase tracking-wider text-text-muted">Triggers</span>
					<span class="ml-auto text-[10px] font-mono text-text-faint">{eventMappings.length}</span>
				</div>
				{#if eventMappings.length === 0}
					<p class="text-[11px] text-text-faint py-2">No mappings</p>
				{:else}
					<ul class="space-y-1.5">
						{#each eventMappings as esm}
							{@const hasFilter = (esm.filterCriteria?.Filters?.length ?? 0) > 0}
							<li class="flex items-center gap-2 group" title={hasFilter ? `Filter: ${esm.filterCriteria!.Filters[0].Pattern}` : `${esm.queueName} → ${esm.functionName}`}>
								<LedDot color={esm.state === 'Enabled' ? 'green' : 'gray'} />
								<span class="text-xs text-text truncate flex-1">
									{esm.queueName} → {esm.functionName}
								</span>
								{#if hasFilter}
									<Badge variant="amber" class="text-[9px] px-1 py-0 shrink-0">filter</Badge>
								{/if}
								<span class="text-[10px] font-mono text-text-faint shrink-0">×{esm.batchSize}</span>
							</li>
						{/each}
					</ul>
				{/if}
			</div>

			<!-- Infrastructure -->
			<div class="p-3">
				<div class="flex items-center gap-2 mb-2">
					<HardDrivesIcon size={13} weight="fill" class="text-accent" />
					<span class="text-[10px] font-mono uppercase tracking-wider text-text-muted">Infrastructure</span>
					<span class="ml-auto text-[10px] font-mono text-text-faint">{infra.length}</span>
				</div>
				{#if infra.length === 0}
					<p class="text-[11px] text-text-faint py-2">No probes selected</p>
				{:else}
					<ul class="space-y-1.5">
						{#each infra as probe (probe.kind + probe.host + probe.port)}
							<li class="flex items-center gap-2 group">
								<LedDot color={probe.status === 'connected' ? 'green' : 'red'} />
								{#if probe.kind === 'http' || probe.kind === 'https'}
									<a
										href="{probe.kind}://{probe.host}:{probe.port}"
										target="_blank"
										rel="noreferrer"
										class="text-xs text-accent truncate flex-1 hover:underline"
										title="{probe.host}:{probe.port}"
									>{probe.name}</a>
								{:else}
									<span class="text-xs text-text truncate flex-1" title="{probe.host}:{probe.port}">{probe.name}</span>
								{/if}
								{#if probe.version}
									<Badge variant="secondary" class="text-[10px] px-1.5 py-0">{probe.version}</Badge>
								{/if}
								{#if probe.status === 'connected'}
									<span class="text-[10px] font-mono text-text-faint shrink-0">{Math.round(probe.latencyMs)}ms</span>
								{:else}
									<span class="text-[10px] font-mono text-red/70 shrink-0">{probe.status}</span>
								{/if}
							</li>
						{/each}
					</ul>
				{/if}
			</div>
		</div>
	{/if}
</div>
