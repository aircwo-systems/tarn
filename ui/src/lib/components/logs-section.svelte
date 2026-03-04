<script lang="ts">
	import { Scroll, MagnifyingGlass, Funnel, ArrowLeft, ArrowsClockwise, CaretDown } from 'phosphor-svelte';
	import Badge from '$lib/components/ui/badge/badge.svelte';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import EmptyState from './empty-state.svelte';
	import { fetchLogGroups, fetchLogEvents, type FetchLogEventsParams } from '$lib/api';
	import type { LogGroupSummary, LogEvent } from '$lib/types';

	let {
		initialGroup = ''
	}: {
		initialGroup?: string;
	} = $props();

	// ── State ────────────────────────────────────────────────────────────
	let groups = $state<LogGroupSummary[]>([]);
	let groupsLoading = $state(true);
	let groupsError = $state('');

	let selectedGroup = $state('');
	let events = $state<LogEvent[]>([]);
	let eventsTotal = $state(0);
	let eventsLoading = $state(false);
	let eventsError = $state('');

	// Filters
	let filterLevel = $state('');
	let filterPattern = $state('');
	let filterStream = $state('');
	let eventsLimit = $state(100);
	let eventsCursor = $state<string | null>(null); // Cursor for pagination (timestamp)
	let prevCursors = $state<string[]>([]); // Stack of previous cursors for back navigation
	let showFilters = $state(false);
	let autoRefresh = $state(false);
	let autoRefreshTimer = $state<ReturnType<typeof setInterval> | null>(null);
	let groupSearch = $state('');
	let serviceFilter = $state('all');

	// ── Lifecycle ────────────────────────────────────────────────────────
	$effect(() => {
		loadGroups();
	});

	$effect(() => {
		// Sync prop -> local state when parent passes a new group (e.g. deep-link)
		if (initialGroup) {
			selectedGroup = initialGroup;
		}
	});

	$effect(() => {
		if (selectedGroup) {
			eventsOffset = 0;
			loadEvents();
		}
	});

	$effect(() => {
		if (autoRefresh && selectedGroup) {
			autoRefreshTimer = setInterval(() => {
				loadEvents();
			}, 3000);
		}
		return () => {
			if (autoRefreshTimer) {
				clearInterval(autoRefreshTimer);
				autoRefreshTimer = null;
			}
		};
	});

	// ── Data fetching ────────────────────────────────────────────────────
	async function loadGroups() {
		groupsLoading = true;
		groupsError = '';
		try {
			groups = await fetchLogGroups();
		} catch (err) {
			groupsError = err instanceof Error ? err.message : 'Failed to load log groups';
		} finally {
			groupsLoading = false;
		}
	}

	async function loadEvents() {
		if (!selectedGroup) return;
		eventsLoading = true;
		eventsError = '';
		try {
			const params: FetchLogEventsParams = { limit: eventsLimit };
			if (eventsCursor) params.cursor = eventsCursor;
			if (filterLevel) params.level = filterLevel;
			if (filterPattern) params.pattern = filterPattern;
			if (filterStream) params.stream = filterStream;
			const result = await fetchLogEvents(selectedGroup, params);
			events = result.events ?? [];
			eventsTotal = result.total ?? 0;
			nextCursor = result.nextCursor || null;
		} catch (err) {
			eventsError = err instanceof Error ? err.message : 'Failed to load log events';
		} finally {
			eventsLoading = false;
		}
	}

	// ── Helpers ──────────────────────────────────────────────────────────
	function selectGroup(name: string) {
		selectedGroup = name;
		window.location.hash = `logs?group=${encodeURIComponent(name)}`;
	}

	function backToGroups() {
		selectedGroup = '';
		events = [];
		eventsTotal = 0;
		autoRefresh = false;
		window.location.hash = 'logs';
	}

	function applyFilters() {
		eventsCursor = null;
		prevCursors = [];
		loadEvents();
	}

	function clearFilters() {
		filterLevel = '';
		filterPattern = '';
		filterStream = '';
		eventsCursor = null;
		prevCursors = [];
		loadEvents();
	}

	let nextCursor: string | null = null;
	function nextPage() {
		if (nextCursor) {
			prevCursors = [...prevCursors, eventsCursor || ''];
			eventsCursor = nextCursor;
			loadEvents();
		}
	}

	function prevPage() {
		if (prevCursors.length > 0) {
			eventsCursor = prevCursors[prevCursors.length - 1] || null;
			prevCursors = prevCursors.slice(0, -1);
			loadEvents();
		} else {
			eventsCursor = null;
			loadEvents();
		}
	}

	function levelColor(level: string): 'default' | 'destructive' | 'amber' | 'secondary' | 'outline' {
		switch (level) {
			case 'ERROR': return 'destructive';
			case 'WARN': return 'amber';
			case 'DEBUG': return 'secondary';
			case 'INFO': return 'default';
			default: return 'outline';
		}
	}

	function groupDisplayName(name: string): string {
		if (name.startsWith('/aws/lambda/')) return name.slice('/aws/lambda/'.length);
		if (name.startsWith('/openstack/')) return name.slice(1);
		return name;
	}

	function groupCategory(name: string): string {
		return serviceLabel(groupServiceKey(name));
	}

	function groupServiceKey(name: string): string {
		if (name.startsWith('/aws/lambda/')) return 'lambda';
		if (name === '/openstack/api') return 'api';
		if (name === '/openstack/system') return 'system';
		if (name.startsWith('/openstack/apigateway')) return 'apigatewayv2';
		if (name.startsWith('/openstack/sqs')) return 'sqs';
		if (name.startsWith('/openstack/secrets')) return 'secretsmanager';
		if (name.startsWith('/openstack/')) return 'system';
		return 'other';
	}

	function serviceLabel(key: string): string {
		switch (key) {
			case 'all': return 'All';
			case 'lambda': return 'Lambda';
			case 'api': return 'API';
			case 'system': return 'System';
			case 'apigatewayv2': return 'API Gateway';
			case 'sqs': return 'SQS';
			case 'secretsmanager': return 'Secrets';
			default: return 'Other';
		}
	}

	function isLambdaGroup(name: string): boolean {
		return name.startsWith('/aws/lambda/');
	}

	function isLambdaOutputEvent(event: LogEvent): boolean {
		return isLambdaGroup(selectedGroup) && event.source === 'output';
	}

	const hasNextPage = $derived(!!nextCursor && events.length === eventsLimit);
	const hasPrevPage = $derived(prevCursors.length > 0 || !!eventsCursor);
	const selectedGroupIsLambda = $derived(isLambdaGroup(selectedGroup));
	const pageInfo = $derived(
		eventsTotal > 0 && events.length > 0
			? `${events[0].timestamp} – ${events[events.length - 1].timestamp} (${events.length} events)`
			: 'No events'
	);
	const serviceOptions = $derived((() => {
		const counts = new Map<string, number>();
		for (const group of groups) {
			const key = groupServiceKey(group.name);
			counts.set(key, (counts.get(key) ?? 0) + 1);
		}

		const orderedKeys = ['lambda', 'api', 'system', 'apigatewayv2', 'sqs', 'secretsmanager', 'other'];
		const options = [{ key: 'all', label: serviceLabel('all'), count: groups.length }];
		for (const key of orderedKeys) {
			const count = counts.get(key) ?? 0;
			if (count > 0) {
				options.push({ key, label: serviceLabel(key), count });
			}
		}
		return options;
	})());
	const filteredGroups = $derived(
		groups.filter((group) => {
			if (serviceFilter !== 'all' && groupServiceKey(group.name) !== serviceFilter) {
				return false;
			}

			const query = groupSearch.trim().toLowerCase();
			if (!query) {
				return true;
			}

			return group.name.toLowerCase().includes(query) || groupDisplayName(group.name).toLowerCase().includes(query);
		})
	);
	const groupsCountLabel = $derived(
		filteredGroups.length === groups.length
			? `${groups.length} groups`
			: `${filteredGroups.length} of ${groups.length} groups`
	);
