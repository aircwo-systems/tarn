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

<section class="rounded-lg border border-border bg-card px-4 py-3">
	<div class="flex flex-wrap items-center gap-3">
		<div class="flex items-center gap-2 text-xs font-medium text-foreground">
			<FunnelSimpleIcon size={14} class="text-primary" />
			<span>Tag Filter</span>
		</div>
		<div class="relative min-w-[16rem] flex-1">
			<MagnifyingGlassIcon size={12} class="absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground/70" />
			<input
				type="text"
				placeholder="Filter by tag value or key:value"
				bind:value={draft}
				onkeydown={(event) => {
					if (event.key === 'Enter') apply();
				}}
				class="w-full rounded-md border border-border bg-muted pl-7 pr-8 py-2 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary placeholder:text-muted-foreground/70"
			/>
			{#if draft}
				<button
					type="button"
					class="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground/70 hover:text-foreground"
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
			class="rounded-md border border-primary/50 bg-primary/10 px-3 py-1.5 text-xs text-primary hover:bg-primary/20"
		>
			Apply
		</button>
		{#if filters.tagFilter}
			<span class="rounded-full border border-border px-2.5 py-1 text-[11px] font-mono text-muted-foreground/70">
				{filters.tagFilter}
			</span>
		{/if}
	</div>
</section>
