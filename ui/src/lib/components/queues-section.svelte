<script lang="ts">
	import { ChatCircleIcon } from 'phosphor-svelte';
	import { TableRow, TableCell } from '$lib/components/ui/table';
	import Badge from '$lib/components/ui/badge/badge.svelte';
	import ResourceTable from './resource-table.svelte';
	import ArnCell from './arn-cell.svelte';
	import { fetchQueueMessages } from '$lib/api';
	import { getDashboard, getDashboardFilters, matchesTagFilter } from '$lib/state.svelte';
	import type { QueueMessageSummary } from '$lib/types';
	import { formatUnixSeconds } from '$lib/utils';

	const dashboard = getDashboard();
	const filters = getDashboardFilters();
	const queues = $derived(
		(dashboard.data?.queues ?? []).filter((queue) => matchesTagFilter(queue.tags, filters.tagFilter))
	);

	let selectedQueueName = $state('');
	let selectedMessages = $state<QueueMessageSummary[]>([]);
	let selectedLoading = $state(false);
	let selectedError = $state('');
	let requestToken = 0;

	const selectedQueue = $derived(queues.find((queue) => queue.name === selectedQueueName) ?? null);

	$effect(() => {
		if (selectedQueueName && !queues.some((queue) => queue.name === selectedQueueName)) {
			selectedQueueName = '';
			selectedMessages = [];
			selectedError = '';
		}
	});

	async function selectQueue(queueName: string) {
		selectedQueueName = queueName;
		await loadQueueMessages(queueName);
	}

	async function refreshSelectedQueueMessages() {
		if (!selectedQueueName) return;
		await loadQueueMessages(selectedQueueName);
	}

	async function loadQueueMessages(queueName: string) {
		const token = ++requestToken;
		selectedLoading = true;
		selectedError = '';
		try {
			const messages = await fetchQueueMessages(queueName);
			if (token !== requestToken) return;
			selectedMessages = messages;
		} catch (error) {
			if (token !== requestToken) return;
			const queue = queues.find((item) => item.name === queueName);
			selectedMessages = queue?.recentMessages ?? [];
			selectedError = error instanceof Error ? error.message : 'Failed to load queue messages';
		} finally {
			if (token === requestToken) {
				selectedLoading = false;
			}
		}
	}

	function onQueueRowKeydown(event: KeyboardEvent, queueName: string) {
		if (event.key === 'Enter' || event.key === ' ') {
			event.preventDefault();
			void selectQueue(queueName);
		}
	}

	function formatSentAt(ms: number): string {
		if (!ms) return '--';
		return new Date(ms).toLocaleString();
	}

	function messageBadgeVariant(state: string): 'default' | 'amber' | 'secondary' {
		if (state === 'visible') return 'default';
		if (state === 'inflight') return 'amber';
		return 'secondary';
	}
</script>

