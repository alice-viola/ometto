#!/usr/bin/env python3
"""
"Is this point inside the region, or within N metres of it?", asked ten million
times.

A polygon test per point against the region's 3,776-point ring would be tens of
billions of segment tests, so the polygon is rasterised once into a grid of
500 m cells (the region is 330 x 340 cells), and the buffer is a dilation of
that grid by a disk of the right radius. Asking is then one array lookup, and
the answer is right to within half a cell -- which is the point of a 3 km buffer
whose only job is to keep the roads that cross the border.

    from geomask import Mask
    m = Mask.from_geojson("web/public/region.geojson", buffer_m=3000)
    m.contains(46.07, 11.12)        # one point
"""
import json
import math
import pathlib

import numpy as np


class Mask:
    def __init__(self, rings, cell_m=500.0, buffer_m=0.0, verbose=True):
        lats = [p[1] for r in rings for p in r]
        lons = [p[0] for r in rings for p in r]
        self.lat0 = (min(lats) + max(lats)) / 2
        self.m_lat = 111_132.92 - 559.82 * math.cos(2 * math.radians(self.lat0))
        self.m_lon = 111_412.84 * math.cos(math.radians(self.lat0))
        self.cell = float(cell_m)

        pad = buffer_m + 2 * cell_m
        self.x0 = min(lons) * self.m_lon - pad
        self.y0 = min(lats) * self.m_lat - pad
        w = int((max(lons) * self.m_lon + pad - self.x0) / cell_m) + 1
        h = int((max(lats) * self.m_lat + pad - self.y0) / cell_m) + 1
        self.w, self.h = w, h

        grid = np.zeros((h, w), dtype=bool)
        # Scanline fill: for one row of cell centres, every edge that straddles
        # it contributes one crossing, and the spans between sorted crossings
        # alternate outside/inside. Vectorised over edges, looped over rows --
        # 340 rows against 3,776 edges is nothing, a per-cell test would be 40M.
        edges = []
        for r in rings:
            a = np.array([[p[0] * self.m_lon, p[1] * self.m_lat] for p in r])
            edges.append(np.stack([a[:-1], a[1:]], axis=1))
        E = np.concatenate(edges, axis=0)
        x0e, y0e, x1e, y1e = E[:, 0, 0], E[:, 0, 1], E[:, 1, 0], E[:, 1, 1]
        dy = y1e - y0e
        dy_safe = np.where(dy == 0, 1e-12, dy)
        for row in range(h):
            y = self.y0 + (row + 0.5) * cell_m
            hit = (y0e > y) != (y1e > y)
            if not hit.any():
                continue
            xs = x0e[hit] + (y - y0e[hit]) * (x1e[hit] - x0e[hit]) / dy_safe[hit]
            xs.sort()
            cols = np.ceil((xs - self.x0) / cell_m - 0.5).astype(np.int64)
            np.clip(cols, 0, w, out=cols)
            for a, b in zip(cols[0::2], cols[1::2]):
                if b > a:
                    grid[row, a:b] = True

        if buffer_m > 0:
            grid = self._dilate(grid, int(math.ceil(buffer_m / cell_m)))
        self.grid = grid
        if verbose:
            print(f"  mask {h} x {w} cells of {cell_m:.0f} m, "
                  f"{100.0 * grid.mean():.1f}% inside (+{buffer_m:.0f} m buffer)")

    @staticmethod
    def _dilate(grid, r):
        """Dilate by a DISK of r cells: a square would add 41% at the corners."""
        h, w = grid.shape
        out = np.zeros_like(grid)
        for dr in range(-r, r + 1):
            dc_max = int(math.sqrt(max(r * r - dr * dr, 0)))
            src_r0, src_r1 = max(0, -dr), min(h, h - dr)
            dst_r0, dst_r1 = max(0, dr), min(h, h + dr)
            for dc in range(-dc_max, dc_max + 1):
                src_c0, src_c1 = max(0, -dc), min(w, w - dc)
                dst_c0, dst_c1 = max(0, dc), min(w, w + dc)
                if src_r1 <= src_r0 or src_c1 <= src_c0:
                    continue
                out[dst_r0:dst_r1, dst_c0:dst_c1] |= grid[src_r0:src_r1, src_c0:src_c1]
        return out

    @classmethod
    def from_geojson(cls, path, buffer_m=0.0, cell_m=500.0, kinds=("region",), verbose=True):
        """The outer rings of the features whose `kind` is asked for."""
        fc = json.loads(pathlib.Path(path).read_text())
        rings = []
        for f in fc.get("features", []):
            if kinds and f.get("properties", {}).get("kind") not in kinds:
                continue
            g = f["geometry"]
            polys = g["coordinates"] if g["type"] == "MultiPolygon" else [g["coordinates"]]
            for poly in polys:
                rings.append(poly[0])       # outer ring; holes are not enclaves of ours
        if not rings:
            raise SystemExit(f"{path}: no feature of kind {kinds}")
        return cls(rings, cell_m=cell_m, buffer_m=buffer_m, verbose=verbose)

    def contains(self, lat, lon):
        c = int((lon * self.m_lon - self.x0) / self.cell)
        r = int((lat * self.m_lat - self.y0) / self.cell)
        if r < 0 or c < 0 or r >= self.h or c >= self.w:
            return False
        return bool(self.grid[r, c])

    def contains_any(self, geom):
        """geom: [{"lat","lon"}, ...] -- true as soon as one point is in."""
        for g in geom:
            if self.contains(g["lat"], g["lon"]):
                return True
        return False
