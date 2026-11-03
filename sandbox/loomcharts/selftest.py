"""Self-test: synthetic Strava CSVs through every chart, full and degraded.

    python -m loomcharts.selftest            # temp dir, removed afterwards
    python loomcharts/selftest.py --out DIR  # keep the CSVs and PNGs in DIR
    python -m loomcharts.selftest --rc PATH  # also check PATH is the active, complete style

Fails (exit 1) when a chart raises, writes no PNG, writes a near-empty one, takes too long
or leaves matplotlib's global settings changed; with --rc also when the image's matplotlibrc
is not the one matplotlib read, differs from STYLE, loom's font does not resolve, or
matplotlib logs a warning (a job would see it in its output).
"""

import logging
import math
import os
import struct
import sys
import tempfile
import time

if __package__ in (None, ""):  # run as a file, also under python -I (no script dir on sys.path)
    sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    os.environ.setdefault("MPLCONFIGDIR", os.path.join(tempfile.gettempdir(), "loomcharts-mpl"))

import matplotlib as mpl  # noqa: E402
import numpy as np  # noqa: E402
import pandas as pd  # noqa: E402

import loomcharts  # noqa: E402

FTP, WEIGHT = 265, 74.0
HR_EDGES = (124, 155, 170, 180)
PZ_EDGES = (0.55 * FTP, 0.75 * FTP, 0.90 * FTP, 1.05 * FTP)
ROUTE_KM = 74.3
CLIMBS = ((17.9, 24.3, 0.061), (47.0, 51.8, 0.050))
DURS = (1, 5, 15, 30, 60, 120, 300, 600, 1200, 1800, 3600, 5400)


def _grade(x):
    g = 0.011 * np.sin(x / 2.3) + 0.008 * np.sin(x / 0.9 + 1.0)
    for a, b, v in ((17.9, 24.3, 0.061), (24.3, 30.6, -0.054), (47.0, 51.8, 0.050), (51.8, 57.2, -0.043)):
        m = (x >= a) & (x < b)
        g[m] = v + 0.007 * np.sin(x[m] * 3.1)
    return g


GX = np.arange(0, ROUTE_KM, 0.1)
GG = _grade(GX)
ALT = 412.0 + np.concatenate([[0], np.cumsum(GG[:-1] * 100)])


def _at(arr, d):
    return arr[min(int(d * 10), len(arr) - 1)]


def _speed(p, grade):
    m, g = WEIGHT + 8.5, 9.81
    lo, hi = 0.5, 25.0
    for _ in range(30):
        v = (lo + hi) / 2
        need = (0.0042 * m * g + m * g * grade + 0.5 * 1.2 * 0.32 * v * v) * v
        lo, hi = (v, hi) if need < p else (lo, v)
    return min(v, 17.5)


def simulate(factor, seed):
    """One ride of the loop at 1 Hz: power from the grade, physics speed, lagging HR, stops."""
    rng = np.random.default_rng(seed)
    stops = {2.1: 40, 35.4: 55, 70.2: 30}
    d, t, noise, hr = 0.0, 0, 0.0, 98.0
    rows, done, p30 = [], set(), []
    while d < ROUTE_KM:
        grade = _at(GG, d)
        if 17.9 <= d < 24.3:
            target = 272
        elif 47.0 <= d < 51.8:
            target = 254
        elif grade < -0.03:
            target = 25
        else:
            target = float(np.clip(186 + 1300 * grade, 60, 300))
        noise = 0.93 * noise + rng.normal(0, 13)
        p = max(0.0, (target * (0.95 if d > 62 else 1.0) + noise) * factor)
        if 40.0 <= d < 40.18:
            p = (740 - 18 * ((d - 40.0) * 1000 / 14)) * factor + rng.normal(0, 25)
        if rng.random() < 0.035 and grade > -0.03:
            p = 0.0
        for s_km, dur in stops.items():
            if s_km not in done and d >= s_km:
                for _ in range(dur):
                    hr += (max(95, hr - 20) - hr) / 30
                    rows.append((t, d, _at(ALT, d), 0.0, 0.0, hr, 0.0, grade, 0))
                    t += 1
                done.add(s_km)
        v = _speed(max(p, 1.0), grade) if p > 0 else max(_speed(1.0, grade), 6.0)
        p30 = (p30 + [p])[-30:]
        drift = max(0.0, (t / 60 - 30) * 0.045)
        hr += (min(184, 90 + 0.29 * float(np.mean(p30)) + drift) - hr) / 25
        cad = 0.0 if p == 0 else (78 if grade > 0.04 else 89) + rng.normal(0, 3)
        rows.append((t, d, _at(ALT, d), v * 3.6, p, hr, cad, grade, 1))
        d += v / 1000
        t += 1
    return pd.DataFrame(rows, columns=["t", "km", "alt", "kmh", "watts", "hr", "cad", "grade", "moving"])


