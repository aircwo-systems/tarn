<script lang="ts">
	export let viewMode: 'components' | 'connections';
	export let setViewMode: (mode: 'components' | 'connections') => void;
	export let canvasExpanded: boolean;
	export let setCanvasExpanded: (expanded: boolean) => void;
	export let resourceCount: number;
	export let linkCount: number;
</script>

<div class="flex items-center justify-between px-3 py-2 border-b border-border gap-3">
	<h3 class="text-xs font-mono uppercase tracking-wider text-text-muted">Topology</h3>

	<div class="inline-flex rounded-md border border-border bg-bg-surface p-0.5">
		<button
			type="button"
			class={`px-2.5 py-1 text-[10px] font-mono uppercase tracking-wide rounded-sm transition ${
				viewMode === 'components'
					? 'bg-bg-overlay text-text'
					: 'text-text-faint hover:text-text-muted'
			}`}
			on:click={() => setViewMode('components')}
		>
			Component View
		</button>
		<button
			type="button"
			class={`px-2.5 py-1 text-[10px] font-mono uppercase tracking-wide rounded-sm transition ${
				viewMode === 'connections'
					? 'bg-bg-overlay text-text'
					: 'text-text-faint hover:text-text-muted'
			}`}
			on:click={() => setViewMode('connections')}
		>
			Connection View
		</button>
	</div>

	<div class="flex items-center gap-2">
		<span class="text-[10px] text-text-faint font-mono">
			{viewMode === 'components'
				? `${resourceCount} resources`
				: `${linkCount} links`}
		</span>
		<button
			type="button"
			class="inline-flex h-7 w-7 items-center justify-center rounded-md border border-border text-text-muted transition-colors hover:bg-bg-overlay hover:text-text"
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
