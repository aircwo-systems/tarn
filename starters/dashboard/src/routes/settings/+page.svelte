<script lang="ts">
  import { resolve } from "$app/paths";
  import PageHeader from "$lib/components/PageHeader.svelte";
  import SettingsLayout, {
    type SettingsSection,
  } from "$lib/components/SettingsLayout.svelte";
  import Button from "$lib/components/Button.svelte";
  import { getShell } from "$lib/shell.svelte";
  import { product } from "$lib/config";
  const shell = getShell();
  const sections: SettingsSection[] = [
    { id: "appearance", label: "Appearance" },
    { id: "navigation", label: "Navigation" },
    { id: "examples", label: "Layout examples" },
    { id: "keyboard", label: "Keyboard" },
    { id: "about", label: "About" },
  ];
</script>

<svelte:head
  ><title>Settings | {product.name}</title><meta
    name="description"
    content="Browser preferences and reusable dashboard settings patterns."
  /></svelte:head
>
<PageHeader
  title="Settings"
  description="Preferences for this dashboard and examples for the next one."
  dataLabel="Browser preferences"
/>

<SettingsLayout {sections}>
  {#snippet section(item: SettingsSection)}
    {#if item.id === "appearance"}
      <header>
        <h2>Appearance</h2>
        <p>Choose the same quiet surfaces in either theme.</p>
      </header>
      <div class="setting">
        <div class="setting-label">
          <span>Theme</span><small
            >Saved in this browser. Applies across every section.</small
          >
        </div>
        <div class="controls" role="group" aria-label="Theme">
          <Button
            aria-pressed={shell.state.theme === "dark"}
            onclick={() => shell.setTheme("dark")}>Dark</Button
          ><Button
            aria-pressed={shell.state.theme === "light"}
            onclick={() => shell.setTheme("light")}>Light</Button
          >
        </div>
      </div>
      <div class="setting">
        <div class="setting-label">
          <span>Motion</span><small
            >Sidebar and submenu transitions follow your device’s reduced motion
            preference.</small
          >
        </div>
        <span class="setting-value">System preference</span>
      </div>
      <div class="preview">
        <span class="preview-outer"
          ><span class="preview-inner"
            ><span></span><span></span><span></span></span
          ></span
        >
        <p>
          The outer canvas frames a rounded inset workspace. Thin separators and
          neutral surfaces carry the structure.
        </p>
      </div>
    {:else if item.id === "navigation"}
      <header>
        <h2>Navigation</h2>
        <p>
          A compact rail when you need space, a wider column when you need
          context.
        </p>
      </header>
      <div class="setting">
        <div class="setting-label">
          <span>Desktop sidebar</span><small
            >Both views keep every section accessible.</small
          >
        </div>
        <div class="controls" role="group" aria-label="Desktop sidebar">
          <Button
            aria-pressed={shell.state.collapsed}
            onclick={() => (shell.state.collapsed = true)}>Compact</Button
          ><Button
            aria-pressed={!shell.state.collapsed}
            onclick={() => (shell.state.collapsed = false)}>Expanded</Button
          >
        </div>
      </div>
      <div class="setting">
        <div class="setting-label">
          <span>Section information</span><small
            >Drag the sidebar above 296px to reveal summaries and supporting
            information. Narrower widths retain simple labels.</small
          >
        </div>
        <span class="setting-value">Adapts to width</span>
      </div>
      <div class="setting">
        <div class="setting-label">
          <span>Width</span><small
            >The last expanded width is saved in this browser. Double click the
            edge to restore 320px.</small
          >
        </div>
        <span class="setting-value">180–520px</span>
      </div>
      <p class="note">
        On phones, the header control opens navigation inline. The compact rail
        is a desktop layout.
      </p>
    {:else if item.id === "examples"}
      <header>
        <h2>Layout examples</h2>
        <p>Starting points for a future product’s screens.</p>
      </header>
      <a class="example-link" href={resolve("/section-template")}
        ><div>
          <span>Resource browser</span><small
            >A working resource list and detail pane. Collapse the list into
            horizontal pills while keeping the selected worker.</small
          >
        </div>
        <span aria-hidden="true">↗</span></a
      >
      <a
        class="example-link"
        href="{resolve('/section-template')}?example=skeleton"
        ><div>
          <span>Section skeleton</span><small
            >A static skeleton with a toolbar, summary, main content and
            details. A recommendation to adapt, with no final fields or actions.</small
          >
        </div>
        <span aria-hidden="true">↗</span></a
      >
      <a class="example-link" href={resolve("/foundation")}
        ><div>
          <span>UI foundation</span><small
            >Working controls, status treatments, typography and loading, empty
            and error states.</small
          >
        </div>
        <span aria-hidden="true">↗</span></a
      >
      <p class="note">
        Keep the shell and its interaction rules. Choose content, fields and
        actions around the job your next dashboard needs to do.
      </p>
    {:else if item.id === "keyboard"}
      <header>
        <h2>Keyboard</h2>
        <p>Every pointer gesture has a keyboard alternative.</p>
      </header>
      <dl class="shortcuts">
        <div>
          <dt>Toggle sidebar</dt>
          <dd><kbd>⌘ / Ctrl</kbd> <kbd>B</kbd></dd>
        </div>
        <div>
          <dt>Resize focused sidebar edge</dt>
          <dd><kbd>←</kbd> <kbd>→</kbd></dd>
        </div>
        <div>
          <dt>Reset focused sidebar edge</dt>
          <dd><kbd>Enter</kbd> or <kbd>Space</kbd></dd>
        </div>
        <div>
          <dt>Move focused canvas node</dt>
          <dd><kbd>Alt</kbd> + arrow key</dd>
        </div>
        <div>
          <dt>Close mobile navigation</dt>
          <dd><kbd>Esc</kbd></dd>
        </div>
      </dl>
      <p class="note">
        The sidebar shortcut leaves text fields alone. Nonessential animation
        stops when reduced motion is enabled.
      </p>
    {:else if item.id === "about"}
      <header>
        <h2>About this starter</h2>
        <p>A Svelte dashboard foundation drawn from Tarn’s interface.</p>
      </header>
      <div class="setting">
        <div class="setting-label">
          <span>Workspace</span><small>{product.subtitle}</small>
        </div>
        <span class="setting-value">{product.environment}</span>
      </div>
      <div class="setting">
        <div class="setting-label">
          <span>Interface</span><small
            >Svelte 5, locally bundled Geist fonts and a static SvelteKit build.</small
          >
        </div>
        <span class="setting-value">Reusable starter</span>
      </div>
      <div class="setting">
        <div class="setting-label">
          <span>Example data</span><small
            >Worker actions update sample data in this session. Reloading
            restores the fixtures.</small
          >
        </div>
        <span class="setting-value">Sample only</span>
      </div>
      <p class="note">
        These browser preferences work now. Add product settings here when your
        application has a real data layer and a persistence policy.
      </p>
    {/if}
  {/snippet}
</SettingsLayout>

<style>
  header {
    margin-bottom: 12px;
  }
  header p {
    font-size: 11.5px;
    color: var(--ink-tertiary);
    margin-top: 4px;
  }
  .setting {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 12px 0;
    border-top: 1px solid var(--line);
  }
  .setting-label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .setting-label > span,
  .example-link div > span {
    font-size: 12.5px;
    font-weight: 500;
  }
  small {
    display: block;
    color: var(--ink-tertiary);
    font-size: 11px;
    line-height: 1.6;
  }
  .controls {
    display: flex;
    gap: 6px;
    flex-shrink: 0;
  }
  .controls :global(button[aria-pressed="true"]) {
    border-color: var(--line-strong);
    background: var(--nav-active);
    color: var(--ink);
  }
  .setting-value {
    flex-shrink: 0;
    font: 10.5px var(--font-data);
    color: var(--ink-secondary);
  }
  .note {
    color: var(--ink-secondary);
    font-size: 11.5px;
    line-height: 1.7;
    border-top: 1px solid var(--line);
    padding-top: 12px;
    margin-top: 4px;
  }
  .preview {
    display: flex;
    align-items: center;
    gap: 16px;
    border-top: 1px solid var(--line);
    padding-top: 16px;
    margin-top: 4px;
  }
  .preview p {
    font-size: 11.5px;
    color: var(--ink-secondary);
    line-height: 1.7;
  }
  .preview-outer {
    display: block;
    width: 120px;
    height: 76px;
    padding: 6px 6px 6px 28px;
    border-radius: 8px;
    background: var(--canvas);
    border: 1px solid var(--line);
    flex-shrink: 0;
  }
  .preview-inner {
    display: flex;
    flex-direction: column;
    gap: 8px;
    height: 100%;
    padding: 12px 10px;
    border-radius: 5px;
    background: var(--surface);
    border: 1px solid var(--line);
  }
  .preview-inner span {
    height: 3px;
    background: var(--line-strong);
    border-radius: 2px;
  }
  .preview-inner span:last-child {
    width: 55%;
  }
  .example-link {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 24px;
    border-top: 1px solid var(--line);
    padding: 14px 0;
    text-decoration: none;
  }
  .example-link small {
    margin-top: 5px;
  }
  .example-link:hover div > span {
    color: var(--accent);
  }
  .example-link > span {
    color: var(--ink-tertiary);
  }
  .shortcuts {
    margin: 0;
  }
  .shortcuts div {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    border-top: 1px solid var(--line);
    padding: 12px 0;
    font-size: 11.5px;
  }
  dd {
    margin: 0;
    color: var(--ink-secondary);
  }
  kbd {
    display: inline-block;
    padding: 2px 5px;
    border: 1px solid var(--line-strong);
    border-radius: 4px;
    font: 10.5px var(--font-data);
  }
  @media (max-width: 640px) {
    .setting {
      align-items: flex-start;
      flex-direction: column;
      gap: 10px;
    }
    .controls {
      flex-wrap: wrap;
    }
    .shortcuts div {
      flex-wrap: wrap;
    }
    .preview {
      align-items: flex-start;
    }
  }
</style>
