<script lang="ts">
  import type { Snippet } from "svelte";
  import "../app.css";
  import { resolve } from "$app/paths";
  import {
    SquaresFourIcon,
    CpuIcon,
    ScrollIcon,
    BookOpenIcon,
    LayoutIcon,
  } from "phosphor-svelte";
  import AppShell from "$lib/components/AppShell.svelte";
  import type { NavigationItem } from "$lib/components/Sidebar.svelte";
  import { createDemo } from "$lib/demo/store.svelte";
  import { createShell } from "$lib/shell.svelte";
  let { children }: { children: Snippet } = $props();
  const demo = createDemo();
  createShell();
  const navigation = $derived<NavigationItem[]>([
    {
      href: resolve("/"),
      label: "Overview",
      icon: SquaresFourIcon,
      group: "Workspace",
      description: "A summary of the system and its connections.",
      details: [
        { label: "Sample resources", value: demo.data.workers.length },
        {
          label: "Need attention",
          value: demo.data.workers.filter((worker) => worker.status === "error")
            .length,
        },
      ],
    },
    {
      href: resolve("/workers"),
      label: "Workers",
      icon: CpuIcon,
      group: "Workspace",
      count: demo.data.workers.length,
      description: "Search the example inventory and inspect details.",
      details: [
        {
          label: "Running",
          value: demo.data.workers.filter(
            (worker) => worker.status === "running",
          ).length,
        },
      ],
    },
    {
      href: resolve("/activity"),
      label: "Activity",
      icon: ScrollIcon,
      group: "Observe",
      description: "Recent events and changes in this sample session.",
      details: [{ label: "Sample events", value: demo.data.activity.length }],
    },
    {
      href: resolve("/foundation"),
      label: "UI foundation",
      icon: BookOpenIcon,
      group: "Build",
      description: "Controls, typography and reusable interface states.",
      details: [{ label: "Themes", value: "Dark / Light" }],
    },
    {
      href: resolve("/section-template"),
      label: "Layout examples",
      icon: LayoutIcon,
      group: "Build",
      description: "A resource browser and a skeleton for the next section.",
      details: [{ label: "Examples", value: "Resource / Skeleton" }],
    },
  ]);
</script>

<AppShell {navigation} settingsHref={resolve("/settings")}
  >{@render children()}</AppShell
>