def streams_csv(ride):
    """The sidecar's 5 s resample with zones and a closed GPS loop."""
    p30 = ride.watts.rolling(30, min_periods=1).mean()
    r5 = ride.iloc[::5].copy()
    theta = 2 * math.pi * r5.km / ROUTE_KM
    rad = 0.075 + 0.018 * np.sin(3 * theta) + 0.008 * np.sin(7 * theta)
    lat = 47.30 + rad * np.sin(theta)
    lng = 8.52 + rad * np.cos(theta) / math.cos(math.radians(47.3)) * 1.3
    return pd.DataFrame({
        "elapsed_s": r5.t, "distance_km": r5.km.round(3), "lat": lat.round(6), "lng": lng.round(6),
        "altitude_m": r5.alt.round(1), "speed_kmh": r5.kmh.round(1), "hr": r5.hr.round(0),
        "cadence": r5.cad.round(0), "watts": r5.watts.round(0), "grade_pct": (r5.grade * 100).round(1),
        "temp_c": 9.0, "moving": r5.moving,
        "power_zone": np.digitize(p30.iloc[::5].values, PZ_EDGES) + 1,
        "hr_zone": np.digitize(r5.hr.values, HR_EDGES) + 1,
    })


def climbs_csv(ride):
    rows = []
    for n, (a, b, _) in enumerate(CLIMBS, 1):
        seg = ride[(ride.km >= a) & (ride.km < b)]
        gain = _at(ALT, b) - _at(ALT, a)
        secs = len(seg)
        rows.append({"n": n, "from_km": a, "to_km": b, "len_km": round(b - a, 2), "gain_m": round(gain),
                     "grade_pct": round(gain / ((b - a) * 10), 1), "secs": secs, "vam": round(gain / (secs / 3600)),
                     "avg_w": round(seg.watts.mean()), "wkg": round(seg.watts.mean() / WEIGHT, 2),
                     "avg_hr": round(seg.hr.mean())})
    return pd.DataFrame(rows)


def mean_max(w):
    s = pd.Series(w)
    return {d: float(s.rolling(d).mean().max()) if d <= len(s) else math.nan for d in DURS}


def metrics(ride):
    mv = ride[ride.moving == 1]
    npw = float((ride.watts.rolling(30, min_periods=1).mean() ** 4).mean() ** 0.25)
    moving_s = int(len(mv))
    half = len(mv) // 2
    ef1 = mv.watts.values[:half].mean() / mv.hr.values[:half].mean()
    ef2 = mv.watts.values[half:].mean() / mv.hr.values[half:].mean()
    return {"distance_km": float(ride.km.max()), "moving_s": moving_s, "avg_kmh": ride.km.max() / (moving_s / 3600),
            "gain_m": float(np.clip(np.diff(ride.alt.values), 0, None).sum()), "np_w": npw,
            "tss": moving_s * npw * (npw / FTP) / (FTP * 3600) * 100, "ef": npw / mv.hr.mean(),
            "decoupling_pct": (ef1 - ef2) / ef1 * 100, "best_20m_w": mean_max(ride.watts.values)[1200],
            "avg_hr": float(mv.hr.mean())}


