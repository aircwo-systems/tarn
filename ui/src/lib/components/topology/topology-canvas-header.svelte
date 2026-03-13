<script lang="ts">
	export let viewMode: 'components' | 'connections';
	export let setViewMode: (mode: 'components' | 'connections') => void;
	export let canvasExpanded: boolean;
	export let setCanvasExpanded: (expanded: boolean) => void;
	export let resourceCount: number;
	export let linkCount: number;
</script>

<div class="flex items-center justify-between px-3 py-2 border-b border-border gap-3">
	<h3 class="text-xs font-mono uppercase tracking-wider text-muted-foreground">Topology</h3>

	<div class="inline-flex rounded-md border border-border bg-muted p-0.5">
		<button
			type="button"
			class={`px-2.5 py-1 text-[10px] font-mono uppercase tracking-wide rounded-sm transition ${
				viewMode === 'components'
					? 'bg-popover text-foreground'
					: 'text-muted-foreground/70 hover:text-muted-foreground'
			}`}
			on:click={() => setViewMode('components')}
		>
			Component View
		</button>
		<button
			type="button"
			class={`px-2.5 py-1 text-[10px] font-mono uppercase tracking-wide rounded-sm transition ${
				viewMode === 'connections'
					? 'bg-popover text-foreground'
					: 'text-muted-foreground/70 hover:text-muted-foreground'
			}`}
			on:click={() => setViewMode('connections')}
		>
			Connection View
		</button>
	</div>

	<div class="flex items-center gap-2">
		<span class="text-[10px] text-muted-foreground/70 font-mono">
			{viewMode === 'components'
				? `${resourceCount} resources`
				: `${linkCount} links`}
		</span>
		<button
			type="button"
			class="inline-flex h-7 w-7 items-center justify-center rounded-md border border-border text-muted-foreground transition-colors hover:bg-popover hover:text-foreground"
			aria-label={canvasExpanded ? 'Collapse canvas' : 'Expand canvas'}
			title={canvasExpanded ? 'Collapse canvas' : 'Expand canvas'}
			on:click={() => setCanvasExpanded(!canvasExpanded)}
		>
			{#if canvasExpanded}
				<slot name="collapseIcon" />
			{:else}
				<slot name="expandIcon" />
			{/if}
		</button>
	</div>
</div>
