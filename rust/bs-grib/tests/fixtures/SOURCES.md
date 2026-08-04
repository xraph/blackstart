# Decoder fixtures

Three GRIB2 messages, byte-ranged out of published NOAA products on
2026-08-03. Both products are named in the data source registry in the project
spec §8, and both are U.S. Government Work in the public domain.

A GRIB2 decoder cannot be meaningfully tested against synthetic input: the
formats that matter are the ones NOAA actually emits, and the failure that
matters is a plausible number coming out of a misread stream. These are real
messages, small enough to commit because each is one field pulled by range out
of a much larger file.

| File | Bytes | Grid | Data template | Exercises |
|---|---|---|---|---|
| `hrrr_cicep_surface.grib2` | 188 | 3.30 Lambert | 5.0, zero bits | The constant field: section 7 is empty and every point equals the reference |
| `hrrr_ltng_atmos.grib2` | 70,858 | 3.30 Lambert | 5.3, 12 bits, 10,733 groups | Complex packing with first-order spatial differencing |
| `gfs_clmr_50mb.grib2` | 179 | 3.0 lat/lon | 5.0, zero bits | The other grid template |

## Origins

| | |
|---|---|
| HRRR | `https://noaa-hrrr-bdp-pds.s3.amazonaws.com/hrrr.20260803/conus/hrrr.t12z.wrfsfcf01.grib2` |
| GFS | `https://noaa-gfs-bdp-pds.s3.amazonaws.com/gfs.20260803/12/atmos/gfs.t12z.pgrb2.0p25.f001` |

Byte ranges: CICEP 60262945–60263132, LTNG 34694076–34764933, CLMR
164421814–164421992. Each range came from the product's index sidecar, which
is the same mechanism the ingest pipeline uses.

## The oracle

Spec §10 asks for tolerance tests against an established reference
implementation. That reference is **ecCodes 2.48.0**, ECMWF's own GRIB
library, run out of process: `tests/golden/generate.sh` produces the golden
files and nothing in the build or the test path links against it.

The goldens carry ecCodes' whole-field statistics plus a strided sample of
241 points per message — position and value at each. That checks two things a
self-consistent test cannot: that the values are right in absolute terms, and
that they are in the right places.

Both halves were confirmed to fail on a deliberately broken decoder before
being trusted. A 0.01% scaling error is caught by the maximum; a flipped row
order is caught by the geometry.

`wgrib2` was the oracle originally named for this. It has no Homebrew formula;
ecCodes is bottled, is the ECMWF reference implementation, and reports point
positions as well as values, which `wgrib2` would not have made as easy.