def build(d):
    """Write every CSV the Strava tools attach into d; return their paths."""
    ride, prev1, prev2 = simulate(1.0, 7), simulate(0.972, 11), simulate(0.948, 23)
    p = {k: os.path.join(d, v) for k, v in {
        "streams": "strava_1_streams.csv", "climbs": "strava_1_climbs.csv", "curve": "strava_1_power_curve.csv",
        "baseline": "strava_1_baseline.csv", "same_route": "strava_1_same_route.csv", "weeks": "strava_weeks.csv",
        "activities": "strava_activities.csv"}.items()}
    streams_csv(ride).to_csv(p["streams"], index=False)
    climbs_csv(ride).to_csv(p["climbs"], index=False)

    rng = np.random.default_rng(5)
    dates = pd.date_range("2026-09-20", "2026-10-31", freq="D")
    names = ["Lunch ride", "Pfannenstiel", "Evening ride", "Greifensee loop", "Morning ride", "Uetliberg repeats",
             "Recovery spin", "Lunch ride", "Zürichberg", "Evening ride", "Long ride", "Morning ride"]
    base, curves = [], []
    for k, i in enumerate((2, 5, 9, 12, 16, 18, 24, 26, 30, 33, 37, 40)):
        dist = float(np.clip(rng.normal(52, 17), 24, 118))
        spd, npw, hr = float(rng.normal(27.4, 1.3)), float(rng.normal(206, 14)), float(rng.normal(146, 5))
        moving = dist / spd * 3600
        cp, wp, pmax = rng.normal(224, 8), rng.normal(15500, 2500), rng.normal(760, 60)
        curves.append({dd: (float(min(pmax, cp + wp / (dd + 20))) if dd <= moving else math.nan) for dd in DURS})
        base.append({"activity_id": 100 + k, "date": dates[i].strftime("%Y-%m-%d"), "name": names[k],
                     "sport_type": "Ride", "is_this": 0, "same_route": 0, "distance_km": dist, "moving_s": moving,
                     "avg_kmh": spd, "gain_m": dist * float(rng.normal(11.5, 3)), "np_w": npw,
                     "tss": moving * npw * (npw / FTP) / (FTP * 3600) * 100, "ef": npw / hr,
                     "decoupling_pct": float(np.clip(rng.normal(5.0, 1.8), 1.2, 9.5)),
                     "best_20m_w": curves[-1][1200], "avg_hr": hr})
    for aid, date, r in ((90, "2026-10-12", prev1), (91, "2026-09-27", prev2)):
        base.append({"activity_id": aid, "date": date, "name": "Albis loop", "sport_type": "Ride", "is_this": 0,
                     "same_route": 1, **metrics(r)})
        curves.append(mean_max(r.watts.values))
    base.append({"activity_id": 1, "date": "2026-11-01", "name": "Albis loop", "sport_type": "Ride", "is_this": 1,
                 "same_route": 0, **metrics(ride)})
    bdf = pd.DataFrame(base).sort_values("date")
    bdf.to_csv(p["baseline"], index=False)

    this_mm = mean_max(ride.watts.values)
    pd.DataFrame({"duration_s": DURS, "this_w": [this_mm[d] for d in DURS],
                  "window_best_w": [np.nanmax([c[d] for c in curves]) for d in DURS],
                  "window_median_w": [np.nanmedian([c[d] for c in curves]) for d in DURS]}).round(1) \
        .to_csv(p["curve"], index=False)

    grid = np.arange(0, ROUTE_KM - 0.2, 0.2)
    tt = np.interp(grid, ride.km.values, ride.t.values)
    sr = {"distance_km": grid.round(1)}
    for date, r in (("2026-10-12", prev1), ("2026-09-27", prev2)):
        sr[f"gap_s_{date}"] = (tt - np.interp(grid, r.km.values, r.t.values)).round(0)
    pd.DataFrame(sr).to_csv(p["same_route"], index=False)

    acts = bdf[bdf.is_this == 0]
    runs = pd.DataFrame({"date": ["2026-09-23", "2026-10-02", "2026-10-15", "2026-10-29"], "sport_type": "Run",
                         "distance_km": [8.2, 10.4, 6.1, 12.0], "moving_s": [2700, 3400, 2000, 4100],
                         "gain_m": [60, 110, 40, 150], "tss": [np.nan] * 4})
    wk = pd.concat([bdf[["date", "sport_type", "distance_km", "moving_s", "gain_m", "tss"]], runs])
    wk["week_start"] = (pd.to_datetime(wk.date) - pd.to_timedelta(pd.to_datetime(wk.date).dt.day_of_week, unit="D"))
    weeks = wk.groupby(["week_start", "sport_type"]).agg(count=("date", "size"), distance_km=("distance_km", "sum"),
                                                         moving_s=("moving_s", "sum"), gain_m=("gain_m", "sum"),
                                                         tss=("tss", "sum")).reset_index()
    weeks["iso_week"] = weeks.week_start.dt.isocalendar().week.values
    weeks["week_start"] = weeks.week_start.dt.strftime("%Y-%m-%d")
    weeks[["week_start", "iso_week", "sport_type", "count", "distance_km", "moving_s", "gain_m", "tss"]] \
        .round(1).to_csv(p["weeks"], index=False)

    al = pd.concat([acts.assign(trainer=0), runs.assign(trainer=0, name="Run")], ignore_index=True)
    al["activity_id"] = range(1, len(al) + 1)
    al["avg_w"] = al.get("np_w", np.nan) * 0.9
    al["weighted_w"] = al.get("np_w")
    al["kj"] = al["avg_w"] * al["moving_s"] / 1000
    al["suffer_score"] = al["moving_s"] / 60
    al = al.reindex(columns=["activity_id", "date", "name", "sport_type", "trainer", "distance_km", "moving_s",
                             "gain_m", "avg_kmh", "avg_hr", "avg_w", "weighted_w", "kj", "suffer_score"])
    al.sort_values("date").round(2).to_csv(p["activities"], index=False)
    return p