<div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
	<ResourceTable
		title="SQS Queues"
		count={queues.length}
		loading={dashboard.loading && !dashboard.data}
		empty={queues.length === 0}
		emptyMessage="No queues created yet."
		emptyIcon={ChatCircleIcon}
		columns={['Queue', 'Type', 'Visible', 'In Flight', 'Delayed', 'Messages', 'Visibility', 'Long Poll', 'Created']}
	>
		{#each queues as queue}
			<TableRow
				class={`cursor-pointer focus-within:bg-bg-surface/60 ${queue.name === selectedQueueName ? 'bg-bg-surface/60' : ''}`}
				role="button"
				tabindex="0"
				aria-label={`Open messages for queue ${queue.name}`}
				onclick={() => void selectQueue(queue.name)}
				onkeydown={(event: KeyboardEvent) => onQueueRowKeydown(event, queue.name)}
			>
				<TableCell><ArnCell name={queue.name} arn={queue.url} /></TableCell>
				<TableCell>
					<Badge variant={queue.fifo ? 'amber' : 'secondary'}>
						{queue.fifo ? 'FIFO' : 'Standard'}
					</Badge>
				</TableCell>
				<TableCell class="font-mono text-text-muted">{queue.approxVisible}</TableCell>
				<TableCell class="font-mono text-text-muted">{queue.approxInFlight}</TableCell>
				<TableCell class="font-mono text-text-muted">{queue.approxDelayed}</TableCell>
				<TableCell class="min-w-[18rem]">
					{#if queue.recentMessages?.length}
						<div class="space-y-1.5">
							{#each queue.recentMessages.slice(0, 3) as message}
								<div class="rounded-md border border-border bg-bg-subtle/70 px-2 py-1.5">
									<div class="mb-1 flex items-center gap-2 text-[10px] uppercase tracking-wide text-text-faint">
										<Badge variant={messageBadgeVariant(message.state)}>
											{message.state}
										</Badge>
										<span class="font-mono">{message.id.slice(0, 8)}</span>
										{#if message.receiveCount > 0}
											<span class="font-mono">x{message.receiveCount}</span>
										{/if}
									</div>
									<p class="max-h-9 overflow-hidden break-all text-xs text-text-muted" title={message.body}>
										{message.body || '(empty)'}
									</p>
								</div>
							{/each}
							{#if queue.recentMessages.length > 3}
								<p class="text-[11px] text-text-faint">+{queue.recentMessages.length - 3} more messages</p>
							{/if}
						</div>
					{:else}
						<span class="text-xs text-text-faint">No messages</span>
					{/if}
				</TableCell>
				<TableCell class="font-mono text-text-muted">{queue.visibilitySec}s</TableCell>
				<TableCell class="font-mono text-text-muted">{queue.waitTimeSec}s</TableCell>
				<TableCell class="text-text-faint text-xs">{formatUnixSeconds(queue.createdTimestamp)}</TableCell>
			</TableRow>
		{/each}
	</ResourceTable>

	<section class="rounded-lg border border-border bg-bg-raised">
		<div class="flex items-center justify-between border-b border-border px-3 py-2">
			<h3 class="text-sm font-semibold text-text">Queue Messages</h3>
			{#if selectedQueue}
				<button
					type="button"
					class="rounded-md border border-border px-2 py-1 text-xs text-text-muted hover:bg-bg-subtle"
					onclick={() => void refreshSelectedQueueMessages()}
					disabled={selectedLoading}
				>
					Refresh
				</button>
			{/if}
		</div>

		{#if !selectedQueue}
			<p class="px-3 py-5 text-sm text-text-faint">Click a queue row to inspect pending messages.</p>
		{:else}
			<div class="border-b border-border px-3 py-2 text-xs text-text-faint">
				<p class="truncate font-mono text-text">{selectedQueue.name}</p>
				<p class="mt-1">
					{selectedQueue.approxVisible} visible, {selectedQueue.approxInFlight} in flight, {selectedQueue.approxDelayed} delayed
				</p>
			</div>

			{#if selectedError}
				<p class="border-b border-border px-3 py-2 text-xs text-red-300">{selectedError}</p>
			{/if}

			<div class="max-h-[36rem] space-y-2 overflow-y-auto px-3 py-3">
				{#if selectedLoading}
					<p class="text-sm text-text-faint">Loading messages...</p>
				{:else if selectedMessages.length === 0}
					<p class="text-sm text-text-faint">No messages available for this queue.</p>
				{:else}
					{#each selectedMessages as message}
						<div class="rounded-md border border-border bg-bg-subtle/70 p-2">
							<div class="mb-1.5 flex flex-wrap items-center gap-2 text-[11px] text-text-faint">
								<Badge variant={messageBadgeVariant(message.state)}>
									{message.state}
								</Badge>
								<span class="font-mono">{message.id}</span>
								{#if message.receiveCount > 0}
									<span class="font-mono">receives: {message.receiveCount}</span>
								{/if}
							</div>
							<p class="break-all text-xs text-text-muted">{message.body || '(empty)'}</p>
							<p class="mt-1 text-[11px] text-text-faint">{formatSentAt(message.sentAt)}</p>
						</div>
					{/each}
				{/if}
			</div>
		{/if}
	</section>
</div>
