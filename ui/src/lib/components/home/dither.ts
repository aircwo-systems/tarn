// Shared by the band and the buoys: the 8x8 Bayer thresholds the dots are
// drawn against, and a theme colour read as RGB.

export const BAYER8 = [
  0, 32, 8, 40, 2, 34, 10, 42, 48, 16, 56, 24, 50, 18, 58, 26, 12, 44, 4, 36, 14, 46, 6, 38, 60, 28,
  52, 20, 62, 30, 54, 22, 3, 35, 11, 43, 1, 33, 9, 41, 51, 19, 59, 27, 49, 17, 57, 25, 15, 47, 7,
  39, 13, 45, 5, 37, 63, 31, 55, 23, 61, 29, 53, 21,
].map((v) => (v + 0.5) / 64);

// colourOf reads a theme colour (the stage, the accent) as RGB.
export function colourOf(el: HTMLElement, token: string): [number, number, number] {
  const probe = document.createElement("span");
  probe.style.color = `var(${token})`;
  el.appendChild(probe);
  const m = getComputedStyle(probe)
    .color.match(/[\d.]+/g)
    ?.map(Number) ?? [0, 0, 0];
  probe.remove();
  return m[0] <= 1 && m[1] <= 1 && m[2] <= 1
    ? [m[0] * 255, m[1] * 255, m[2] * 255]
    : [m[0], m[1], m[2]];
}
