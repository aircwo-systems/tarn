<script lang="ts">
	import { cn } from '$lib/utils';

	let {
		class: className,
		children,
		...rest
	}: { class?: string; children?: import('svelte').Snippet; [key: string]: any } = $props();
</script>

<td class={cn('px-3 py-2 align-middle text-sm', className)} {...rest}>
	{@render children?.()}
</td>
