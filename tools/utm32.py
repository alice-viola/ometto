#!/usr/bin/env python3
"""
ETRS89 / UTM zone 32N <-> WGS84, in numpy, because pyproj is not installed.

The Province's open data (the SAT trail catalogue, the huts) is published in
EPSG:25832: transverse Mercator on GRS80, central meridian 9 E, scale 0.9996,
false easting 500 km. ETRS89 and WGS84 have drifted about 60 cm apart since
1989 -- a tenth of a DEM pixel, and nothing next to the 60 m this is used to
judge -- so the datum shift is deliberately not applied.

Snyder's series (USGS PP 1395, 8-17 to 8-25), which is exact to a millimetre
within a zone. Both directions are here so that each can check the other.

    python3 tools/utm32.py            # the self-checks
"""
import math

import numpy as np

A = 6378137.0                      # GRS80, and WGS84 to within 0.1 mm
F = 1 / 298.257222101
E2 = F * (2 - F)
EP2 = E2 / (1 - E2)
K0 = 0.9996
LON0 = math.radians(9.0)
FE = 500000.0
FN = 0.0

_M1 = 1 - E2 / 4 - 3 * E2**2 / 64 - 5 * E2**3 / 256
_M2 = 3 * E2 / 8 + 3 * E2**2 / 32 + 45 * E2**3 / 1024
_M3 = 15 * E2**2 / 256 + 45 * E2**3 / 1024
_M4 = 35 * E2**3 / 3072


def _meridian_arc(phi):
    return A * (_M1 * phi - _M2 * np.sin(2 * phi) + _M3 * np.sin(4 * phi) - _M4 * np.sin(6 * phi))


def to_lonlat(east, north):
    """(easting, northing) in metres -> (lon, lat) in degrees."""
    x = np.asarray(east, dtype=np.float64) - FE
    y = np.asarray(north, dtype=np.float64) - FN
    mu = (y / K0) / (A * _M1)
    e1 = (1 - math.sqrt(1 - E2)) / (1 + math.sqrt(1 - E2))
    phi1 = (mu
            + (3 * e1 / 2 - 27 * e1**3 / 32) * np.sin(2 * mu)
            + (21 * e1**2 / 16 - 55 * e1**4 / 32) * np.sin(4 * mu)
            + (151 * e1**3 / 96) * np.sin(6 * mu)
            + (1097 * e1**4 / 512) * np.sin(8 * mu))
    s, c, t = np.sin(phi1), np.cos(phi1), np.tan(phi1)
    C1 = EP2 * c * c
    T1 = t * t
    N1 = A / np.sqrt(1 - E2 * s * s)
    R1 = A * (1 - E2) / (1 - E2 * s * s) ** 1.5
    D = x / (N1 * K0)
    lat = phi1 - (N1 * t / R1) * (
        D**2 / 2
        - (5 + 3 * T1 + 10 * C1 - 4 * C1**2 - 9 * EP2) * D**4 / 24
        + (61 + 90 * T1 + 298 * C1 + 45 * T1**2 - 252 * EP2 - 3 * C1**2) * D**6 / 720)
    lon = LON0 + (
        D
        - (1 + 2 * T1 + C1) * D**3 / 6
        + (5 - 2 * C1 + 28 * T1 - 3 * C1**2 + 8 * EP2 + 24 * T1**2) * D**5 / 120) / c
    return np.degrees(lon), np.degrees(lat)


def to_utm(lon, lat):
    """(lon, lat) in degrees -> (easting, northing) in metres."""
    phi = np.radians(np.asarray(lat, dtype=np.float64))
    lam = np.radians(np.asarray(lon, dtype=np.float64))
    s, c, t = np.sin(phi), np.cos(phi), np.tan(phi)
    N = A / np.sqrt(1 - E2 * s * s)
    T = t * t
    C = EP2 * c * c
    Aa = (lam - LON0) * c
    M = _meridian_arc(phi)
    east = FE + K0 * N * (
        Aa + (1 - T + C) * Aa**3 / 6
        + (5 - 18 * T + T**2 + 72 * C - 58 * EP2) * Aa**5 / 120)
    north = FN + K0 * (M + N * t * (
        Aa**2 / 2 + (5 - T + 9 * C + 4 * C**2) * Aa**4 / 24
        + (61 - 58 * T + T**2 + 600 * C - 330 * EP2) * Aa**6 / 720))
    return east, north


if __name__ == "__main__":
    # Trento cathedral, the reference point of every check in this project.
    lon, lat = 11.1213, 46.0669
    e, n = to_utm(lon, lat)
    print(f"Trento cathedral {lat} N {lon} E  ->  E {float(e):.3f}  N {float(n):.3f}")
    lo2, la2 = to_lonlat(e, n)
    dlat = (float(la2) - lat) * 111132.0
    dlon = (float(lo2) - lon) * 77000.0
    print(f"  round trip back: {float(la2):.9f} N {float(lo2):.9f} E "
          f"({dlat*1000:+.3f} mm N, {dlon*1000:+.3f} mm E)")

    # A grid over the province: the worst round-trip error anywhere in it.
    la = np.linspace(45.6, 47.2, 40)
    lo = np.linspace(10.3, 12.5, 40)
    LA, LO = np.meshgrid(la, lo)
    E_, N_ = to_utm(LO, LA)
    LO2, LA2 = to_lonlat(E_, N_)
    err = np.hypot((LA2 - LA) * 111132.0, (LO2 - LO) * 77000.0)
    print(f"  worst round trip over the region: {err.max()*1000:.4f} mm")

    # On the central meridian the northing is exactly k0 times the meridian
    # arc, which is an independent formula: if the series above were wrong,
    # these two would not agree to a millimetre.
    for la in (45.7, 46.4, 47.1):
        n_grid = float(to_utm(9.0, la)[1])
        n_arc = K0 * float(_meridian_arc(math.radians(la)))
        print(f"  at {la} N on the central meridian: grid {n_grid:.4f} m, "
              f"arc {n_arc:.4f} m, difference {1000*(n_grid-n_arc):+.4f} mm")
    # And the grid's own scale: at 2 degrees off the meridian it must be a
    # little OVER 1 (0.9996 at the meridian, 1.0 at ~180 km either side).
    d = 1000.0
    e0, n0 = to_utm(11.0, 46.0)
    e1, n1 = to_utm(11.0 + d / (111412.84 * math.cos(math.radians(46.0))), 46.0)
    print(f"  {d:.0f} m of ground east at 11 E measures "
          f"{math.hypot(float(e1-e0), float(n1-n0)):.2f} m in the grid")
