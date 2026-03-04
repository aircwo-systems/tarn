<script lang="ts">
	import { FunnelSimpleIcon, MagnifyingGlassIcon, XIcon } from 'phosphor-svelte';
	import { getDashboardFilters, setDashboardTagFilter } from '$lib/state.svelte';

	const filters = getDashboardFilters();
	let draft = $state(filters.tagFilter);

	$effect(() => {
		draft = filters.tagFilter;
	});

	function apply() {
		setDashboardTagFilter(draft);
	}

	function clear() {
		draft = '';
		setDashboardTagFilter('');
	}
</script>

<section class="rounded-lg border border-border bg-bg-raised px-4 py-3">
	<div class="flex flex-wrap items-center gap-3">
		<div class="flex items-center gap-2 text-xs font-medium text-text">
			<FunnelSimpleIcon size={14} class="text-accent" />
			<span>Tag Filter</span>
		</div>
		<div class="relative min-w-[16rem] flex-1">
			<MagnifyingGlassIcon size={12} class="absolute left-2 top-1/2 -translate-y-1/2 text-text-faint" />
			<input
				type="text"
				placeholder="Filter by tag value or key:value"
				bind:value={draft}
				onkeydown={(event) => {
					if (event.key === 'Enter') apply();
				}}
				class="w-full rounded-md border border-border bg-bg-surface pl-7 pr-8 py-2 text-xs text-text outline-none focus:ring-1 focus:ring-accent placeholder:text-text-faint"
			/>
			{#if draft}
				<button
					type="button"
					class="absolute right-2 top-1/2 -translate-y-1/2 text-text-faint hover:text-text"
					onclick={clear}
					aria-label="Clear tag filter"
				>
					<XIcon size={12} />
				</button>
			{/if}
		</div>
		<button
			type="button"
			onclick={apply}
			class="rounded-md border border-accent-strong bg-accent-muted px-3 py-1.5 text-xs text-accent hover:bg-accent/20"
		>
			Apply
		</button>
		{#if filters.tagFilter}
			<span class="rounded-full border border-border px-2.5 py-1 text-[11px] font-mono text-text-faint">
				{filters.tagFilter}
			</span>
		{/if}
	</div>
</section>
