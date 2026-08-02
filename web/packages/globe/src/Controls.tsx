import { useFrame, useThree } from "@react-three/fiber";
import { useEffect, useRef } from "react";
import { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";

import { SEMI_MAJOR_M, SEMI_MINOR_M } from "./wgs84";

/**
 * Orbit the globe: drag to rotate, scroll to zoom.
 *
 * three.js's own OrbitControls rather than @react-three/drei. drei is a large
 * dependency for one class that three already ships, and every package here is
 * another licence to audit in a project that publishes its licence audit.
 *
 * Created inside the effect and held in a ref, not in a useMemo. Under
 * StrictMode a memoised instance is disposed by the first cleanup and then
 * reused by the second run, because useMemo does not re-run for a remount —
 * so the controls the scene ends up with are the ones that were already torn
 * down.
 */
export function Controls(): null {
  const camera = useThree((state) => state.camera);
  const element = useThree((state) => state.gl.domElement);
  const controls = useRef<OrbitControls | null>(null);

  useEffect(() => {
    const created = new OrbitControls(camera, element);

    created.enableDamping = true;
    created.dampingFactor = 0.08;

    // Zoom is bounded so the camera can neither pass through the surface nor
    // lose the globe entirely. The near bound is just outside the polar
    // radius, which is the closest approach any point on the ellipsoid allows.
    created.minDistance = (SEMI_MINOR_M / SEMI_MAJOR_M) * 1.02;
    created.maxDistance = 12;

    // Panning is off. On a globe it translates the orbit target off the
    // earth's centre, and rotation then sweeps the planet out of frame with no
    // obvious way back.
    created.enablePan = false;

    controls.current = created;

    return () => {
      created.dispose();
      controls.current = null;
    };
  }, [camera, element]);

  // Damping needs a per-frame update. Without it a drag stops dead on release
  // rather than easing, which reads as the control having lost the pointer.
  useFrame(() => {
    controls.current?.update();
  });

  return null;
}
