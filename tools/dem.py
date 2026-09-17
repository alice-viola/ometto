#!/usr/bin/env python3
"""
Give every point of a built map its elevation, from the Copernicus DEM GLO-30.

The map files carry positions in local metres, which is what the renderer and the
routing want, but a bike route over the Bondone and a bike route along the Adige
are not the same ride. This tool projects every point back to lat/lon, samples the
30 m digital surface model, and writes the result back as a parallel "z" array.

    python3 tools/dem.py fetch                       # the tiles covering Trentino
    python3 tools/dem.py fetch web/public/region-bbox.json   # ... or any bbox
    python3 tools/dem.py fetch 45.67,10.38,47.10,12.48       # ... S,W,N,E
    python3 tools/dem.py at 46.0669 11.1213          # one elevation, for checks
    python3 tools/dem.py add-z web/public/city.json  # add "z" to a built map

The tiles are public Copernicus GLO-30 COG GeoTIFFs on AWS (~45 MB each, 1 arcsec
grid, EPSG:4326, 176 MB for the four). They land outside the repo -- see TILE_DIR
below, or set QUEEN_DEM_DIR to put them where you want them. `fetch` takes a bbox
(a {"south","west","north","east"} file or S,W,N,E) and downloads every one-degree
tile it touches, skipping the ones already there; the whole region is nine tiles.
Whatever is in TILE_DIR is what `at` and `add-z` mosaic, so a map that grew past
the tiles it was built from only needs another `fetch`.

Needs numpy, tifffile and imagecodecs (the tiles are deflate with the floating
point predictor, which tifffile hands to imagecodecs); no GDAL, no rasterio.

    python3 -m pip install --user numpy tifffile imagecodecs
"""

import json
import math
import os
import pathlib
import re
import shutil
import ssl
import subprocess
import sys
import time
import urllib.request

import numpy as np
import tifffile

# Where the tiles live. Big, re-downloadable, and not ours: keep them out of git.
# $QUEEN_DEM_DIR when it is set (tools/refresh-region.sh points it at its work
# folder), else a cache in $HOME; `fetch` fills an empty one in about ten seconds.
TILE_DIR = (
    pathlib.Path(os.environ["QUEEN_DEM_DIR"])
    if os.environ.get("QUEEN_DEM_DIR")
    else pathlib.Path.home() / ".cache" / "queen-dem"
)

# The default for a bare `fetch`: the tiles that cover Trentino
# (bbox 45.66..46.57 N, 10.42..12.00 E). Trentino-Alto Adige as a whole reaches
# 47.09 N and 12.48 E, which is nine tiles -- pass its bbox to `fetch`.
DEFAULT_BBOX = (45.66, 10.42, 46.57, 12.00)   # S, W, N, E

BASE = "https://copernicus-dem-30m.s3.amazonaws.com"

# Copernicus voids and sea are flagged with a large negative; be generous.
VOID_BELOW = -1000.0


def tiles_for(bbox):
    """Every 1x1 degree tile the bbox touches, as (lat, lon) of its SW corner.

    Copernicus names a tile by the corner it starts at, so the tile holding
    46.9 N is N46: floor, and the north/east edge is exclusive unless the bbox
    ends exactly on it.
    """
    s, w, n, e = bbox
    out = []
    for la in range(math.floor(s), math.floor(n) + 1):
        for lo in range(math.floor(w), math.floor(e) + 1):
            out.append((la, lo))
    return out


def tiles_present():
    """The (lat, lon) of every tile actually sitting in TILE_DIR."""
    out = []
    if not TILE_DIR.is_dir():
        return out
    for p in sorted(TILE_DIR.glob("Copernicus_DSM_COG_10_*_DEM.tif")):
        m = re.match(r"Copernicus_DSM_COG_10_([NS])(\d{2})_00_([EW])(\d{3})_00_DEM", p.name)
        if not m:
            continue
        la = int(m.group(2)) * (1 if m.group(1) == "N" else -1)
        lo = int(m.group(4)) * (1 if m.group(3) == "E" else -1)
        out.append((la, lo))
    return out


