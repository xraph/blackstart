/**
 * A layer the build cannot draw.
 *
 * `off` means the reader turned it off. That is a different fact from "this
 * build has no such data", and collapsing them tells someone a layer is
 * available when toggling it would do nothing.
 */

import { describe, expect, it } from "vitest";
import { activeLayerCount, Layer, LayerStack } from "./scene";

const layer = (over: Partial<Layer> = {}): unknown => ({
  id: "clouds",
  label: "Clouds",
  state: "on",
  opacity: 1,
  source: "HRRR conus 18Z f04",
  ...over,
});

describe("Layer", () => {
  it("accepts a layer this build cannot draw", () => {
    expect(Layer.parse(layer({ state: "unavailable", opacity: 0 }))).toMatchObject({
      state: "unavailable",
    });
  });

  it("refuses an unavailable layer that claims to be drawing something", () => {
    // Opacity on a layer with no data behind it is a contradiction the panel
    // would render as a working slider.
    expect(() => Layer.parse(layer({ state: "unavailable", opacity: 0.5 }))).toThrow();
  });
});

describe("activeLayerCount", () => {
  it("does not count a layer the build cannot draw", () => {
    const stack = LayerStack.parse([
      layer({ id: "network", label: "Network" }),
      layer({ id: "clouds", state: "unavailable", opacity: 0 }),
    ]);

    expect(activeLayerCount(stack)).toBe(1);
  });

  it("still counts a partial layer, which is drawing something", () => {
    const stack = LayerStack.parse([
      layer({ id: "wind", label: "Wind field", state: "partial", opacity: 0.4 }),
    ]);

    expect(activeLayerCount(stack)).toBe(1);
  });
});
