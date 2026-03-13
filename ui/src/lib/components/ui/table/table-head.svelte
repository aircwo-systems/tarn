<script lang="ts">
	import { cn } from '$lib/utils';

	let {
		class: className,
		children,
		...rest
	}: { class?: string; children?: import('svelte').Snippet; [key: string]: any } = $props();
</script>

<th
	class={cn(
		'h-8 px-3 text-left align-middle font-mono text-[10px] font-medium uppercase tracking-wider text-muted-foreground',
		className
	)}
	{...rest}
>
	{@render children?.()}
</th>