def parse_bbox(arg):
    """S,W,N,E, or a JSON file with south/west/north/east."""
    p = pathlib.Path(arg)
    if p.exists():
        b = json.loads(p.read_text())
        return (float(b["south"]), float(b["west"]), float(b["north"]), float(b["east"]))
    parts = [float(v) for v in arg.split(",")]
    if len(parts) != 4:
        sys.exit(f"bbox must be S,W,N,E or a file with south/west/north/east: {arg}")
    return tuple(parts)


def tile_name(lat, lon):
    """The bucket's naming: Copernicus_DSM_COG_10_N46_00_E011_00_DEM."""
    ns = "N" if lat >= 0 else "S"
    ew = "E" if lon >= 0 else "W"
    return (
        f"Copernicus_DSM_COG_10_{ns}{abs(lat):02d}_00_{ew}{abs(lon):03d}_00_DEM"
    )


def tile_url(lat, lon):
    n = tile_name(lat, lon)
    return f"{BASE}/{n}/{n}.tif"


def tile_path(lat, lon):
    return TILE_DIR / f"{tile_name(lat, lon)}.tif"


# ---------------------------------------------------------------- fetch


CURL = shutil.which("curl")


def _ssl_context():
    """A context that actually verifies.

    A python.org build with no `Install Certificates.command` run has an empty
    trust store, and every HTTPS call dies with CERTIFICATE_VERIFY_FAILED; use
    certifi's bundle when that is the case. Never disable verification.
    """
    try:
        import certifi
        return ssl.create_default_context(cafile=certifi.where())
    except Exception:
        return ssl.create_default_context()


def head(url):
    """Content-Length of url, or None if it does not resolve."""
    if CURL:
        r = subprocess.run(
            [CURL, "-sILf", "--max-time", "30", url],
            capture_output=True, text=True,
        )
        if r.returncode != 0:
            print(f"  HEAD failed: curl exit {r.returncode}")
            return None
        size = 0
        for line in r.stdout.splitlines():
            if line.lower().startswith("content-length:"):
                size = int(line.split(":", 1)[1].strip())
        return size
    req = urllib.request.Request(url, method="HEAD")
    try:
        with urllib.request.urlopen(req, timeout=30, context=_ssl_context()) as r:
            return int(r.headers.get("Content-Length") or 0)
    except Exception as e:  # 404 or network
        print(f"  HEAD failed: {e}")
        return None


def download(url, dest):
    """Stream url to dest, via a .part file so a kill cannot leave a half tile."""
    part = dest.with_suffix(dest.suffix + ".part")
    t0 = time.time()
    if CURL:
        r = subprocess.run(
            [CURL, "-fL", "--retry", "3", "--retry-delay", "2",
             "-o", str(part), "--progress-bar", url]
        )
        if r.returncode != 0:
            part.unlink(missing_ok=True)
            sys.exit(f"download failed (curl exit {r.returncode}): {url}")
        got = part.stat().st_size
    else:
        got = 0
        with urllib.request.urlopen(url, timeout=120, context=_ssl_context()) as r, \
                open(part, "wb") as f:
            total = int(r.headers.get("Content-Length") or 0)
            while True:
                chunk = r.read(1 << 20)
                if not chunk:
                    break
                f.write(chunk)
                got += len(chunk)
                if total:
                    print(f"\r    {got/1e6:6.1f} / {total/1e6:.1f} MB "
                          f"{100.0*got/total:5.1f}%", end="", flush=True)
    print(f"\r    {got/1e6:6.1f} MB in {time.time()-t0:.1f}s" + " " * 20)
    part.replace(dest)
    return got


