<script lang="ts">
  import { cn } from "$lib/utils";

  let {
    icon: Icon,
    label,
    active = false,
    collapsed = false,
    count = null,
    onclick,
  }: {
    icon: any;
    label: string;
    active?: boolean;
    collapsed?: boolean;
    count?: number | null;
    onclick?: () => void;
  } = $props();
</script>

<button
  type="button"
  class={cn(
    "group flex w-full items-center rounded-md font-mono text-[12px] transition-colors",
    collapsed ? "justify-center px-0 py-1.5" : "gap-2 px-2 py-1.5",
    active
      ? "bg-primary/[0.06] text-primary"
      : "text-muted-foreground/70 hover:bg-sidebar-foreground/[0.04] hover:text-foreground",
  )}
  {onclick}
  aria-current={active ? "page" : undefined}
  title={collapsed ? label : undefined}
>
  <span
    class={cn(
      "flex shrink-0 items-center justify-center",
      collapsed && "w-8",
    )}
  >
    <Icon size={15} weight={active ? "fill" : "regular"} />
  </span>
  {#if !collapsed}
    <span class="truncate">{label}</span>
    {#if count != null && count > 0}
      <span
        class={cn(
          "ml-auto font-mono text-[10px] font-medium tabular-nums",
          active ? "text-primary" : "text-muted-foreground/40",
        )}
      >
        {count}
      </span>
    {/if}
  {/if}
</button>
