import type { TarnLight } from "./tarn-art";

// How Home's banner is drawn: which plate (following the theme, or pinned to
// day or night) and in what style. Kept in localStorage; Settings drafts a
// change and sets it on save, like its other settings.

export type BannerScene = "auto" | TarnLight;
export type BannerStyle = "halftone" | "chunky" | "painted" | "phosphor" | "off";

export interface BannerRender {
  /** dither: halftone in the plate's colours; ink: one-colour halftone; smooth: the painting as is */
  mode: "dither" | "ink" | "smooth";
  cell: number;
  levels: number;
}

export const BANNER_STYLES: {
  id: BannerStyle;
  label: string;
  detail: string;
  render?: BannerRender;
}[] = [
  {
    id: "halftone",
    label: "Halftone",
    detail: "Fine dots over the painting",
    render: { mode: "dither", cell: 2, levels: 6 },
  },
  {
    id: "chunky",
    label: "Chunky",
    detail: "Big pixels, few tones",
    render: { mode: "dither", cell: 4, levels: 3 },
  },
  {
    id: "painted",
    label: "Painted",
    detail: "The painting, untouched",
    render: { mode: "smooth", cell: 1, levels: 0 },
  },
  {
    id: "phosphor",
    label: "Phosphor",
    detail: "One-colour dots in the accent",
    render: { mode: "ink", cell: 2, levels: 4 },
  },
  { id: "off", label: "Off", detail: "No banner" },
];

export const BANNER_SCENES: { id: BannerScene; label: string }[] = [
  { id: "auto", label: "Theme" },
  { id: "day", label: "Day" },
  { id: "night", label: "Night" },
];

const KEY = "tarn-home-banner";

let scene = $state<BannerScene>("auto");
let style = $state<BannerStyle>("halftone");
// The theme's plate, from the class app.html and the theme toggle set.
let themeLight = $state<TarnLight>("night");

if (typeof window !== "undefined") {
  try {
    const saved = JSON.parse(localStorage.getItem(KEY) ?? "{}");
    if (BANNER_SCENES.some((s) => s.id === saved.scene)) scene = saved.scene;
    if (BANNER_STYLES.some((s) => s.id === saved.style)) style = saved.style;
  } catch {
    // A bad value keeps the defaults.
  }
  const sync = () => {
    themeLight = document.documentElement.classList.contains("light") ? "day" : "night";
  };
  sync();
  new MutationObserver(sync).observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["class"],
  });
}

function save() {
  try {
    localStorage.setItem(KEY, JSON.stringify({ scene, style }));
  } catch {
    // Storage full or blocked: the choice lasts this session.
  }
}

export function getBannerPrefs() {
  return {
    get scene() {
      return scene;
    },
    get style() {
      return style;
    },
    /** the plate to draw: the pinned one, or the theme's */
    get light(): TarnLight {
      return scene === "auto" ? themeLight : scene;
    },
    /** the theme's plate, whatever is pinned */
    get themeLight(): TarnLight {
      return themeLight;
    },
    get render(): BannerRender | undefined {
      return BANNER_STYLES.find((s) => s.id === style)?.render;
    },
  };
}

export function setBannerScene(next: BannerScene) {
  scene = next;
  save();
}

export function setBannerStyle(next: BannerStyle) {
  style = next;
  save();
}