def cmd_fetch(argv):
    bbox = parse_bbox(argv[0]) if argv else DEFAULT_BBOX
    tiles = tiles_for(bbox)
    TILE_DIR.mkdir(parents=True, exist_ok=True)
    print(f"tiles -> {TILE_DIR}")
    print(f"bbox {bbox[0]}..{bbox[2]} N {bbox[1]}..{bbox[3]} E: {len(tiles)} tile(s)")
    missing = 0
    for lat, lon in tiles:
        dest = tile_path(lat, lon)
        url = tile_url(lat, lon)
        if dest.exists() and dest.stat().st_size > 1 << 20:
            print(f"  have {dest.name} ({dest.stat().st_size/1e6:.1f} MB)")
            continue
        size = head(url)
        if not size:
            print(f"  !! {url} does not resolve -- check the bucket index:")
            print(f"     {BASE}/?prefix={tile_name(lat, lon)[:28]}")
            missing += 1
            continue
        print(f"  get  {dest.name} ({size/1e6:.1f} MB)")
        download(url, dest)
    if missing:
        print(f"{missing} tile(s) missing")
        return 1
    print(f"all {len(tiles)} tiles present")
    return 0


# ---------------------------------------------------------------- the DEM


class Dem:
    """A lat/lon-addressed float32 mosaic of the tiles, cropped to what is asked.

    Cropping matters: a tile is 3600x3600, so the nine that cover the region are
    ~460 MB of float32, while the region's own bbox is a third of that. Which
    tiles exist is read off TILE_DIR rather than a list in this file, and only
    the ones that overlap the asked-for bounds are opened at all.
    """

    def __init__(self, bounds=None, margin=0.02, verbose=True):
        have = tiles_present()
        if bounds is not None:
            la_min, la_max, lo_min, lo_max = bounds
            have = [(la, lo) for la, lo in have
                    if la <= la_max + margin and la + 1 >= la_min - margin
                    and lo <= lo_max + margin and lo + 1 >= lo_min - margin]
        paths = [tile_path(la, lo) for la, lo in have]
        paths = [p for p in paths if p.exists()]
        if not paths:
            sys.exit(f"no DEM tiles for this area in {TILE_DIR} -- run: "
                     f"python3 tools/dem.py fetch <bbox>")

        heads = [self._read_header(p) for p in paths]

        # Every tile must share one grid, or the mosaic is a lie.
        sx = heads[0]["sx"]
        sy = heads[0]["sy"]
        for h in heads[1:]:
            if abs(h["sx"] - sx) > 1e-12 or abs(h["sy"] - sy) > 1e-12:
                sys.exit(
                    "tiles have different pixel scales "
                    f"({h['path'].name}: {h['sx']} vs {sx}); "
                    "Copernicus thins the grid above 50 N, so a mosaic "
                    "across that line needs resampling this tool does not do."
                )
        self.sx, self.sy = sx, sy

        # Union grid: pixel CENTRES, north-up, row 0 at the top.
        lon0 = min(h["lon0"] for h in heads)
        lat0 = max(h["lat0"] for h in heads)
        full_w = max(round((h["lon0"] - lon0) / sx) + h["w"] for h in heads)
        full_h = max(round((lat0 - h["lat0"]) / sy) + h["h"] for h in heads)

        # Crop to the asked-for bounds (+ margin) so we only hold what we sample.
        c_lo, r_lo, c_hi, r_hi = 0, 0, full_w - 1, full_h - 1
        if bounds is not None:
            la_min, la_max, lo_min, lo_max = bounds
            c_lo = max(0, int(math.floor((lo_min - margin - lon0) / sx)) - 1)
            c_hi = min(full_w - 1, int(math.ceil((lo_max + margin - lon0) / sx)) + 1)
            r_lo = max(0, int(math.floor((lat0 - (la_max + margin)) / sy)) - 1)
            r_hi = min(full_h - 1, int(math.ceil((lat0 - (la_min - margin)) / sy)) + 1)
            if c_hi < c_lo or r_hi < r_lo:
                sys.exit("the requested area does not overlap the DEM tiles")

        self.w = c_hi - c_lo + 1
        self.h = r_hi - r_lo + 1
        self.lon0 = lon0 + c_lo * sx          # centre lon of mosaic column 0
        self.lat0 = lat0 - r_lo * sy          # centre lat of mosaic row 0

        # NaN = no data, which is exactly what the sampler wants to see.
        self.z = np.full((self.h, self.w), np.nan, dtype=np.float32)

        for h in heads:
            off_c = round((h["lon0"] - lon0) / sx)
            off_r = round((lat0 - h["lat0"]) / sy)
            # Intersection of this tile with the crop window, in union coords.
            a_c, b_c = max(off_c, c_lo), min(off_c + h["w"] - 1, c_hi)
            a_r, b_r = max(off_r, r_lo), min(off_r + h["h"] - 1, r_hi)
            if b_c < a_c or b_r < a_r:
                continue
            if verbose:
                print(f"  read {h['path'].name}")
            arr = self._read_data(h)
            # Straight into the mosaic and masked in place: a np.where per tile
            # would cost two more full-tile copies for nothing.
            dst = self.z[a_r - r_lo : b_r - r_lo + 1, a_c - c_lo : b_c - c_lo + 1]
            dst[:] = arr[a_r - off_r : b_r - off_r + 1, a_c - off_c : b_c - off_c + 1]
            del arr
            nd = h["nodata"]
            if nd is not None:
                dst[dst == np.float32(nd)] = np.nan
            dst[dst < VOID_BELOW] = np.nan
            del dst

        self._near_cache = {}
        if verbose:
            good = int(np.isfinite(self.z).sum())
            print(
                f"  grid {self.h} x {self.w} @ {self.sx*3600:.3f}\" "
                f"({self.z.nbytes/1e6:.0f} MB), {100.0*good/self.z.size:.2f}% valid"
            )

    # -- GeoTIFF plumbing -------------------------------------------------

    @staticmethod
    def _raster_type(tags):
        """GTRasterTypeGeoKey, read straight out of GeoKeyDirectoryTag.

        tifffile's own `geotiff_metadata` is not on TiffPage in every version,
        and the default when it is missing is the WRONG one for these tiles, so
        parse the directory: a 4-word header, then (KeyID, Location, Count,
        Value) quads. Location 0 means the value is inline. Copernicus tiles say
        2 = RasterPixelIsPoint: the tiepoint is the CENTRE of pixel (0,0), and
        assuming otherwise slides the whole grid half a pixel (~15 m).
        """
        if "GeoKeyDirectoryTag" not in tags:
            return None
        v = list(tags["GeoKeyDirectoryTag"].value)
        n = v[3] if len(v) > 3 else 0
        for k in range(n):
            key, loc, count, val = v[4 + 4 * k : 8 + 4 * k]
            if key == 1025 and loc == 0 and count == 1:
                return int(val)
        return None

    @staticmethod
    def _read_header(path):
        """Grid geometry of a tile, straight out of the GeoTIFF tags.

        ModelTiepointTag ties raster point (i,j) to model (lon,lat) and
        ModelPixelScaleTag gives the step. Whether that raster point is the
        CORNER of the pixel (RasterPixelIsArea, the default) or its CENTRE
        (RasterPixelIsPoint) is GTRasterTypeGeoKey's business -- get it wrong
        and everything slides half a pixel, ~15 m, which on a cliff is 100 m
        of elevation.
        """
        with tifffile.TiffFile(path) as tif:
            page = tif.pages[0]
            tags = page.tags
            tie = tuple(tags["ModelTiepointTag"].value)
            scale = tuple(tags["ModelPixelScaleTag"].value)
            h, w = int(page.imagelength), int(page.imagewidth)
            raster_type = Dem._raster_type(tags)
            nodata = None
            if "GDAL_NODATA" in tags:
                try:
                    nodata = float(str(tags["GDAL_NODATA"].value).strip("\x00 \t\n"))
                except ValueError:
                    nodata = None

        if raster_type is None:
            raise SystemExit(
                f"{path.name}: no GTRasterTypeGeoKey; refusing to guess whether "
                "the tiepoint is a pixel corner or a pixel centre"
            )

        i0, j0, _, x0, y0, _ = tie[:6]
        sx, sy = float(scale[0]), float(scale[1])
        # Offset from the tie raster point to the centre of pixel (0,0).
        off = 0.5 if raster_type == 1 else 0.0   # 1 = PixelIsArea, 2 = PixelIsPoint
        lon0 = x0 + (0 + off - i0) * sx
        lat0 = y0 - (0 + off - j0) * sy
        return {
            "path": path, "w": w, "h": h, "sx": sx, "sy": sy,
            "lon0": lon0, "lat0": lat0, "nodata": nodata,
            "raster_type": raster_type,
        }

    @staticmethod
    def _read_data(hdr):
        # maxworkers is deliberately small: this box runs the live demo.
        with tifffile.TiffFile(hdr["path"]) as tif:
            try:
                return tif.pages[0].asarray(maxworkers=2)
            except TypeError:
                return tif.pages[0].asarray()

    # -- sampling ---------------------------------------------------------

    def sample(self, lat, lon, chunk=500_000):
        """Bilinear elevation (float, NaN-free) for arrays of lat/lon.

        Chunked, because the bilinear needs a dozen temporaries the size of the
        input and a whole province is millions of points: unchunked, sampling
        alone would ask the box for half a gigabyte it does not need.
        """
        lat, lon = np.broadcast_arrays(
            np.asarray(lat, dtype=np.float64), np.asarray(lon, dtype=np.float64)
        )
        shape = lat.shape
        flat_lat = np.ravel(lat)
        flat_lon = np.ravel(lon)
        out = np.empty(flat_lat.size, dtype=np.float64)
        nbad = 0
        for s in range(0, flat_lat.size, chunk):
            e = min(s + chunk, flat_lat.size)
            out[s:e], b = self._sample_block(flat_lat[s:e], flat_lon[s:e])
            nbad += b
        return out.reshape(shape), nbad

    def _sample_block(self, lat, lon):
        """One chunk's worth of bilinear sampling."""
        # Fractional pixel-centre coordinates. Clipping is what makes a point
        # outside the tiles take the nearest edge value instead of a NaN.
        fc = np.clip((lon - self.lon0) / self.sx, 0.0, self.w - 1.0)
        fr = np.clip((self.lat0 - lat) / self.sy, 0.0, self.h - 1.0)

        c0 = np.clip(np.floor(fc).astype(np.int64), 0, max(self.w - 2, 0))
        r0 = np.clip(np.floor(fr).astype(np.int64), 0, max(self.h - 2, 0))
        c1 = np.minimum(c0 + 1, self.w - 1)
        r1 = np.minimum(r0 + 1, self.h - 1)
        tc = (fc - c0).astype(np.float64)
        tr = (fr - r0).astype(np.float64)

        z = self.z
        v00 = z[r0, c0].astype(np.float64)
        v01 = z[r0, c1].astype(np.float64)
        v10 = z[r1, c0].astype(np.float64)
        v11 = z[r1, c1].astype(np.float64)

        w00 = (1 - tr) * (1 - tc)
        w01 = (1 - tr) * tc
        w10 = tr * (1 - tc)
        w11 = tr * tc

        # Voids are dropped from the average rather than poisoning it.
        num = np.zeros(fc.shape, dtype=np.float64)
        den = np.zeros(fc.shape, dtype=np.float64)
        for v, wgt in ((v00, w00), (v01, w01), (v10, w10), (v11, w11)):
            ok = np.isfinite(v)
            num += np.where(ok, wgt * np.nan_to_num(v), 0.0)
            den += np.where(ok, wgt, 0.0)

        out = np.zeros(fc.shape, dtype=np.float64)
        good = den > 0
        out[good] = num[good] / den[good]

        bad = ~good
        nbad = int(bad.sum())
        if nbad:
            # All four corners void: walk outwards for the nearest real value.
            idx = np.nonzero(bad.ravel())[0]
            rr = np.rint(fr).astype(np.int64).ravel()
            cc = np.rint(fc).astype(np.int64).ravel()
            flat = out.ravel()
            for k in idx:
                flat[k] = self._nearest(int(rr[k]), int(cc[k]))
            out = flat.reshape(out.shape)
        return out, nbad

    def _nearest(self, r, c, maxrad=128):
        key = (r >> 4, c >> 4)
        if key in self._near_cache:
            return self._near_cache[key]
        val = 0.0
        rad = 2
        while rad <= maxrad:
            r0, r1 = max(0, r - rad), min(self.h, r + rad + 1)
            c0, c1 = max(0, c - rad), min(self.w, c + rad + 1)
            win = self.z[r0:r1, c0:c1]
            m = np.isfinite(win)
            if m.any():
                yy, xx = np.nonzero(m)
                d = (yy + r0 - r) ** 2 + (xx + c0 - c) ** 2
                k = int(np.argmin(d))
                val = float(win[yy[k], xx[k]])
                break
            rad *= 2
        self._near_cache[key] = val
        return val

    def at(self, lat, lon):
        z, _ = self.sample(np.array([lat]), np.array([lon]))
        return float(z[0])