def _blank(src, dst, cols):
    df = pd.read_csv(src)
    for c in cols:
        if c in df.columns:
            df[c] = np.nan
    df.to_csv(dst, index=False)
    return dst


def variants(p, d):
    """(name, function, args, kwargs, minimum bytes) for every chart, normal and degraded."""
    j = lambda name: os.path.join(d, name)  # noqa: E731
    nopower = _blank(p["streams"], j("streams_nopower.csv"), ["watts", "power_zone"])
    nohr = _blank(p["streams"], j("streams_nohr.csv"), ["hr", "hr_zone", "watts", "power_zone"])
    noalt = _blank(p["streams"], j("streams_noalt.csv"), ["altitude_m"])
    nothing = _blank(p["streams"], j("streams_nothing.csv"),
                     ["distance_km", "lat", "lng", "altitude_m", "speed_kmh", "hr", "cadence", "watts", "grade_pct",
                      "temp_c", "power_zone", "hr_zone"])
    indoor = _blank(p["streams"], j("streams_indoor.csv"), ["lat", "lng"])
    curve_nothis = _blank(p["curve"], j("curve_nothis.csv"), ["this_w"])
    curve_empty = _blank(p["curve"], j("curve_empty.csv"), ["this_w", "window_best_w", "window_median_w"])
    base_nopower = _blank(p["baseline"], j("baseline_nopower.csv"), ["np_w", "tss", "ef", "decoupling_pct",
                                                                     "best_20m_w"])
    b = pd.read_csv(p["baseline"])
    b[b.is_this == 0].to_csv(j("baseline_nothis.csv"), index=False)
    pd.concat([b[b.is_this == 0].head(1), b[b.is_this == 1]]).to_csv(j("baseline_one.csv"), index=False)
    b.head(0).to_csv(j("baseline_empty.csv"), index=False)
    sr = pd.read_csv(p["same_route"])
    sr[["distance_km", sr.columns[1]]].to_csv(j("same_route_one.csv"), index=False)
    many = sr.copy()
    many["gap_s_2026-08-30"] = sr[sr.columns[2]] * 1.4 + 20
    many["gap_s_2026-08-02"] = sr[sr.columns[2]] * 0.6 - 30
    many.to_csv(j("same_route_many.csv"), index=False)
    (sr[["distance_km"]].assign(**{sr.columns[1]: sr[sr.columns[1]] / 8})).to_csv(j("same_route_secs.csv"),
                                                                                   index=False)
    sr[["distance_km"]].to_csv(j("same_route_none.csv"), index=False)
    w = pd.read_csv(p["weeks"])
    _blank(p["weeks"], j("weeks_notss.csv"), ["tss"])
    w[w.week_start == w.week_start.max()].to_csv(j("weeks_one.csv"), index=False)
    long = pd.DataFrame({"week_start": pd.date_range("2026-03-02", periods=30, freq="7D").strftime("%Y-%m-%d"),
                         "iso_week": range(10, 40), "sport_type": "Ride", "count": 3,
                         "distance_km": np.linspace(80, 220, 30), "moving_s": np.linspace(3, 8, 30) * 3600,
                         "gain_m": 900.0, "tss": 150 + 120 * np.sin(np.arange(30) / 3)})
    long.to_csv(j("weeks_long.csv"), index=False)
    full = 40_000
    note = 3_000
    heads = {}
    for k in ("streams", "curve", "baseline", "same_route", "weeks", "activities"):
        heads[k] = j(f"head_{k}.csv")
        pd.read_csv(p[k]).head(0).to_csv(heads[k], index=False)
    header_only = [
        ("head_profile", loomcharts.ride_profile, (heads["streams"], j("head_profile.png")), {}, note),
        ("head_power_curve", loomcharts.power_curve, (heads["curve"], j("head_power_curve.png")), {}, note),
        ("head_rank_strip", loomcharts.rank_strip, (heads["baseline"], j("head_rank_strip.png")), {}, note),
        ("head_same_route", loomcharts.same_route_gap, (heads["same_route"], j("head_same_route.png")), {}, note),
        ("head_route_track", loomcharts.route_track, (heads["streams"], j("head_route_track.png")), {}, note),
        ("head_weekly_load", loomcharts.weekly_load, (heads["weeks"], j("head_weekly_load.png")), {}, note),
        ("head_activities", loomcharts.activities_summary, (heads["activities"], j("head_activities.png")), {},
         note),
    ]
    return header_only + [
        ("profile", loomcharts.ride_profile, (p["streams"], j("profile.png")),
         {"climbs": p["climbs"], "title": "Albis loop · 1 Nov 2026"}, full),
        ("profile_hrzones", loomcharts.ride_profile, (nopower, j("profile_hrzones.png")),
         {"climbs": p["climbs"]}, full),
        ("profile_plain", loomcharts.ride_profile, (nohr, j("profile_plain.png")), {"climbs": p["climbs"]}, full),
        ("profile_noalt", loomcharts.ride_profile, (noalt, j("profile_noalt.png")), {}, full),
        ("profile_nothing", loomcharts.ride_profile, (nothing, j("profile_nothing.png")), {}, note),
        ("profile_badclimbs", loomcharts.ride_profile, (p["streams"], j("profile_badclimbs.png")),
         {"climbs": j("does_not_exist.csv")}, full),
        ("power_curve", loomcharts.power_curve, (p["curve"], j("power_curve.png")), {"weeks": 6}, full),
        ("power_curve_nothis", loomcharts.power_curve, (curve_nothis, j("power_curve_nothis.png")), {}, full),
        ("power_curve_empty", loomcharts.power_curve, (curve_empty, j("power_curve_empty.png")), {}, note),
        ("rank_strip", loomcharts.rank_strip, (p["baseline"], j("rank_strip.png")), {"weeks": 6}, full),
        ("rank_strip_nopower", loomcharts.rank_strip, (base_nopower, j("rank_strip_nopower.png")), {}, full),
        ("rank_strip_nothis", loomcharts.rank_strip, (j("baseline_nothis.csv"), j("rank_strip_nothis.png")), {},
         full),
        ("rank_strip_one", loomcharts.rank_strip, (j("baseline_one.csv"), j("rank_strip_one.png")), {}, 20_000),
        ("rank_strip_empty", loomcharts.rank_strip, (j("baseline_empty.csv"), j("rank_strip_empty.png")), {}, note),
        ("weekly_load", loomcharts.weekly_load, (p["weeks"], j("load.png")), {"sport_type": "Ride"}, full),
        ("weekly_load_all", loomcharts.weekly_load, (p["weeks"], j("load_all.png")), {"metric": "moving_s"}, full),
        ("weekly_load_notss", loomcharts.weekly_load, (j("weeks_notss.csv"), j("load_notss.png")), {}, full),
        ("weekly_load_one", loomcharts.weekly_load, (j("weeks_one.csv"), j("load_one.png")), {}, 20_000),
        ("weekly_load_long", loomcharts.weekly_load, (j("weeks_long.csv"), j("load_long.png")), {}, full),
        ("same_route", loomcharts.same_route_gap, (p["same_route"], j("same_route.png")),
         {"title": "Same loop: time gap"}, full),
        ("same_route_one", loomcharts.same_route_gap, (j("same_route_one.csv"), j("same_route_one.png")), {}, full),
        ("same_route_many", loomcharts.same_route_gap, (j("same_route_many.csv"), j("same_route_many.png")), {},
         full),
        ("same_route_secs", loomcharts.same_route_gap, (j("same_route_secs.csv"), j("same_route_secs.png")), {},
         full),
        ("same_route_none", loomcharts.same_route_gap, (j("same_route_none.csv"), j("same_route_none.png")), {},
         note),
        ("route_track", loomcharts.route_track, (p["streams"], j("route_track.png")), {}, full),
        ("route_track_speed", loomcharts.route_track, (p["streams"], j("route_track_speed.png")),
         {"color_by": "speed"}, full),
        ("route_track_zones", loomcharts.route_track, (p["streams"], j("route_track_zones.png")),
         {"color_by": "power_zone"}, full),
        ("route_track_fallback", loomcharts.route_track, (nohr, j("route_track_fallback.png")),
         {"color_by": "cadence_typo"}, full),
        ("route_track_indoor", loomcharts.route_track, (indoor, j("route_track_indoor.png")), {}, note),
        ("activities", loomcharts.activities_summary, (p["activities"], j("activities.png")), {}, 20_000),
        ("activities_month", loomcharts.activities_summary, (p["activities"], j("activities_month.png")),
         {"metric": "moving_s", "by": "month"}, 15_000),
        ("activities_sport", loomcharts.activities_summary, (p["activities"], j("activities_sport.png")),
         {"metric": "count", "by": "sport_type"}, 15_000),
        ("activities_each", loomcharts.activities_summary, (p["activities"], j("activities_each.png")),
         {"metric": "avg_hr", "by": "activity"}, 20_000),
        ("activities_nometric", loomcharts.activities_summary, (p["activities"], j("activities_nometric.png")),
         {"metric": "avg_w", "by": "week", "title": "Avg power"}, 15_000),
    ]


