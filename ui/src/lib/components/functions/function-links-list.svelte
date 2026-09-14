<script lang="ts" module>
  export interface LinkItem {
    key: string;
    label: string;
    detail: string;
    /** short type tag shown before the label */
    tag: string;
    href?: string;
    tone?: "green" | "amber" | "red" | "muted";
    trailing?: string;
  }
</script>

<script lang="ts">
  import { ArrowUpRightIcon } from "phosphor-svelte";

  let { items, empty }: { items: LinkItem[]; empty: string } = $props();
</script>

{#if items.length === 0}
  <p class="empty">{empty}</p>
{:else}
  <ul class="links">
    {#each items as item (item.key)}
      <li>
        <svelte:element
          this={item.href ? "a" : "div"}
          href={item.href ? `#${item.href}` : undefined}
          class="link"
          class:clickable={!!item.href}
          data-tone={item.tone}
        >
          <span class="tag">{item.tag}</span>
          <span class="body">
            <span class="label" title={item.label}>{item.label}</span>
            <span class="detail">{item.detail}</span>
          </span>
          {#if item.trailing}<span class="trailing">{item.trailing}</span>{/if}
          {#if item.href}<ArrowUpRightIcon size={11} class="go" />{/if}
        </svelte:element>
      </li>
    {/each}
  </ul>
{/if}

<style>
  .empty { font-size: 11.5px; color: var(--text-tertiary); padding: 4px 0; }
  .links { display: flex; flex-direction: column; gap: 2px; }
  .link {
    --tone: var(--text-tertiary);
    position: relative; display: flex; align-items: center; gap: 10px; padding: 7px 10px; border-radius: 8px;
    border: 1px solid transparent; text-decoration: none;
    transition: background 120ms ease, border-color 120ms ease, padding-left 220ms var(--ease-snappy);
  }
  .link.clickable:hover { background: var(--bg-element-hover); padding-left: 14px; }
  .link[data-tone="green"] { --tone: var(--accent-green); }
  .link[data-tone="amber"] { --tone: var(--accent-amber); }
  .link[data-tone="red"] { --tone: var(--accent-red); background: color-mix(in srgb, var(--accent-red) 5%, transparent); }
  .link[data-tone="muted"] { opacity: 0.6; }
  .tag {
    flex-shrink: 0; min-width: 52px; font-size: 10px; letter-spacing: 0.04em; text-transform: uppercase;
    color: var(--tone);
  }
  .body { display: flex; flex-direction: column; min-width: 0; flex: 1; }
  .label { font: 12px var(--font-mono, ui-monospace, monospace); color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .detail { font-size: 10.5px; color: var(--text-tertiary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .trailing { font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); flex-shrink: 0; }
  .link :global(.go) { color: var(--text-tertiary); opacity: 0; flex-shrink: 0; transition: opacity 120ms ease; }
  .link:hover :global(.go) { opacity: 1; }
  @media (prefers-reduced-motion: reduce) { .link { transition: none; } }
</style>
