import { useMemo } from "react";
import * as THREE from "three";

import { SEMI_MAJOR_M, SEMI_MINOR_M } from "./wgs84";

/**
 * A shaded WGS84 ellipsoid.
 *
 * Deliberately plain: no atmosphere, no clouds, no terrain tiles, no
 * post-processing. The subject is the network, and every one of those would
 * obscure it while adding a dependency and a licensing question. The ellipsoid
 * is here to give the network somewhere to sit and to make it obvious when
 * something lands in the wrong hemisphere.
 *
 * Scaled to earth-radius-one to match `toEcefArray`. A sphere squashed on the
 * vertical axis rather than a sphere, so it agrees with the geodetic
 * conversion about what shape the earth is — a network computed on an
 * ellipsoid and drawn on a sphere floats above the surface at the poles by
 * 21 km, which at this scale is a visible gap.
 */
export function Globe(): React.JSX.Element {
  const geometry = useMemo(() => {
    const sphere = new THREE.SphereGeometry(1, 96, 64);

    // Squashed, not re-derived. A SphereGeometry scaled on one axis is exactly
    // an ellipsoid of revolution, which is what WGS84 is.
    sphere.scale(1, 1, SEMI_MINOR_M / SEMI_MAJOR_M);

    // three.js builds spheres around +Y; ECEF puts the pole on +Z. Rotating
    // the geometry once here is cheaper than rotating every position in the
    // network, and keeps the conversion functions free of renderer
    // conventions.
    sphere.rotateX(Math.PI / 2);

    return sphere;
  }, []);

  return (
    <mesh geometry={geometry}>
      <meshStandardMaterial color="#1c2f4a" roughness={0.9} metalness={0} />
    </mesh>
  );
}
