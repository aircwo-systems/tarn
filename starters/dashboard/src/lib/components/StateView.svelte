<script lang="ts" module>
  export type ViewState =
    | { kind: "loading"; message: string }
    | { kind: "empty"; title: string; description: string }
    | {
        kind: "error";
        title: string;
        description: string;
        onretry: () => void;
      };
</script>

<script lang="ts">
  import Button from "./Button.svelte";
  let { state }: { state: ViewState } = $props();
</script>

<div
  class="state"
  role={state.kind === "error" ? "alert" : "status"}
  aria-busy={state.kind === "loading"}
>
  {#if state.kind === "loading"}
    <div class="skeleton" aria-hidden="true">
      <span></span><span></span><span></span>
    </div>
    <p>{state.message}</p>
  {:else}
    <h3>{state.title}</h3>
    <p>{state.description}</p>
    {#if state.kind === "error"}<Button onclick={state.onretry}
        >Try again</Button
      >{/if}
  {/if}
</div>

<style>
  .state {
    min-height: 180px;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    gap: var(--space-3);
    border-block: 1px solid var(--line);
    padding: var(--space-6);
  }
  p {
    color: var(--ink-secondary);
    font-size: 13px;
    max-width: 40ch;
  }
  .skeleton {
    display: grid;
    gap: var(--space-2);
    width: min(280px, 100%);
  }
  .skeleton span {
    height: 10px;
    background: var(--surface-hover);
  }
  .skeleton span:last-child {
    width: 65%;
  }
</style>