# ---------------------------------------------------------------- map files


TOKEN = re.compile(r'"(?:[^"\\]|\\.)*"|[\[\]{}]')


def value_end(text, i, dec):
    """Offset just past the JSON value starting at i.

    Cheap for a flat [[x,y],[x,y],...] array: it holds no strings and never
    nests deeper than two, so the first "]]" ends it. Worth the special case,
    because raw_decode on a few million pairs builds -- and immediately throws
    away -- a gigabyte of Python lists and floats just to report where the
    array stopped, on a box that is also running the demo. Every assumption is
    checked, and anything else goes to the real parser.
    """
    if text.startswith("[[", i) and not text.startswith("[[[", i):
        j = text.find("]]", i)
        if j != -1:
            j += 2
            k = j
            while k < len(text) and text[k] in " \t\r\n":
                k += 1
            if (
                k < len(text)
                and text[k] in ",}"
                and text.find('"', i, j) == -1
                and text.count("[", i, j) == text.count("]", i, j)
            ):
                return j

    if text[i] in "[{":
        # Anything else nested: count brackets, with the regex swallowing whole
        # string literals so a "[" inside a road name cannot throw the count.
        depth = 0
        for m in TOKEN.finditer(text, i):
            t = m.group()
            if t[0] == '"':
                continue
            if t in "[{":
                depth += 1
            else:
                depth -= 1
                if depth == 0:
                    return m.end()
        raise ValueError("unterminated JSON value")

    _v, j = dec.raw_decode(text, i)   # a scalar, and cheap
    return j


