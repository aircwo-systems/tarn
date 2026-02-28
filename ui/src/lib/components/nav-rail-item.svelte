<script lang="ts">
	import { cn } from '$lib/utils';

	let {
		icon: Icon,
		label,
		active = false,
		collapsed = false,
		onclick
	}: {
		icon: any;
		label: string;
		active?: boolean;
		collapsed?: boolean;
		onclick?: () => void;
	} = $props();
</script>

<button
	type="button"
	class={cn(
		'group flex items-center gap-2.5 w-full px-2.5 py-1.5 rounded-md text-sm transition-colors relative',
		active
			? 'text-text bg-bg-surface border-l-2 border-l-accent pl-2'
			: 'text-text-muted hover:text-text hover:bg-bg-surface/50 border-l-2 border-l-transparent pl-2'
	)}
	{onclick}
	aria-current={active ? 'page' : undefined}
>
	<Icon size={16} weight={active ? 'fill' : 'regular'} />
	{#if !collapsed}
		<span class="truncate">{label}</span>
	{/if}
</button>
