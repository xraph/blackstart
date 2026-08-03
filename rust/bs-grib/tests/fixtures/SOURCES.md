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

## The missing oracle

Spec §10 asks for tolerance tests against an established reference
implementation. `wgrib2` is not installed on the machine this was written on,
so the decoded values are currently checked for physical plausibility — count,
finiteness, and a range a lightning threat index can actually take — rather
than compared against a known-good decode.

That is weaker and is recorded as such. The count check is not nothing: it
caught a real offset error that produced 74,743 values for a grid of
1,905,141. But a decoder that is wrong by a constant factor would pass every
assertion here, and closing that gap needs either `wgrib2` or `eccodes`
installed as an out-of-process oracle.
