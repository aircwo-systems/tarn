<script lang="ts">
	import { cn } from '$lib/utils';

	let {
		class: className,
		children,
		...rest
	}: { class?: string; children?: import('svelte').Snippet; [key: string]: any } = $props();
</script>

<div class={cn('flex flex-col gap-1 px-3 py-2', className)} {...rest}>
	{@render children?.()}
</div>
