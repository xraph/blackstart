/**
 * WGS84 geodetic conversions.
 *
 * Pure functions over numbers — no three.js, no React. The scene draws
 * whatever these return, so if a substation lands in the wrong hemisphere the
 * cause is here and a test can say so without a renderer.
 *
 * The output frame is ECEF: metres, origin at the centre of the earth, +Z
 * through the north pole, +X through the intersection of the equator and the
 * prime meridian. That is what the geometry is drawn in, so a line between two
 * points is a straight segment through the earth rather than something that
 * needs projecting.
 */

/** Semi-major axis of the WGS84 ellipsoid, in metres. */
export const SEMI_MAJOR_M = 6_378_137.0

/** Flattening of the WGS84 ellipsoid. */
export const FLATTENING = 1 / 298.257223563

/** Semi-minor axis, derived. */
export const SEMI_MINOR_M = SEMI_MAJOR_M * (1 - FLATTENING)

/** First eccentricity squared, derived. */
const E2 = FLATTENING * (2 - FLATTENING)

const DEG = Math.PI / 180

/**
 * A position on the ellipsoid.
 *
 * Longitude first, matching the well-known text the platform stores and the
 * order the API returns. Named fields rather than a tuple precisely because
 * the two are easy to swap and a tuple gives the compiler nothing to catch it
 * with.
 */
export interface Geodetic {
  lon: number
  lat: number
  /** Height above the ellipsoid in metres. */
  elevation?: number
}

/** A position in earth-centred, earth-fixed metres. */
export interface Ecef {
  x: number
  y: number
  z: number
}

/**
 * Converts a geodetic position to ECEF metres.
 *
 * The ellipsoid, not a sphere. A sphere is off by up to 21 km at the poles —
 * larger than any substation, and enough that a network drawn on a sphere
 * would visibly float above or sink into a globe drawn as an ellipsoid. Since
 * the globe and the network are drawn in the same scene, the two have to agree
 * on what shape the earth is.
 */
export function toEcef({ lon, lat, elevation = 0 }: Geodetic): Ecef {
  const sinLat = Math.sin(lat * DEG)
  const cosLat = Math.cos(lat * DEG)
  const sinLon = Math.sin(lon * DEG)
  const cosLon = Math.cos(lon * DEG)

  // Radius of curvature in the prime vertical. This is what makes it an
  // ellipsoid rather than a sphere: it varies with latitude.
  const n = SEMI_MAJOR_M / Math.sqrt(1 - E2 * sinLat * sinLat)

  return {
    x: (n + elevation) * cosLat * cosLon,
    y: (n + elevation) * cosLat * sinLon,
    // The (1 - e²) factor applies only to the vertical axis, and leaving it
    // out is the single most common error in this conversion: everything still
    // looks plausible, and every latitude away from the equator is wrong by up
    // to 21 km.
    z: (n * (1 - E2) + elevation) * sinLat,
  }
}

/**
 * Writes a run of geodetic positions into a flat Float32Array of ECEF metres.
 *
 * Flat and typed because this is what the geometry attributes want: an array
 * of objects would be converted on upload anyway, and a 2000-bus network with
 * per-span line geometry is hundreds of thousands of vertices. The conversion
 * happens once here rather than once per frame.
 *
 * The array is scaled to earth-radius-equals-one. WebGL depth precision is
 * finite and the near plane has to sit close to the camera for a globe you can
 * fly to the surface of; at metre-scale coordinates the far plane is 10^7 and
 * the depth buffer collapses into z-fighting across the whole scene.
 */
export function toEcefArray(points: readonly Geodetic[]): Float32Array {
  const out = new Float32Array(points.length * 3)

  for (let index = 0; index < points.length; index++) {
    const { x, y, z } = toEcef(points[index]!)

    out[index * 3] = x / SEMI_MAJOR_M
    out[index * 3 + 1] = y / SEMI_MAJOR_M
    out[index * 3 + 2] = z / SEMI_MAJOR_M
  }

  return out
}

/**
 * Converts ECEF metres back to a geodetic position.
 *
 * Bowring's method, which converges to well under a millimetre in one
 * iteration for any point near the surface. Present so a click on the globe
 * can be reported as a coordinate — a viewer that can only go one way can show
 * the network but cannot answer "what is here".
 */
export function toGeodetic({ x, y, z }: Ecef): Geodetic {
  const p = Math.hypot(x, y)

  // The poles, where longitude is undefined and the closed form divides by
  // zero. Reported as longitude zero rather than NaN, which would propagate
  // into the scene as an invisible object rather than an error.
  if (p < 1e-9) {
    return { lon: 0, lat: z >= 0 ? 90 : -90, elevation: Math.abs(z) - SEMI_MINOR_M }
  }

  const ep2 = (SEMI_MAJOR_M * SEMI_MAJOR_M - SEMI_MINOR_M * SEMI_MINOR_M) / (SEMI_MINOR_M * SEMI_MINOR_M)
  const theta = Math.atan2(z * SEMI_MAJOR_M, p * SEMI_MINOR_M)
  const sinTheta = Math.sin(theta)
  const cosTheta = Math.cos(theta)

  const lat = Math.atan2(
    z + ep2 * SEMI_MINOR_M * sinTheta * sinTheta * sinTheta,
    p - E2 * SEMI_MAJOR_M * cosTheta * cosTheta * cosTheta,
  )

  const sinLat = Math.sin(lat)
  const n = SEMI_MAJOR_M / Math.sqrt(1 - E2 * sinLat * sinLat)

  return {
    lon: Math.atan2(y, x) / DEG,
    lat: lat / DEG,
    elevation: p / Math.cos(lat) - n,
  }
}
