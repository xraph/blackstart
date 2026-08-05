/**
 * The layer stack over the globe.
 *
 * Every layer carries the version of the data it is drawing. That is not
 * bookkeeping — it is what makes a screenshot self-describing, so a figure in
 * a review can be traced without asking who took it and when.
 */

import { z } from "zod";

/**
 * A layer's visibility.
 *
 * More than a boolean because several layers are genuinely partial: a wind
 * field can be subsampled and a risk surface can be shown for the selected
 * element only. Rendering those as "on" overstates what is drawn.
 *
 * `unavailable` is a different fact from `off` and the distinction is the
 * point. `off` means the reader turned it off and can turn it back on;
 * `unavailable` means this build has no such data to draw, so the row shows
 * the intended shape of the system without offering a control that would do
 * nothing.
 */
export const LAYER_STATES = ["on", "partial", "off", "unavailable"] as const;
export type LayerState = (typeof LAYER_STATES)[number];

export const Layer = z
  .object({
    id: z.string().min(1),
    label: z.string().min(1),
    state: z.enum(LAYER_STATES),
    opacity: z.number().min(0).max(1),
    /**
     * The dataset version this layer is drawing, shown inline on the row.
     * Required — a layer whose provenance is unknown should not be drawable.
     */
    source: z.string().min(1),
  })
  // Opacity on a layer with nothing behind it is a contradiction, and the
  // panel would render it as a working slider.
  .refine((l) => l.state !== "unavailable" || l.opacity === 0, {
    message: "an unavailable layer cannot have opacity",
    path: ["opacity"],
  });
export type Layer = z.infer<typeof Layer>;

export const LayerStack = z.array(Layer).min(1);
export type LayerStack = z.infer<typeof LayerStack>;

/** How many layers are actually contributing pixels, for the panel header. */
export function activeLayerCount(stack: LayerStack): number {
  return stack.filter((l) => l.state !== "off" && l.opacity > 0).length;
}

/**
 * Camera state, as the coordinate readout prints it.
 *
 * Sun elevation is part of camera state rather than a theme concern: the scene
 * renders the sky at the scenario's valid time, so whether the terrain is lit
 * follows the forecast and not the operator's colour preference.
 */
export const CameraReadout = z.object({
  lon: z.number().min(-180).max(180),
  lat: z.number().min(-90).max(90),
  /** Eye altitude above the ellipsoid, kilometres. */
  altitudeKm: z.number().positive(),
  /** Metres per pixel at the centre of the view, for the scale bar. */
  metresPerPixel: z.number().positive(),
  /** Degrees above the horizon; negative is night. */
  sunElevation: z.number().min(-90).max(90),
});
export type CameraReadout = z.infer<typeof CameraReadout>;

/** `31.34° N  99.41° W`, the way the readout prints a position. */
export function formatPosition(lon: number, lat: number): string {
  const ns = lat >= 0 ? "N" : "S";
  const ew = lon >= 0 ? "E" : "W";
  return `${Math.abs(lat).toFixed(2)}° ${ns}  ${Math.abs(lon).toFixed(2)}° ${ew}`;
}
