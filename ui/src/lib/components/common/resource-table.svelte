<script lang="ts">
	import { Table, TableHeader, TableBody, TableRow, TableHead } from '$lib/components/ui/table';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { ArrowClockwiseIcon } from 'phosphor-svelte';
	import EmptyState from './empty-state.svelte';

	let {
		title,
		count = 0,
		loading = false,
		empty = false,
		emptyMessage = 'No items created yet.',
		emptyIcon,
		columns = [],
		onRefresh,
		children
	}: {
		title: string;
		count?: number;
		loading?: boolean;
		empty?: boolean;
		emptyMessage?: string;
		emptyIcon?: any;
		columns?: string[];
		onRefresh?: () => void;
		children?: import('svelte').Snippet;
	} = $props();
</script>

<div class="rounded-lg border border-border overflow-hidden">
	<div class="flex items-center justify-between px-3 py-2 border-b border-border bg-bg-raised">
		<h3 class="text-sm font-semibold text-text">{title}</h3>
		<div class="flex items-center gap-2">
			<span class="text-xs text-text-faint font-mono">{count} items</span>
			{#if onRefresh}
				<button
					type="button"
					onclick={onRefresh}
					class="flex h-6 w-6 items-center justify-center rounded text-text-faint hover:text-text hover:bg-bg-overlay transition-colors"
					aria-label="Refresh"
					title="Refresh"
				>
					<ArrowClockwiseIcon size={13} />
				</button>
			{/if}
		</div>
	</div>

	{#if loading}
		<div class="p-3 space-y-2">
			{#each Array(3) as _}
				<Skeleton class="h-8 w-full" />
			{/each}
		</div>
	{:else if empty}
		<EmptyState message={emptyMessage} icon={emptyIcon} />
	{:else}
		<Table>
			<TableHeader>
				<TableRow>
					{#each columns as col}
						<TableHead>{col}</TableHead>
					{/each}
				</TableRow>
			</TableHeader>
			<TableBody>
				{@render children?.()}
			</TableBody>
		</Table>
	{/if}
</div>
