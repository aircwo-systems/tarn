<script lang="ts">
  import { onMount } from "svelte";
  import NavRail from "$lib/components/layout/nav-rail.svelte";
  import OverviewSection from "$lib/components/sections/overview-section.svelte";
  import APIGatewaysSection from "$lib/components/sections/api-gateways-section.svelte";
  import FunctionsSection from "$lib/components/sections/functions-section.svelte";
  import QueuesSection from "$lib/components/sections/queues-section.svelte";
  import SNSSection from "$lib/components/sections/sns-section.svelte";
  import SecretsSection from "$lib/components/sections/secrets-section.svelte";
  import TriggersSection from "$lib/components/sections/triggers-section.svelte";
  import EventBridgeSection from "$lib/components/sections/eventbridge-section.svelte";
  import StorageSection from "$lib/components/sections/storage-section.svelte";
  import LogsSection from "$lib/components/sections/logs-section.svelte";
  import XraySection from "$lib/components/sections/xray-section.svelte";
  import ChaosSection from "$lib/components/sections/chaos-section.svelte";
  import { getDashboard } from "$lib/state.svelte";
  import ScrollArea from "$lib/components/ui/scroll-area/scroll-area.svelte";

  const dashboard = getDashboard();
  const validTabs = [
    "overview",
    "gateways",
    "functions",
    "queues",
    "sns",
    "secrets",
    "triggers",
    "eventbridge",
    "storage",
    "logs",
    "xray",
    "chaos",
  ];

  let activeTab = $state("overview");
  let logsInitialGroup = $state("");
  let logsInitialTimestamp = $state("");

  function readHash() {
    const raw = window.location.hash.replace("#", "");
    const [tab, qs] = raw.split("?");
    if (validTabs.includes(tab)) {
      activeTab = tab;
    }
    if (tab === "logs" && qs) {
      const params = new URLSearchParams(qs);
      logsInitialGroup = params.get("group") ?? "";
      logsInitialTimestamp = params.get("ts") ?? "";
    }
  }

  function setTab(tab: string) {
    activeTab = tab;
    window.location.hash = tab;
  }

  onMount(() => {
    readHash();
    window.addEventListener("hashchange", readHash);
    return () => window.removeEventListener("hashchange", readHash);
  });
</script>

<svelte:head>
  <title>Rack Console — Tarn</title>
</svelte:head>

<div class="flex min-h-screen bg-background">
  <NavRail {activeTab} onTabChange={setTab} />

  <main
    class="flex-1 min-w-0 px-4 py-4 md:px-6 md:py-5 pb-20 md:pb-5 space-y-4"
  >
    <ScrollArea class="w-full h-screen pr-6 pb-6">
      {#if activeTab === "overview"}
        <OverviewSection onNavigate={setTab} />
      {:else if activeTab === "gateways"}
        <APIGatewaysSection />
      {:else if activeTab === "functions"}
        <FunctionsSection />
      {:else if activeTab === "queues"}
        <QueuesSection />
      {:else if activeTab === "sns"}
        <SNSSection />
      {:else if activeTab === "secrets"}
        <SecretsSection />
      {:else if activeTab === "triggers"}
        <TriggersSection />
      {:else if activeTab === "eventbridge"}
        <EventBridgeSection />
      {:else if activeTab === "storage"}
        <StorageSection />
      {:else if activeTab === "logs"}
        <LogsSection initialGroup={logsInitialGroup} initialTimestamp={logsInitialTimestamp} />
      {:else if activeTab === "xray"}
        <XraySection />
      {:else if activeTab === "chaos"}
        <ChaosSection gateways={dashboard.data?.gateways ?? []} />
      {/if}
    </ScrollArea>
  </main>
</div>
