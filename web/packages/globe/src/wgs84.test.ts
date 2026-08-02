import { describe, expect, it } from "vitest";

import { SEMI_MAJOR_M, SEMI_MINOR_M, toEcef, toEcefArray, toGeodetic } from "./wgs84";

describe("toEcef", () => {
  it("puts the origin of the coordinate system where the definition says", () => {
    // Null Island is where +X pierces the ellipsoid, at exactly the semi-major
    // axis. Any error in the radius shows up here undisguised by latitude.
    const { x, y, z } = toEcef({ lon: 0, lat: 0 });

    expect(x).toBeCloseTo(SEMI_MAJOR_M, 6);
    expect(y).toBeCloseTo(0, 6);
    expect(z).toBeCloseTo(0, 6);
  });

  it("places the pole at the semi-minor axis, not the semi-major one", () => {
    // The check that separates an ellipsoid from a sphere. Dropping the
    // (1 - e²) factor on the vertical axis — the single most common error in
    // this conversion — puts the pole 21 km too high and still looks like a
    // globe.
    const { x, y, z } = toEcef({ lon: 0, lat: 90 });

    expect(Math.hypot(x, y)).toBeCloseTo(0, 6);
    expect(z).toBeCloseTo(SEMI_MINOR_M, 6);
    expect(SEMI_MAJOR_M - z).toBeGreaterThan(21_000);
  });

  it("takes longitude east and latitude north", () => {
    // 90° east is +Y. A sign error here mirrors the network about the prime
    // meridian, which on a featureless globe is invisible.
    const east = toEcef({ lon: 90, lat: 0 });
    expect(east.y).toBeCloseTo(SEMI_MAJOR_M, 6);
    expect(east.x).toBeCloseTo(0, 6);

    // And the northern hemisphere is +Z.
    expect(toEcef({ lon: 0, lat: 45 }).z).toBeGreaterThan(0);
    expect(toEcef({ lon: 0, lat: -45 }).z).toBeLessThan(0);
  });

  it("raises a point by its elevation along the surface normal", () => {
    const surface = toEcef({ lon: -97.7856, lat: 32.2983 });
    const raised = toEcef({ lon: -97.7856, lat: 32.2983, elevation: 1_000 });

    const lifted = Math.hypot(raised.x - surface.x, raised.y - surface.y, raised.z - surface.z);

    expect(lifted).toBeCloseTo(1_000, 6);
  });
});

describe("toGeodetic", () => {
  it("inverts toEcef to well under a millimetre", () => {
    // Points chosen to exercise both hemispheres in both axes and a range of
    // latitudes, because the ellipsoid's error is latitude-dependent and a
    // test near the equator would pass on a spherical implementation.
    const places: Array<{ lon: number; lat: number; elevation: number }> = [
      { lon: 0, lat: 0, elevation: 0 },
      { lon: -97.7856, lat: 32.2983, elevation: 240 },
      { lon: 139.6917, lat: 35.6895, elevation: 40 },
      { lon: -58.3816, lat: -34.6037, elevation: 25 },
      { lon: 18.4241, lat: -33.9249, elevation: 0 },
      { lon: 24.9384, lat: 60.1699, elevation: 15 },
      { lon: 179.9, lat: 71.2, elevation: 5 },
    ];

    for (const place of places) {
      const back = toGeodetic(toEcef(place));

      expect(back.lon).toBeCloseTo(place.lon, 9);
      expect(back.lat).toBeCloseTo(place.lat, 9);
      expect(back.elevation!).toBeCloseTo(place.elevation, 6);
    }
  });

  it("reports a pole rather than dividing by zero", () => {
    // Longitude is undefined at the poles and the closed form divides by zero
    // there. NaN would propagate into the scene as an object that silently
    // fails to draw rather than as an error.
    const north = toGeodetic({ x: 0, y: 0, z: SEMI_MINOR_M });

    expect(north.lat).toBeCloseTo(90, 9);
    expect(Number.isNaN(north.lon)).toBe(false);
    expect(north.elevation!).toBeCloseTo(0, 6);
  });
});

describe("toEcefArray", () => {
  it("scales to earth-radius-one so the depth buffer survives", () => {
    // At metre-scale coordinates the far plane is 10^7 and WebGL's depth
    // buffer collapses into z-fighting across the whole scene.
    const packed = toEcefArray([{ lon: 0, lat: 0 }]);

    expect(packed).toHaveLength(3);
    expect(packed[0]).toBeCloseTo(1, 6);
  });

  it("packs points in order, three floats each", () => {
    const packed = toEcefArray([
      { lon: 0, lat: 0 },
      { lon: 90, lat: 0 },
      { lon: 0, lat: 90 },
    ]);

    expect(packed).toHaveLength(9);
    expect(packed[0]).toBeCloseTo(1, 5); // first point on +X
    expect(packed[4]).toBeCloseTo(1, 5); // second point on +Y
    expect(packed[8]).toBeCloseTo(SEMI_MINOR_M / SEMI_MAJOR_M, 5); // third on +Z
  });
});