</script>

{#if selectedGroup}
	<!-- ── Event viewer ─────────────────────────────────────────────── -->
	<div class="space-y-3">
		<!-- Header -->
		<div class="flex items-center justify-between gap-3 flex-wrap rounded-lg border border-border bg-bg-raised px-4 py-3">
			<div class="flex items-center gap-3 min-w-0">
				<button
					type="button"
					onclick={backToGroups}
					class="flex items-center justify-center h-7 w-7 rounded-md text-text-muted hover:text-text hover:bg-bg-surface transition-colors shrink-0"
					aria-label="Back to log groups"
				>
					<ArrowLeft size={16} />
				</button>
				<div class="min-w-0">
					<h2 class="text-sm font-semibold text-text truncate">{selectedGroup}</h2>
					<p class="text-[10px] text-text-faint font-mono">{pageInfo}</p>
				</div>
			</div>
			<div class="flex items-center gap-2 shrink-0">
				<button
					type="button"
					onclick={() => autoRefresh = !autoRefresh}
					class="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs transition-colors {autoRefresh ? 'border-accent-strong bg-accent-muted text-accent' : 'border-border text-text-muted hover:text-text'}"
				>
					<ArrowsClockwise size={12} class={autoRefresh ? 'animate-spin' : ''} />
					{autoRefresh ? 'Live' : 'Auto'}
				</button>
				<button
					type="button"
					onclick={() => showFilters = !showFilters}
					class="inline-flex items-center gap-1.5 rounded-full border border-border px-2.5 py-1 text-xs text-text-muted hover:text-text transition-colors"
				>
					<Funnel size={12} />
					Filters
					<CaretDown size={10} class="transition-transform {showFilters ? 'rotate-180' : ''}" />
				</button>
				<button
					type="button"
					onclick={loadEvents}
					disabled={eventsLoading}
					class="inline-flex items-center gap-1.5 rounded-md border border-accent-strong bg-accent-muted px-2.5 py-1 text-xs text-accent hover:bg-accent/20 transition-colors disabled:opacity-50"
				>
					<ArrowsClockwise size={12} class={eventsLoading ? 'animate-spin' : ''} />
					Refresh
				</button>
			</div>
		</div>

		<!-- Filters panel -->
		{#if showFilters}
			<div class="rounded-lg border border-border bg-bg-raised px-4 py-3">
				<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
					<div class="space-y-1">
						<label class="text-[10px] font-medium uppercase tracking-wider text-text-faint" for="log-level">Level</label>
						<select
							id="log-level"
							bind:value={filterLevel}
							class="w-full rounded-md border border-border bg-bg-surface px-2 py-1.5 text-xs text-text outline-none focus:ring-1 focus:ring-accent"
						>
							<option value="">All levels</option>
							<option value="DEBUG">DEBUG</option>
							<option value="INFO">INFO</option>
							<option value="WARN">WARN</option>
							<option value="ERROR">ERROR</option>
						</select>
					</div>
					<div class="space-y-1">
						<label class="text-[10px] font-medium uppercase tracking-wider text-text-faint" for="log-pattern">Search pattern</label>
						<div class="relative">
							<MagnifyingGlass size={12} class="absolute left-2 top-1/2 -translate-y-1/2 text-text-faint" />
							<input
								id="log-pattern"
								type="text"
								placeholder="Filter messages..."
								bind:value={filterPattern}
								onkeydown={(e) => { if (e.key === 'Enter') applyFilters(); }}
								class="w-full rounded-md border border-border bg-bg-surface pl-7 pr-2 py-1.5 text-xs text-text outline-none focus:ring-1 focus:ring-accent placeholder:text-text-faint"
							/>
						</div>
					</div>
					<div class="space-y-1">
						<label class="text-[10px] font-medium uppercase tracking-wider text-text-faint" for="log-stream">Stream</label>
						<input
							id="log-stream"
							type="text"
							placeholder="Stream name..."
							bind:value={filterStream}
							onkeydown={(e) => { if (e.key === 'Enter') applyFilters(); }}
							class="w-full rounded-md border border-border bg-bg-surface px-2 py-1.5 text-xs text-text outline-none focus:ring-1 focus:ring-accent placeholder:text-text-faint"
						/>
					</div>
					<div class="flex items-end gap-2">
						<button
							type="button"
							onclick={applyFilters}
							class="rounded-md border border-accent-strong bg-accent-muted px-3 py-1.5 text-xs text-accent hover:bg-accent/20 transition-colors"
						>
							Apply
						</button>
						<button
							type="button"
							onclick={clearFilters}
							class="rounded-md border border-border px-3 py-1.5 text-xs text-text-muted hover:bg-bg-surface transition-colors"
						>
							Clear
						</button>
					</div>
				</div>
			</div>
		{/if}

		<!-- Events list -->
		{#if eventsError}
			<div class="rounded-lg border border-red/20 bg-red-muted px-4 py-3 text-xs text-red">
				{eventsError}
			</div>
		{:else if eventsLoading && events.length === 0}
			<div class="rounded-lg border border-border overflow-hidden">
				<div class="p-3 space-y-2">
					{#each Array(8) as _, i (i)}
						<Skeleton class="h-6 w-full" />
					{/each}
				</div>
			</div>
		{:else if events.length === 0}
			<EmptyState message="No log events found. Try adjusting your filters or invoke a function." icon={Scroll} />
		{:else}
			<div class="rounded-lg border border-border overflow-hidden">
				<div class="divide-y divide-border max-h-[calc(100vh-16rem)] overflow-y-auto font-mono text-xs">
					{#each events as event, i (event.timestamp + '-' + i)}
						<div
							class={`flex gap-3 px-3 py-1.5 transition-colors items-start border-l-2 ${
								selectedGroupIsLambda
									? isLambdaOutputEvent(event)
										? 'border-accent bg-accent/8 hover:bg-accent/12'
										: 'border-transparent bg-bg-surface/35 hover:bg-bg-surface/60 opacity-80'
									: 'border-transparent hover:bg-bg-surface/50'
							}`}
						>
							<span class="text-text-faint whitespace-nowrap shrink-0 w-[140px] tabular-nums">
								{new Date(event.timestamp).toLocaleTimeString()}.{String(new Date(event.timestamp).getMilliseconds()).padStart(3, '0')}
							</span>
							<span class="shrink-0 w-[52px]">
								<Badge variant={levelColor(event.level)} class="text-[10px] px-1.5 py-0">{event.level}</Badge>
							</span>
							{#if selectedGroupIsLambda}
								<span
									class={`hidden xl:inline-flex shrink-0 items-center rounded-full border px-1.5 py-0.5 text-[10px] uppercase tracking-wide ${
										isLambdaOutputEvent(event)
											? 'border-accent-strong bg-accent-muted text-accent'
											: 'border-border bg-bg-surface text-text-faint'
									}`}
								>
									{isLambdaOutputEvent(event) ? 'Output' : 'Runtime'}
								</span>
							{/if}
							<span class={`break-all whitespace-pre-wrap flex-1 min-w-0 ${selectedGroupIsLambda && isLambdaOutputEvent(event) ? 'text-text font-medium' : 'text-text'}`}>
								{event.message}
							</span>
							{#if event.streamName}
								<span class="text-text-faint whitespace-nowrap shrink-0 hidden lg:inline" title={event.streamName}>
									{event.streamName.length > 24 ? event.streamName.slice(-24) : event.streamName}
								</span>
							{/if}
						</div>
					{/each}
				</div>
			</div>

			<!-- Pagination -->
			{#if eventsTotal > eventsLimit}
				<div class="flex items-center justify-between px-1">
					<p class="text-xs text-text-faint">{pageInfo}</p>
					<div class="flex items-center gap-2">
						<button
							type="button"
							onclick={prevPage}
							disabled={!hasPrevPage}
							class="rounded-md border border-border px-2.5 py-1 text-xs text-text-muted hover:bg-bg-surface transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
						>
							Previous
						</button>
						<button
							type="button"
							onclick={nextPage}
							disabled={!hasNextPage}
							class="rounded-md border border-border px-2.5 py-1 text-xs text-text-muted hover:bg-bg-surface transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
						>
							Next
						</button>
					</div>
				</div>
			{/if}
		{/if}
	</div>
{:else}
	<!-- ── Groups list ──────────────────────────────────────────────── -->
	<div class="space-y-3">
		<!-- Header -->
		<div class="flex items-center justify-between gap-3 rounded-lg border border-border bg-bg-raised px-4 py-3">
			<div class="flex items-center gap-3">
				<div class="flex items-center justify-center h-8 w-8 rounded-md bg-accent/10">
					<Scroll size={16} class="text-accent" />
				</div>
				<div>
					<h2 class="text-sm font-semibold text-text">Log Groups</h2>
					<p class="text-[10px] text-text-faint font-mono">{groupsCountLabel}</p>
				</div>
			</div>
			<button
				type="button"
				onclick={loadGroups}
				disabled={groupsLoading}
				class="inline-flex items-center gap-1.5 rounded-md border border-accent-strong bg-accent-muted px-2.5 py-1 text-xs text-accent hover:bg-accent/20 transition-colors disabled:opacity-50"
			>
				<ArrowsClockwise size={12} class={groupsLoading ? 'animate-spin' : ''} />
				Refresh
			</button>
		</div>

		<div class="rounded-lg border border-border bg-bg-raised px-4 py-3 space-y-3">
			<div class="relative">
				<MagnifyingGlass size={12} class="absolute left-2 top-1/2 -translate-y-1/2 text-text-faint" />
				<input
					type="text"
					placeholder="Search services or log groups..."
					bind:value={groupSearch}
					class="w-full rounded-md border border-border bg-bg-surface pl-7 pr-2 py-2 text-xs text-text outline-none focus:ring-1 focus:ring-accent placeholder:text-text-faint"
				/>
			</div>
			<div class="flex flex-wrap gap-2">
				{#each serviceOptions as option (option.key)}
					<button
						type="button"
						onclick={() => serviceFilter = option.key}
						class="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs transition-colors {serviceFilter === option.key ? 'border-accent-strong bg-accent-muted text-accent' : 'border-border text-text-muted hover:text-text hover:bg-bg-surface'}"
					>
						<span>{option.label}</span>
						<span class="rounded-full bg-black/5 px-1.5 py-0.5 text-[10px] tabular-nums dark:bg-white/10">{option.count}</span>
					</button>
				{/each}
			</div>
		</div>

		<!-- Error state -->
		{#if groupsError}
			<div class="rounded-lg border border-red/20 bg-red-muted px-4 py-3 text-xs text-red">
				{groupsError}
			</div>
		{/if}

		<!-- Loading state -->
		{#if groupsLoading}
			<div class="space-y-2">
				{#each Array(4) as _, i (i)}
					<Skeleton class="h-16 w-full rounded-lg" />
				{/each}
			</div>
		{:else if groups.length === 0}
			<EmptyState message="No log groups yet. Create a Lambda function or invoke one to see logs." icon={Scroll} />
		{:else if filteredGroups.length === 0}
			<EmptyState message="No log groups match the current service or search filters." icon={Scroll} />
		{:else}
			<div class="grid grid-cols-1 gap-2">
				{#each filteredGroups as group (group.name)}
					<button
						type="button"
						onclick={() => selectGroup(group.name)}
						class="flex items-center justify-between gap-4 rounded-lg border border-border bg-bg-raised px-4 py-3 text-left hover:border-accent/40 hover:bg-bg-surface/50 transition-colors group"
					>
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-2 mb-1">
								<Badge variant="secondary" class="text-[10px]">{groupCategory(group.name)}</Badge>
								<span class="text-sm font-medium text-text truncate">{groupDisplayName(group.name)}</span>
							</div>
							<p class="text-[10px] font-mono text-text-faint truncate">{group.name}</p>
						</div>
						<div class="flex items-center gap-4 shrink-0 text-right">
							<div>
								<p class="text-sm font-semibold text-text tabular-nums">{group.eventCount}</p>
								<p class="text-[10px] text-text-faint">events</p>
							</div>
							<div>
								<p class="text-sm font-semibold text-text tabular-nums">{group.streamCount}</p>
								<p class="text-[10px] text-text-faint">streams</p>
							</div>
							{#if group.lastEvent}
								<div class="hidden sm:block">
									<p class="text-xs text-text-muted tabular-nums">{new Date(group.lastEvent).toLocaleTimeString()}</p>
									<p class="text-[10px] text-text-faint">last event</p>
								</div>
							{/if}
						</div>
					</button>
				{/each}
			</div>
		{/if}
	</div>
{/if}
