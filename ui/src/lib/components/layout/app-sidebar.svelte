<script lang="ts">
  import { GearIcon } from "phosphor-svelte";
  import TarnLogo from "$lib/components/common/tarn-logo.svelte";
  import ThemeToggle from "$lib/components/layout/theme-toggle.svelte";
  import type { Component } from "svelte";

  export interface NavItem {
    id: string;
    label: string;
    icon: Component<any>;
    count: number | null;
  }

  export interface NavSection {
    id: string;
    label: string;
    items: NavItem[];
  }

  let {
    navSections,
    activeTab,
    sidebarCollapsed = $bindable(false),
    connectionStatus,
    region,
    pollingIntervalSeconds,
    onSetTab,
    onOpenSettings,
  }: {
    navSections: NavSection[];
    activeTab: string;
    sidebarCollapsed?: boolean;
    connectionStatus: "ok" | "loading" | "error" | "idle";
    region?: string;
    pollingIntervalSeconds: number;
    onSetTab: (tab: string) => void;
    onOpenSettings: () => void;
  } = $props();
</script>

<aside
  class="flex h-full shrink-0 flex-col overflow-hidden border-r border-border transition-[width] duration-200
  {sidebarCollapsed ? 'w-11' : 'w-[196px]'}"
>
  <!-- Brand -->
  <div class="flex h-[52px] shrink-0 items-center gap-2 overflow-hidden px-2.5">
    <TarnLogo class="h-7 w-7 shrink-0" color="var(--color-primary)" />
    {#if !sidebarCollapsed}
      <div class="min-w-0">
        <p class="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground/60">Tarn</p>
        <p class="truncate text-[13px] font-semibold text-foreground">Rack Console</p>
      </div>
    {/if}
  </div>

  <div class="h-px bg-border"></div>

  <!-- Nav -->
  <nav class="flex flex-1 flex-col overflow-y-auto px-1.5 py-2">
    {#each navSections as section, sectionIndex (section.id)}
      <div class:mt-3={sectionIndex > 0} class="flex flex-col gap-px">
        {#if !sidebarCollapsed}
          <div class="px-2 pb-1 pt-1 text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground/40">
            {section.label}
          </div>
        {:else if sectionIndex > 0}
          <div class="mx-1.5 my-1 h-px bg-border"></div>
        {/if}

        {#each section.items as item (item.id)}
          {@const Icon = item.icon}
          {@const active = item.id === activeTab}
          <button
            type="button"
            title={sidebarCollapsed ? item.label : undefined}
            onclick={() => onSetTab(item.id)}
            class="flex w-full items-center rounded-[5px] px-2 py-1.5 text-[12px] transition-colors
              {sidebarCollapsed ? 'justify-center' : 'gap-2 text-left'}
              {active
              ? 'bg-primary/[0.06] text-primary'
              : 'text-muted-foreground hover:bg-white/[0.04] hover:text-foreground'}"
            aria-current={active ? "page" : undefined}
          >
            <Icon
              size={15}
              class="shrink-0 {active ? 'opacity-100' : 'opacity-55'}"
              weight={active ? "fill" : "regular"}
            />
            {#if !sidebarCollapsed}
              <span class="flex-1 truncate">{item.label}</span>
              {#if item.count !== null && item.count > 0}
                <span
                  class="text-right text-[10px] font-medium tabular-nums {active
                    ? 'text-primary'
                    : 'text-muted-foreground/50'}"
                >
                  {item.count}
                </span>
              {/if}
            {/if}
          </button>
        {/each}
      </div>
    {/each}
  </nav>

  <!-- Footer -->
  <div class="border-t border-border px-1.5 py-1.5">
    {#if !sidebarCollapsed}
      <div class="flex items-center gap-1">
        <div class="flex flex-1 items-center gap-1.5 px-1 text-[10px] text-muted-foreground/50">
          <span
            class="inline-block h-1.5 w-1.5 shrink-0 rounded-full
            {connectionStatus === 'ok'
              ? 'bg-primary shadow-[0_0_5px_var(--color-primary)]'
              : connectionStatus === 'loading'
                ? 'bg-amber-400'
                : 'bg-destructive'}"
          ></span>
          <span class="truncate">
            {connectionStatus === "ok"
              ? (region ?? "connected")
              : connectionStatus === "loading"
                ? "connecting…"
                : "error"}
          </span>
        </div>
        <ThemeToggle />
        <button
          type="button"
          onclick={onOpenSettings}
          class="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          aria-label="Settings"
        >
          <GearIcon size={15} />
        </button>
      </div>
    {:else}
      <div class="flex flex-col items-center gap-1">
        <ThemeToggle />
        <button
          type="button"
          onclick={onOpenSettings}
          class="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          aria-label="Settings"
        >
          <GearIcon size={15} />
        </button>
      </div>
    {/if}
  </div>
</aside>
