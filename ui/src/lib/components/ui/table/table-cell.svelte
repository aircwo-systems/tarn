<script lang="ts">
	import { cn, type WithElementRef } from "$lib/lib/utils.js";
	import type { HTMLTdAttributes } from "svelte/elements";

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLTdAttributes> = $props();
</script>

<td
	bind:this={ref}
	data-slot="table-cell"
	class={cn(
		"px-3 py-2.5 align-middle text-[12.5px] leading-snug whitespace-nowrap overflow-hidden [&:has([role=checkbox])]:pr-0",
		className
	)}
	{...restProps}
>
	{@render children?.()}
</td>