def png_size(path):
    with open(path, "rb") as f:
        head = f.read(24)
    if head[:8] != b"\x89PNG\r\n\x1a\n" or head[12:16] != b"IHDR":
        raise AssertionError(f"{path}: not a PNG")
    return struct.unpack(">II", head[16:24])


def run(d):
    before = dict(mpl.rcParams)
    paths = build(d)
    failures = []
    for name, fn, args, kwargs, min_bytes in variants(paths, d):
        t0 = time.monotonic()
        try:
            out = fn(*args, **kwargs)
            secs = time.monotonic() - t0
            size = os.path.getsize(out)
            w, h = png_size(out)
            assert out == args[1], f"returned {out}, wanted {args[1]}"
            assert size >= min_bytes, f"{size} bytes, expected at least {min_bytes}"
            assert w >= 900 and h >= 300, f"{w}x{h} px"
            assert w <= 4000 and h <= 4000, f"{w}x{h} px"
            assert secs < 20, f"took {secs:.1f} s"
            print(f"ok   {name:24s} {w:5d}x{h:<5d} {size / 1024:7.0f} KiB {secs:5.2f} s")
        except Exception as e:  # noqa: BLE001 - report every chart, then fail
            failures.append(name)
            print(f"FAIL {name:24s} {type(e).__name__}: {e}")
    changed = [k for k, v in before.items() if k in mpl.rcParams and str(mpl.rcParams[k]) != str(v)]
    if changed:
        failures.append("rcParams")
        print(f"FAIL chart functions changed rcParams: {changed[:5]}")
    loomcharts.apply_style()
    if mpl.rcParams["axes.facecolor"] != loomcharts.BG:
        failures.append("apply_style")
        print("FAIL apply_style did not set the dark style")
    mpl.rcParams.update(before)
    return failures


