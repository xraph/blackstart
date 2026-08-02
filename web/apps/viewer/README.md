# viewer

The network on a WGS84 globe. Drag to rotate, scroll to zoom, click an element
to inspect it.

## Getting a model into it

The data path is a static file, not a live API call. The REST surface exists
and is tested, but a static default keeps CORS, auth and streaming off the
critical path and makes the screenshot in the root README reproducible from a
build with no server running.

```bash
bsctl model import data/cases/case3.m
bsctl model export <nmdl_…> --format viewer \
  --coords coords.csv \
  --out web/apps/viewer/public/model.json
pnpm --dir web --filter viewer dev
```

`coords.csv` is `bus,lat,lon[,elevation]`, latitude first — the order every
mapping tool uses and the order the ACTIVSg distribution ships. MATPOWER has no
field for coordinates, which is why they arrive separately; keeping them out of
the case also keeps them out of the content hash, so a substation moving fifty
metres does not invalidate every result computed for the network.

Buses with no coordinate draw at 0°N 0°E and the export says how many. That is
in the Gulf of Guinea, so it is conspicuous — but conspicuous reads as a
rendering fault rather than as missing data unless something says so.

## What it deliberately is not

No atmosphere, no clouds, no terrain tiles, no post-processing, no basemap. The
subject is the network, and each of those would obscure it while adding a
dependency and a licensing question to a project that publishes its licence
audit.

The globe is a shaded WGS84 ellipsoid — a sphere scaled on one axis, which is
exactly an ellipsoid of revolution. It has to agree with the geodetic
conversion about the shape of the earth: a network computed on an ellipsoid and
drawn on a sphere floats 21 km above the surface at the poles.

Branches are drawn as straight segments lifted about 8 km above the ellipsoid.
A straight line between two points on a sphere passes below the surface — by
200 m over 100 km and by 20 km over 1000 km — so without the lift a long
interconnector disappears into the planet and reappears at each end, which
reads as a broken line rather than as a drawing artefact.

## Advisory only

Nothing here writes anywhere. The viewer reads a static export and has no
connection to any operational system.
