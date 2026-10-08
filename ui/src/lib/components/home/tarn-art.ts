import day from "./art/tarn-day.webp";
import night from "./art/tarn-night.webp";
import buoyDayLit from "./art/buoy-day-lit.webp";
import buoyDayResting from "./art/buoy-day-resting.webp";
import buoyDayTrouble from "./art/buoy-day-trouble.webp";
import buoyNightLit from "./art/buoy-night-lit.webp";
import buoyNightResting from "./art/buoy-night-resting.webp";
import buoyNightTrouble from "./art/buoy-night-trouble.webp";

// The tarn painting behind Home: a mountain lake in a glacial cirque, a
// stone hut on the left shore, open water below. One plate per theme, the
// night painted from the day, so the overlays (the hut's window, the
// waterline) sit in the same place in each.

export type TarnLight = "day" | "night";

// Where the plate's landmarks sit, as fractions of the picture: the hut's
// window (its centre and the size of its opening) is the lamp; the
// waterline is where the far shore meets the lake.
export const PIC = {
  w: 1672,
  h: 941,
  lampX: 0.1794,
  lampY: 0.4787,
  lampW: 0.0084,
  lampH: 0.018,
  waterline: 0.556,
};

export const TARN_ART: Record<TarnLight, string> = { day, night };

// How far each plate is muted toward the stage: the night more, so the
// stars and the moon's path stay calm behind the page.
export const TARN_MUTE: Record<TarnLight, number> = { day: 0.05, night: 0.15 };

// A function's buoy: banded, with a lamp in a cage on top, unlit while it
// rests, amber once it has run lately, red when it is in trouble.
export type BuoyState = "lit" | "resting" | "trouble";
export const BUOY: Record<TarnLight, Record<BuoyState, string>> = {
  day: { lit: buoyDayLit, resting: buoyDayResting, trouble: buoyDayTrouble },
  night: { lit: buoyNightLit, resting: buoyNightResting, trouble: buoyNightTrouble },
};
export const BUOY_PIC = { w: 108, h: 150, waterline: 0.9 };