def scan_top_level(text):
    """Byte spans of the top-level members of a JSON object.

    Returns [(key, start, end)], where text[start:end] is `"key":value` exactly
    as written. Splicing these back together is how every other key of a 150 MB
    map file survives byte-for-byte: nothing is re-serialised but "z".
    """
    dec = json.JSONDecoder()
    i = text.index("{") + 1
    n = len(text)
    out = []

    def skip_ws(j):
        while j < n and text[j] in " \t\r\n":
            j += 1
        return j

    while True:
        i = skip_ws(i)
        if i >= n:
            raise ValueError("unterminated JSON object")
        if text[i] == "}":
            return out, i
        if text[i] == ",":
            i += 1
            continue
        start = i
        key, i = dec.raw_decode(text, i)
        i = skip_ws(i)
        if text[i] != ":":
            raise ValueError(f"expected ':' at {i}")
        i = value_end(text, skip_ws(i + 1), dec)
        out.append((key, start, i))


BRACKETS = str.maketrans("", "", "[]")


def points_xy(text, start, end):
    """The points array as an (N, 2) float array, without 2.6M Python lists.

    json.loads on a few million [x, y] pairs costs about a gigabyte of list and
    float objects, on a box that is also running the demo. The array holds
    nothing but numbers and brackets, so stripping the brackets leaves a plain
    comma-separated number stream that numpy parses straight into a buffer.
    Anything unexpected (a third component, a nested shape) fails the count
    check and falls back to the honest parser.
    """
    body = text[start:end]
    n = body.count("[") - 1              # minus the outer one
    if n <= 0:
        return np.zeros((0, 2), dtype=np.float64)
    flat = np.fromstring(body.translate(BRACKETS), dtype=np.float64, sep=",")
    if flat.size == 2 * n:
        return flat.reshape(n, 2)
    xy = np.asarray(json.loads(body), dtype=np.float64)
    if xy.ndim != 2 or xy.shape[1] < 2:
        sys.exit(f"points has unexpected shape {xy.shape}")
    return xy[:, :2]


