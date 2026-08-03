# Weather

Every file here is public NOAA output, freely downloadable without an NDA, a
license negotiation or a utility relationship. Both products are named in the
data source registry in the project spec §8.

Retrieved 2026-08-03. These are the files the field vocabulary was verified
against, and the ones its tests still check themselves against — they are
fixtures, not samples.

## hrrr.t12z.wrfsfcf01.grib2.idx

| | |
|---|---|
| Origin | NOAA High-Resolution Rapid Refresh, 3 km CONUS |
| URL | `https://noaa-hrrr-bdp-pds.s3.amazonaws.com/hrrr.20260803/conus/hrrr.t12z.wrfsfcf01.grib2.idx` |
| Cycle | 2026-08-03 12Z, forecast hour 1 |
| Contents | 170 records; the byte offset of every message in a 151 MB file |
| License | U.S. Government Work, public domain |

## gfs.t12z.pgrb2.0p25.f001.idx

| | |
|---|---|
| Origin | NOAA Global Forecast System, 0.25 degree |
| URL | `https://noaa-gfs-bdp-pds.s3.amazonaws.com/gfs.20260803/12/atmos/gfs.t12z.pgrb2.0p25.f001.idx` |
| Cycle | 2026-08-03 12Z, forecast hour 1 |
| License | U.S. Government Work, public domain |

## Why the index and not the data

An index sidecar is a few tens of kilobytes and lists the byte offset of every
message in its file. That is what makes a field fetchable by HTTP range rather
than by downloading 151 MB to keep 1.2 MB of it — and it is why a dataset
version's hash covers the bytes actually read rather than a file mostly
discarded.

The decoder's own fixtures are GRIB2 messages, byte-ranged out of these same
files and catalogued separately when that work lands.
