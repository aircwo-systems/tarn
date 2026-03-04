<script lang="ts">
	export let connInfraNodes: Array<{ id: string; label: string }>;
	export let moveInfraNode: (id: string, direction: -1 | 1) => void;
</script>

<div class="border-b border-border px-3 py-2">
	<div class="flex flex-wrap items-center gap-2">
		<span class="text-[10px] font-mono uppercase tracking-wide text-text-faint">Infra Order</span>
		{#each connInfraNodes as node, index (node.id)}
			<div class="inline-flex items-center gap-1 rounded-md border border-border bg-bg-surface px-1.5 py-1">
				<span class="text-[10px] text-text-muted">{node.label}</span>
				<div class="inline-flex gap-0.5">
					<button
						type="button"
						class="inline-flex h-5 w-5 items-center justify-center rounded border border-border text-text-faint disabled:opacity-40"
						disabled={index === 0}
						on:click={() => moveInfraNode(node.id, -1)}
						aria-label={`Move ${node.label} up`}
					>
						<slot name="caretUp" />
					</button>
					<button
						type="button"
						class="inline-flex h-5 w-5 items-center justify-center rounded border border-border text-text-faint disabled:opacity-40"
						disabled={index === connInfraNodes.length - 1}
						on:click={() => moveInfraNode(node.id, 1)}
						aria-label={`Move ${node.label} down`}
					>
						<slot name="caretDown" />
					</button>
				</div>
			</div>
		{/each}
	</div>
</div>
