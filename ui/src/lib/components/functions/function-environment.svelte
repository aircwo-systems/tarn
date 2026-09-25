<script lang="ts">
  import { EyeIcon, EyeSlashIcon } from "phosphor-svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcButton from "$lib/components/rack/rc-button.svelte";
  import { fetchFunctionEnvironment } from "$lib/api";

  let { name, keys }: { name: string; keys: string[] } = $props();

  let variables = $state<Record<string, string> | null>(null);
  let visible = $state(false);
  let loading = $state(false);
  let error = $state("");

  async function toggle() {
    if (visible) {
      visible = false;
      variables = null;
      return;
    }
    loading = true;
    error = "";
    try {
      const result = await fetchFunctionEnvironment(name);
      variables = result.variables;
      visible = true;
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to load environment variables";
    } finally {
      loading = false;
    }
  }
</script>

<RcPanel title="Environment variables" description="{keys.length} configured" index={6}>
  {#snippet actions()}
    {#if keys.length > 0}
      <RcButton small variant={visible ? "default" : "primary"} disabled={loading} onclick={toggle}>
        {#if visible}<EyeSlashIcon size={11} />Hide{:else}<EyeIcon size={11} />{loading ? "Loading…" : "Reveal"}{/if}
      </RcButton>
    {/if}
  {/snippet}

  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if keys.length === 0}
    <p class="empty">No environment variables configured.</p>
  {:else}
    <dl class="variables">
      {#each keys as key (key)}
        <div class="row">
          <dt>{key}</dt>
          <dd>
            {#if visible && variables}
              <code>{variables[key] === "" ? "(empty)" : (variables[key] ?? "(removed)")}</code>
            {:else}
              <span class="masked" aria-label="Value hidden">••••••••</span>
            {/if}
          </dd>
        </div>
      {/each}
    </dl>
  {/if}
</RcPanel>

<style>
  .variables { display: flex; flex-direction: column; }
  .row { display: grid; grid-template-columns: minmax(8rem, 35%) minmax(0, 1fr); gap: 12px; padding: 8px 0; border-top: 1px solid var(--border-subtle); font-size: 11.5px; }
  dt { font-family: var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); overflow-wrap: anywhere; }
  dd { min-width: 0; }
  code { font-family: var(--font-mono, ui-monospace, monospace); color: var(--text-primary); white-space: pre-wrap; overflow-wrap: anywhere; }
  .masked { color: var(--text-tertiary); letter-spacing: 0.08em; }
  .empty, .error { font-size: 11.5px; color: var(--text-tertiary); }
  .error { color: var(--accent-red); margin-bottom: 8px; }
  @media (max-width: 600px) { .row { grid-template-columns: minmax(0, 1fr); gap: 4px; } }
</style>