def cmd_add_z(argv):
    if not argv:
        sys.exit("usage: python3 tools/dem.py add-z <city.json>")
    path = pathlib.Path(argv[0]).resolve()
    if not path.exists():
        sys.exit(f"no such map file: {path}")

    t0 = time.time()
    print(f"read {path} ({path.stat().st_size/1e6:.1f} MB)")
    text = path.read_text()
    members, _close = scan_top_level(text)
    keys = [k for k, _, _ in members]
    if "points" not in keys or "meta" not in keys:
        sys.exit(f"not a built map (top-level keys: {keys})")
    print(f"  keys: {', '.join(keys)}")

    dec = json.JSONDecoder()
    span = {k: (s, e) for k, s, e in members}

    def value_start(k):
        s, _e = span[k]
        i = text.index(":", s) + 1
        while text[i] in " \t\r\n":
            i += 1
        return i

    def member(k):
        v, _ = dec.raw_decode(text, value_start(k))
        return v

    meta = member("meta")
    xy = points_xy(text, value_start("points"), span["points"][1])
    t_parse = time.time()
    print(f"  {len(xy):,} points parsed in {t_parse-t0:.1f}s")

    origin = meta["origin"]
    m_lat = float(meta["mPerDegLat"])
    m_lon = float(meta["mPerDegLon"])

    x = xy[:, 0]
    y = xy[:, 1]
    # y grows SOUTH, hence the minus on latitude.
    lat = float(origin["lat"]) - y / m_lat
    lon = float(origin["lon"]) + x / m_lon
    del xy

    print(f"  span {lat.min():.4f}..{lat.max():.4f} N, "
          f"{lon.min():.4f}..{lon.max():.4f} E")

    t_dem = time.time()
    dem = Dem(bounds=(lat.min(), lat.max(), lon.min(), lon.max()))
    print(f"  DEM loaded in {time.time()-t_dem:.1f}s")

    t_s = time.time()
    z, nbad = dem.sample(lat, lon)
    zi = np.rint(z).astype(np.int32)
    print(f"  sampled in {time.time()-t_s:.1f}s"
          + (f" ({nbad} point(s) fell in a void)" if nbad else ""))

    print(f"  z: min {int(zi.min())} m, max {int(zi.max())} m, "
          f"median {int(np.median(zi))} m, mean {zi.mean():.1f} m")

    z_json = json.dumps(zi.tolist(), separators=(",", ":"))
    tmp = path.with_suffix(path.suffix + ".tmp")
    step = 1 << 23   # slicing a 58 MB member in one go just to write it is a copy
    with open(tmp, "w") as f:
        f.write("{")
        first = True
        for k, s, e in members:
            if k == "z":
                continue          # a previous run's; replaced below
            if not first:
                f.write(",")
            for c in range(s, e, step):
                f.write(text[c : min(c + step, e)])
            first = False
        if not first:
            f.write(",")
        f.write('"z":')
        f.write(z_json)
        f.write("}")
    os.replace(tmp, path)
    print(f"wrote {path} ({path.stat().st_size/1e6:.1f} MB) "
          f"in {time.time()-t0:.1f}s total")
    return 0


def cmd_at(argv):
    if len(argv) < 2:
        sys.exit("usage: python3 tools/dem.py at <lat> <lon>")
    lat, lon = float(argv[0]), float(argv[1])
    dem = Dem(bounds=(lat, lat, lon, lon), margin=0.01, verbose=False)
    print(f"{dem.at(lat, lon):.1f}")
    return 0


USAGE = """usage:
  python3 tools/dem.py fetch [bbox]          download the Copernicus GLO-30 tiles
                                             (bbox: S,W,N,E or a bbox JSON file)
  python3 tools/dem.py at <lat> <lon>        print one elevation in metres
  python3 tools/dem.py add-z <city.json>     add a "z" array to a built map
"""


def main(argv):
    if not argv:
        print(USAGE)
        return 2
    cmd, rest = argv[0], argv[1:]
    if cmd == "fetch":
        return cmd_fetch(rest)
    if cmd == "at":
        return cmd_at(rest)
    if cmd == "add-z":
        return cmd_add_z(rest)
    print(USAGE)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
