import { Canvas } from "@react-three/fiber";

import { Controls } from "./Controls";
import { Globe } from "./Globe";
import { Network, type Selection } from "./Network";
import type { ViewerModel } from "./model";

interface SceneProps {
  model: ViewerModel;
  onSelect?: ((selection: Selection | null) => void) | undefined;
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
export function Scene({ model, onSelect }: SceneProps): React.JSX.Element {
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
    </Canvas>
  );
}
