<script lang="ts">
  import { cn } from "$lib/utils";

  let {
    icon: Icon,
    label,
    active = false,
    collapsed = false,
    onclick,
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
    "group flex items-center w-full py-1.5 rounded-md text-sm transition-colors relative",
    collapsed ? "justify-center px-0" : "gap-2.5 px-2.5",
    active
      ? collapsed
        ? "text-sidebar-foreground bg-sidebar-accent"
        : "text-sidebar-foreground bg-sidebar-accent border-l-2 border-l-sidebar-primary pl-2"
      : collapsed
        ? "text-sidebar-foreground/70 hover:text-sidebar-foreground hover:bg-sidebar-accent/50"
        : "text-sidebar-foreground/70 hover:text-sidebar-foreground hover:bg-sidebar-accent/50 border-l-2 border-l-transparent pl-2",
  )}
  {onclick}
  aria-current={active ? "page" : undefined}
  title={collapsed ? label : undefined}
>
  <span class="shrink-0 flex items-center justify-center" class:w-8={collapsed}>
    <Icon size={16} weight={active ? "fill" : "regular"} />
  </span>
  {#if !collapsed}
    <span class="truncate">{label}</span>
  {/if}
</button>
