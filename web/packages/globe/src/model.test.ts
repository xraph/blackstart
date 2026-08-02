import { describe, expect, it } from 'vitest'

import { assertConsistent, emptyModel, type ViewerModel } from './model'

function twoBus(): ViewerModel {
  return {
    modelId: 'nmdl_test',
    contentHash: 'a'.repeat(64),
    buses: {
      ids: ['bus-1', 'bus-2'],
      names: ['Comanche Peak', 'Dallas'],
      coordinates: [-97.7856, 32.2983, 240, -96.797, 32.7767, 130],
      nominalKv: [345, 345],
    },
    branches: { ids: ['branch-0'], from: [0], to: [1], inService: [true] },
  }
}

describe('assertConsistent', () => {
  it('accepts a well-formed model and an empty one', () => {
    expect(() => assertConsistent(twoBus())).not.toThrow()
    expect(() => assertConsistent(emptyModel)).not.toThrow()
  })

  it('names the array that does not match', () => {
    // In a renderer a short array is an invisible object or a crash deep in a
    // geometry update, and neither names the file that was wrong.
    const short = twoBus()
    short.buses.names = ['Comanche Peak']

    expect(() => assertConsistent(short)).toThrow(/names/)

    const wrongStride = twoBus()
    wrongStride.buses.coordinates = [-97.7856, 32.2983]

    expect(() => assertConsistent(wrongStride)).toThrow(/coordinate/)
  })

  it('refuses a branch pointing past the end of the bus array', () => {
    // Reading past the end of a Float32Array yields undefined rather than
    // throwing, so this becomes NaN vertices and a branch that silently fails
    // to draw.
    const dangling = twoBus()
    dangling.branches.to = [7]

    expect(() => assertConsistent(dangling)).toThrow(/bus index 7/)

    const negative = twoBus()
    negative.branches.from = [-1]

    expect(() => assertConsistent(negative)).toThrow(/bus index -1/)
  })
})
