import { getContext, onMount, setContext } from "svelte";
import { product } from "$lib/config";

type Theme = "dark" | "light";

const shellContext = Symbol("dashboard-shell");
export function createShell() {
  const state = $state<{
    collapsed: boolean;
    mobileOpen: boolean;
    narrow: boolean;
    theme: Theme;
  }>({
    collapsed: false,
    mobileOpen: false,
    narrow: false,
    theme: "dark",
  });
  onMount(() => {
    const media = window.matchMedia("(max-width: 640px)");
    const sync = () => {
      state.narrow = media.matches;
      if (!media.matches) state.mobileOpen = false;
    };
    sync();
    state.theme =
      document.documentElement.dataset.theme === "light" ? "light" : "dark";
    media.addEventListener("change", sync);
    return () => media.removeEventListener("change", sync);
  });
  function toggle() {
    if (window.matchMedia("(max-width: 640px)").matches)
      state.mobileOpen = !state.mobileOpen;
    else state.collapsed = !state.collapsed;
  }
  function setTheme(theme: "dark" | "light") {
    state.theme = theme;
    document.documentElement.dataset.theme = theme;
    try {
      localStorage.setItem(product.themeStorageKey, theme);
    } catch {
      /* The current page still changes theme. */
    }
  }
  return setContext(shellContext, { state, toggle, setTheme });
}
export function getShell() {
  return getContext<ReturnType<typeof createShell>>(shellContext);
}
