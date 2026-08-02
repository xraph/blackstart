/**
 * The viewer payload.
 *
 * Parallel arrays rather than an array of objects. A 2000-bus network with
 * per-span line geometry is hundreds of thousands of vertices, and an array of
 * objects means one allocation per vertex at parse time and a copy into a
 * typed array before upload. Parallel arrays go straight into geometry
 * attributes.
 *
 * The shape is deliberately dumb: no nesting, no references to resolve, no
 * identifiers to join on except the ones a click needs to report. Anything
 * cleverer would be a query language, and the platform already has an API for
 * that.
 */

/** The wire format `bsctl model export --format viewer` writes. */
export interface ViewerModel {
  /** Identifies the model these coordinates came from. */
  modelId: string

  /**
   * The content hash of the network. Present so a view can be cited: a
   * screenshot of a network nobody can reproduce is not evidence of anything.
   */
  contentHash: string

  buses: ViewerBuses
  branches: ViewerBranches
}

/** Substations and buses, drawn as instanced points. */
export interface ViewerBuses {
  /** Identifiers, in the same order as the coordinates. */
  ids: string[]

  /** Human names, same order. Empty strings where a source gave none. */
  names: string[]

  /**
   * Longitude, latitude, elevation triples in degrees and metres — not ECEF.
   *
   * The conversion happens in the browser rather than in the exporter so the
   * payload stays readable and diffable, and so a fix to the geodetic maths
   * does not require re-exporting every model.
   */
  coordinates: number[]

  /** Nominal voltage in kilovolts, one per bus. Zero where unknown. */
  nominalKv: number[]
}

/** Branches, drawn as instanced line segments. */
export interface ViewerBranches {
  ids: string[]

  /** Index into the bus arrays for each end. */
  from: number[]
  to: number[]

  /**
   * Whether each branch is in service. Drawn differently rather than omitted:
   * an out-of-service line is part of the network in a real state, and hiding
   * it would make the picture disagree with the model about how many lines
   * exist.
   */
  inService: boolean[]
}

/** An empty model, for a viewer that has not loaded one yet. */
export const emptyModel: ViewerModel = {
  modelId: '',
  contentHash: '',
  buses: { ids: [], names: [], coordinates: [], nominalKv: [] },
  branches: { ids: [], from: [], to: [], inService: [] },
}

/**
 * Checks a parsed payload before the scene tries to draw it.
 *
 * A payload whose arrays disagree in length produces buses missing their names
 * or branches pointing past the end of the bus array — which in a renderer is
 * either an invisible object or a crash deep in a geometry update, neither of
 * which names the file that was wrong.
 *
 * @throws {Error} naming the array that does not match.
 */
export function assertConsistent(model: ViewerModel): void {
  const busCount = model.buses.ids.length

  if (model.buses.coordinates.length !== busCount * 3) {
    throw new Error(
      `viewer model: ${busCount} buses but ${model.buses.coordinates.length} coordinate ` +
        `values; three are needed per bus (lon, lat, elevation)`,
    )
  }

  for (const [name, array] of [
    ['names', model.buses.names],
    ['nominalKv', model.buses.nominalKv],
  ] as const) {
    if (array.length !== busCount) {
      throw new Error(`viewer model: ${busCount} buses but ${array.length} ${name}`)
    }
  }

  const branchCount = model.branches.ids.length

  for (const [name, array] of [
    ['from', model.branches.from],
    ['to', model.branches.to],
    ['inService', model.branches.inService],
  ] as const) {
    if (array.length !== branchCount) {
      throw new Error(`viewer model: ${branchCount} branches but ${array.length} ${name}`)
    }
  }

  for (let index = 0; index < branchCount; index++) {
    for (const end of [model.branches.from[index]!, model.branches.to[index]!]) {
      if (!Number.isInteger(end) || end < 0 || end >= busCount) {
        throw new Error(
          `viewer model: branch ${model.branches.ids[index]} names bus index ` +
            `${end}, and there are ${busCount} buses`,
        )
      }
    }
  }
}
