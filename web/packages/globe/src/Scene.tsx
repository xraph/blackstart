import { Canvas } from "@react-three/fiber";

import { Controls } from "./Controls";
import { Extent, type ExtentProps } from "./Extent";
import { Globe } from "./Globe";
import { Network, type Selection } from "./Network";
import { Routes, type RoutePolyline, type Rgba } from "./Routes";
import type { ViewerModel } from "./model";

interface SceneProps {
  model: ViewerModel;

  /**
   * Surveyed conductor routes, drawn over the schematic branches.
   *
   * Optional because the scene is useful without them: a model exported with
   * no geometry still has a topology worth looking at, and a viewer with no
   * platform to ask still draws it.
   */
  routes?: readonly RoutePolyline[] | undefined;

  /** The colour a span is drawn in. Omitted, routes draw in one colour. */
  colorOf?: ((spanId: string) => Rgba) | undefined;

  /**
   * The ground a cascade covered, drawn beneath the routes.
   *
   * Optional for the same reason `routes` is: a viewer with no run to show an
   * extent for still draws a scene.
   */
  extent?: ExtentProps["area"] | undefined;

  onSelect?: ((selection: Selection | null) => void) | undefined;
  onSelectRoute?: ((route: RoutePolyline) => void) | undefined;
}

/**
 * The whole scene: an ellipsoid, the network on it, and a camera.
 *
 * The near and far planes are set for a unit-radius earth. Near at 0.001 is
 * about six kilometres, which is close enough to fly to a substation; far at
 * 100 keeps the whole globe in view from any distance a user will pull back
 * to. Leaving near at the default 0.1 would clip the surface away as soon as
 * the camera approached it.
 */
export function Scene({
  model,
  routes,
  colorOf,
  extent,
  onSelect,
  onSelectRoute,
}: SceneProps): React.JSX.Element {
  return (
    <Canvas
      camera={{ position: [-1.4, -2.2, 1.4], near: 0.001, far: 100, fov: 40 }}
      // Clicking away from any object clears the selection. Without this the
      // inspector keeps showing whatever was last clicked, which reads as the
      // click having failed.
      onPointerMissed={() => onSelect?.(null)}
    >
      <color attach="background" args={["#05070c"]} />
      <ambientLight intensity={0.9} />
      <directionalLight position={[5, -3, 4]} intensity={1.6} />

      <Controls />
      <Globe />
      <Network model={model} onSelect={onSelect} />
      {/* Drawn before the routes so a route is never sorted beneath the
       * translucent ground it sits on — conductors stay legible on top. */}
      {/* `!= null` on purpose: catches both `undefined` (no run selected) and
       * `null` (a wire shape the generated client's types say cannot happen,
       * but a client should not trust a shape it does not control — the
       * platform's own extentResponse.area used to serialise `null` for an
       * empty extent before that was fixed server-side; this guard survives
       * it happening again, here or on some other field shaped like it). */}
      {extent != null && extent.length > 0 && <Extent area={extent} />}
      {routes !== undefined && routes.length > 0 && (
        <Routes routes={routes} colorOf={colorOf} onSelect={onSelectRoute} />
      )}
    </Canvas>
  );
}
