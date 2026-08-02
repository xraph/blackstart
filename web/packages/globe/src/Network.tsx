import { useMemo } from 'react'
import type { ViewerModel } from './model'
import { toEcefArray } from './wgs84'

/** What a click on the network reports. */
export interface Selection {
  kind: 'bus' | 'branch'
  index: number
  id: string
  label: string
}

interface NetworkProps {
  model: ViewerModel
  onSelect?: ((selection: Selection | null) => void) | undefined
}

/**
 * The network: buses as instanced points, branches as line segments.
 *
 * Both are single draw calls. A 2000-bus network has around 3000 branches, and
 * a mesh per branch is 3000 draw calls a frame — which is the difference
 * between a viewer that rotates smoothly and one that does not.
 */
export function Network({ model, onSelect }: NetworkProps): React.JSX.Element {
  const positions = useMemo(() => {
    const { coordinates } = model.buses
    const points = []

    for (let index = 0; index < coordinates.length; index += 3) {
      points.push({
        lon: coordinates[index]!,
        lat: coordinates[index + 1]!,
        elevation: coordinates[index + 2]!,
      })
    }

    return toEcefArray(points)
  }, [model.buses])

  // Two vertices per branch, looked up from the bus positions rather than
  // re-converted. Converting twice would be correct today and would drift the
  // moment a bus moved without its branches being rebuilt.
  const segments = useMemo(() => {
    const { from, to } = model.branches
    const out = new Float32Array(from.length * 6)

    for (let index = 0; index < from.length; index++) {
      out.set(positions.subarray(from[index]! * 3, from[index]! * 3 + 3), index * 6)
      out.set(positions.subarray(to[index]! * 3, to[index]! * 3 + 3), index * 6 + 3)
    }

    return out
  }, [model.branches, positions])

  // Out-of-service branches are drawn dimmer rather than hidden. One is part
  // of the network in a real state, and hiding it would make the picture
  // disagree with the model about how many lines exist.
  const colors = useMemo(() => {
    const { inService } = model.branches
    const out = new Float32Array(inService.length * 6)

    for (let index = 0; index < inService.length; index++) {
      const shade = inService[index] ? 0.85 : 0.22

      for (let vertex = 0; vertex < 6; vertex++) {
        out[index * 6 + vertex] = shade
      }
    }

    return out
  }, [model.branches])

  return (
    <group>
      <lineSegments
        onPointerDown={(event) => {
          event.stopPropagation()

          const index = event.index === undefined ? -1 : Math.floor(event.index / 2)
          if (index < 0 || index >= model.branches.ids.length) {
            return
          }

          onSelect?.({
            kind: 'branch',
            index,
            id: model.branches.ids[index]!,
            label: model.branches.ids[index]!,
          })
        }}
      >
        <bufferGeometry>
          <bufferAttribute attach="attributes-position" args={[segments, 3]} />
          <bufferAttribute attach="attributes-color" args={[colors, 3]} />
        </bufferGeometry>
        <lineBasicMaterial vertexColors transparent opacity={0.9} />
      </lineSegments>

      <points
        onPointerDown={(event) => {
          event.stopPropagation()

          const index = event.index
          if (index === undefined || index >= model.buses.ids.length) {
            return
          }

          onSelect?.({
            kind: 'bus',
            index,
            id: model.buses.ids[index]!,
            label: model.buses.names[index] || model.buses.ids[index]!,
          })
        }}
      >
        <bufferGeometry>
          <bufferAttribute attach="attributes-position" args={[positions, 3]} />
        </bufferGeometry>
        <pointsMaterial color="#ffd479" size={0.006} sizeAttenuation />
      </points>
    </group>
  )
}
