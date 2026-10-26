type Viewport = {
  iphoneStandalone: boolean;
  width: number;
  height: number;
  screenWidth: number;
  screenHeight: number;
  top: number;
  bottom: number;
};

// Some installed iPhone WebKit windows lose the status-bar extent from *all*
// viewport units. Use screen geometry only when the loss fits the safe areas.
// Scale by the layout width to respect Display Zoom and page zoom, and never
// stretch an iPad window, ordinary browser tab, or a keyboard-sized viewport.
export function appViewportHeight(view: Viewport) {
  const { width, height, screenWidth, screenHeight, top, bottom } = view;
  if (
    !view.iphoneStandalone ||
    ![width, height, screenWidth, screenHeight].every(
      (value) => Number.isFinite(value) && value > 0,
    )
  )
    return height;
  const portrait = width < height;
  const short = Math.min(screenWidth, screenHeight);
  const long = Math.max(screenWidth, screenHeight);
  const scale = width / (portrait ? short : long);
  if (scale < 0.75 || scale > 1.25) return height;
  const full = Math.round(width * (portrait ? long / short : short / long));
  const missing = full - height;
  return missing > 1 && missing <= Math.max(0, top) + Math.max(0, bottom) + 2 ? full : height;
}