def check_rc(path):
    """The image default: matplotlib read path at import, it holds STYLE, the font resolves.

    Runs before any chart touches rcParams. A bad key in matplotlibrc only logs a warning
    and a missing font silently falls back, so neither would fail the build otherwise.
    """
    from matplotlib import font_manager
    from loomcharts import _style

    failures = []
    if os.path.realpath(mpl.matplotlib_fname()) != os.path.realpath(path):
        failures.append("rc")
        print(f"FAIL matplotlib read {mpl.matplotlib_fname()}, not {path}")
        return failures
    want = mpl.RcParams({k: v for k, v in _style.STYLE.items() if k not in _style.RC_SKIP})
    wrong = [k for k in want if str(mpl.rcParams[k]) != str(want[k])]
    if wrong:
        failures.append("rc")
        print(f"FAIL matplotlibrc differs from STYLE: {wrong[:5]}")
    files = set()
    for weight in ("normal", "semibold", "bold"):
        try:
            prop = font_manager.FontProperties(family=_style.FONT, weight=weight)
            files.add(font_manager.findfont(prop, fallback_to_default=False))
        except ValueError as e:
            failures.append("font")
            print(f"FAIL font {_style.FONT!r} ({weight}): {e}")
    if "font" not in failures and len(files) != 3:
        failures.append("font")
        print(f"FAIL font {_style.FONT!r}: weights share a file {files}")
    if not failures:
        print(f"ok   rc {path}, font {_style.FONT!r}: {', '.join(sorted(os.path.basename(f) for f in files))}")
    return failures


class _Warnings(logging.Handler):
    """Collects matplotlib's log warnings: in a job they land in the model's tool output."""

    def __init__(self):
        super().__init__(logging.WARNING)
        self.messages = []

    def emit(self, record):
        self.messages.append(record.getMessage())


def main(argv):
    keep = None
    failures = []
    warned = _Warnings()
    logging.getLogger("matplotlib").addHandler(warned)
    if "--rc" in argv:
        failures += check_rc(argv[argv.index("--rc") + 1])
    if "--out" in argv:
        keep = argv[argv.index("--out") + 1]
        os.makedirs(keep, exist_ok=True)
    if keep:
        failures += run(keep)
    else:
        with tempfile.TemporaryDirectory(prefix="loomcharts-") as d:
            failures += run(d)
    # Only in the image: elsewhere the font is missing and every chart warns about it.
    if "--rc" in argv and warned.messages:
        failures.append("warnings")
        for m in dict.fromkeys(warned.messages):
            print(f"FAIL matplotlib warned: {m}")
    if failures:
        print(f"loomcharts selftest: {len(failures)} failed: {', '.join(failures)}")
        return 1
    print("loomcharts selftest: all charts ok")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
