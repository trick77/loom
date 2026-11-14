"""Self-test: synthetic Strava CSVs through every chart, full and degraded.

    python -m loomcharts.selftest            # temp dir, removed afterwards
    python loomcharts/selftest.py --out DIR  # keep the CSVs and PNGs in DIR
    python -m loomcharts.selftest --rc PATH  # also check PATH is the active, complete style

Fails (exit 1) when a chart raises, writes no PNG, writes a near-empty one, takes too long
or leaves matplotlib's global settings changed; with --rc also when the image's matplotlibrc
is not the one matplotlib read, differs from STYLE, loom's font does not resolve, or
matplotlib logs a warning (a job would see it in its output).
"""

import contextlib
import logging
import math
import os
import struct
import sys
import tempfile
import time
import warnings

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
    # The tool's last two columns: Strava's total clock and the part not recorded.
    bdf["not_recorded_s"] = (bdf["activity_id"] % 3) * 120
    bdf["elapsed_time_s"] = bdf["moving_s"] + bdf["not_recorded_s"] + 60
    bdf = bdf[[c for c in bdf.columns if c not in ("elapsed_time_s", "not_recorded_s")] + ["elapsed_time_s", "not_recorded_s"]]
    bdf["avg_w"] = (bdf["np_w"] * 0.94).round(0)
    # Flags as the sidecar writes them (bdf keeps its 0/1 ints); the ride's streams keep 0/1: a
    # model's own DataFrame may hold them so, and loomcharts reads both.
    bdf.assign(is_this=bdf["is_this"].map(flag), same_route=bdf["same_route"].map(flag)).to_csv(p["baseline"],
                                                                                              index=False)
    p.update(activity_files(d))

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
    al["elapsed_time_s"] = al["moving_s"] + 90
    al = al.reindex(columns=["activity_id", "date", "name", "sport_type", "trainer", "distance_km", "moving_s",
                             "gain_m", "avg_kmh", "avg_hr", "avg_w", "weighted_w", "kj", "suffer_score",
                             "elapsed_time_s"])
    al.sort_values("date").round(2).to_csv(p["activities"], index=False)
    return p


RUN_HR_EDGES = (132, 146, 154, 162)


def activity_files(d):
    """A run's streams (7.5 km, steady with a surge), the load series of an analysis and of a list."""
    rng = np.random.default_rng(3)
    rows, t, km, hr = [], 0, 0.0, 120.0
    while km < 7.55:
        v = 2.55 if km < 3.75 else 2.48
        if 4.0 <= km < 4.6:
            v = 3.1
        v += rng.normal(0, 0.05)
        hr += (min(172, 128 + 9 * v + (8 if 4.0 <= km < 4.9 else 0) + t / 400) - hr) / 30
        w = 88 * v + rng.normal(0, 8)
        rows.append({"elapsed_s": t, "distance_km": round(km, 3), "lat": "", "lng": "", "altitude_m": 412.0,
                     "speed_kmh": round(v * 3.6, 1), "hr": round(hr), "cadence": round(81 + rng.normal(0, 1.5)),
                     "watts": round(w), "grade_pct": 0.0, "temp_c": "", "moving": "true", "power_zone": "",
                     "hr_zone": int(np.digitize(hr, RUN_HR_EDGES) + 1)})
        km += v * 5 / 1000
        t += 5
    run = os.path.join(d, "strava_2_streams.csv")
    pd.DataFrame(rows).to_csv(run, index=False)

    days = pd.date_range("2026-07-05", "2026-11-01", freq="D")
    load = np.where(rng.random(len(days)) < 0.55, rng.normal(70, 25, len(days)).clip(20, 160), 0.0)
    load[-6:] = [90, 0, 120, 60, 0, 140]
    ctl = atl = 0.0
    out = []
    for day, x in zip(days, load):
        ctl += (x - ctl) / 42
        atl += (x - atl) / 7
        out.append({"date": day.strftime("%Y-%m-%d"), "load": round(x), "ctl": round(ctl, 1), "atl": round(atl, 1),
                    "form": round(ctl - atl, 1), "acwr": round(atl / ctl, 2) if ctl >= 10 else "", "ramp": "",
                    "monotony": "", "strain": "", "ctl_before": "", "atl_before": ""})
    out[-1]["ctl_before"] = round(out[-2]["ctl"] + (0 - out[-2]["ctl"]) / 42, 1)
    out[-1]["atl_before"] = round(out[-2]["atl"] + (0 - out[-2]["atl"]) / 7, 1)
    p = {"run_streams": run, "load": os.path.join(d, "strava_2_load.csv"), "load_list": os.path.join(d, "strava_load.csv"),
         "bests": os.path.join(d, "strava_run_bests.csv"), "predictions": os.path.join(d, "strava_run_predictions.csv")}
    bests = []
    for i, month in enumerate(pd.period_range("2025-12", "2026-10", freq="M").astype(str)):
        # Strava's names: lower-case k.
        for name, dist, base in (("1k", 1000, 290), ("5k", 5000, 1560), ("10k", 10000, 3260)):
            if name == "10k" and i % 3:
                continue
            secs = base * (1 - 0.006 * i) + rng.normal(0, base * 0.01)
            bests.append({"month": month, "effort": name, "distance_m": dist, "secs": round(secs), "activity_id": 100 + i,
                          "date": f"{month}-15"})
    pd.DataFrame(bests).to_csv(p["bests"], index=False)
    pd.DataFrame([{"race": "5K", "distance_m": 5000, "vdot_s": 1452, "riegel_s": 1460, "vdot": 41.2, "threshold_s_km": 303},
                  {"race": "10K", "distance_m": 10000, "vdot_s": 3015, "riegel_s": 3050, "vdot": 41.2, "threshold_s_km": 303},
                  {"race": "half marathon", "distance_m": 21097.5, "vdot_s": 6660, "riegel_s": 6780, "vdot": 41.2,
                   "threshold_s_km": 303},
                  {"race": "marathon", "distance_m": 42195, "vdot_s": 13870, "riegel_s": 14140, "vdot": 41.2,
                   "threshold_s_km": 303}]).to_csv(p["predictions"], index=False)
    pd.DataFrame(out).to_csv(p["load"], index=False)
    lst = pd.DataFrame(out)
    lst["ctl_before"], lst["atl_before"] = "", ""
    lst.to_csv(p["load_list"], index=False)
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
                                                                     "best_20m_w", "avg_w"])
    run_nozones = _blank(p["run_streams"], j("run_nozones.csv"), ["hr_zone"])
    run_nopower = _blank(p["run_streams"], j("run_nopower.csv"), ["watts"])
    run_nohr = _blank(p["run_streams"], j("run_nohr.csv"), ["hr", "hr_zone"])
    b = pd.read_csv(p["baseline"])
    b[~b.is_this].to_csv(j("baseline_nothis.csv"), index=False)
    pd.concat([b[~b.is_this].head(1), b[b.is_this]]).to_csv(j("baseline_one.csv"), index=False)
    b.head(0).to_csv(j("baseline_empty.csv"), index=False)
    alike_csv(j("baseline_alike.csv"))
    rs = pd.read_csv(p["run_streams"])
    d0 = rs["distance_km"].min()
    rs[rs["distance_km"] <= d0 + 3.2].to_csv(j("run_short.csv"), index=False)
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
    for k in ("streams", "curve", "baseline", "same_route", "weeks", "activities", "load"):
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
        ("head_load_profile", loomcharts.load_profile, (heads["baseline"], j("head_load_profile.png")), {}, note),
        ("head_fitness", loomcharts.fitness_fatigue, (heads["load"], j("head_fitness.png")), {}, note),
        ("head_hr_profile", loomcharts.hr_profile, (heads["streams"], j("head_hr_profile.png")), {}, note),
        ("head_splits", loomcharts.split_variation, (heads["streams"], j("head_splits.png")), {}, note),
    ]
    activity = [
        ("load_profile", loomcharts.load_profile, (p["baseline"], j("load_profile.png")), {"weeks": 6}, full),
        ("load_profile_de", loomcharts.load_profile, (p["baseline"], j("load_profile_de.png")),
         {"weeks": 6, "lang": "de"}, full),
        ("load_profile_nopower", loomcharts.load_profile, (base_nopower, j("load_profile_nopower.png")), {}, full),
        ("load_profile_alike", loomcharts.load_profile, (j("baseline_alike.csv"), j("load_profile_alike.png")),
         {"weeks": 6}, full),
        ("load_profile_one", loomcharts.load_profile, (j("baseline_one.csv"), j("load_profile_one.png")), {}, 20_000),
        ("load_profile_nothis", loomcharts.load_profile, (j("baseline_nothis.csv"), j("load_profile_nothis.png")),
         {}, note),
        ("fitness_fatigue", loomcharts.fitness_fatigue, (p["load"], j("fitness_fatigue.png")), {}, full),
        ("fitness_fatigue_de", loomcharts.fitness_fatigue, (p["load"], j("fitness_fatigue_de.png")), {"lang": "de"},
         full),
        ("fitness_fatigue_list", loomcharts.fitness_fatigue, (p["load_list"], j("fitness_fatigue_list.png")), {},
         full),
        ("hr_profile", loomcharts.hr_profile, (p["run_streams"], j("hr_profile.png")), {}, full),
        ("hr_profile_de", loomcharts.hr_profile, (p["run_streams"], j("hr_profile_de.png")), {"lang": "de"}, full),
        ("hr_profile_ride", loomcharts.hr_profile, (p["streams"], j("hr_profile_ride.png")), {}, full),
        ("hr_profile_nozones", loomcharts.hr_profile, (run_nozones, j("hr_profile_nozones.png")), {}, 20_000),
        ("hr_profile_nohr", loomcharts.hr_profile, (run_nohr, j("hr_profile_nohr.png")), {}, note),
        ("splits", loomcharts.split_variation, (p["run_streams"], j("splits.png")), {"sport": "Run"}, full),
        ("splits_de", loomcharts.split_variation, (p["run_streams"], j("splits_de.png")),
         {"sport": "Run", "lang": "de"}, full),
        ("splits_hr", loomcharts.split_variation, (run_nopower, j("splits_hr.png")), {"sport": "Run"}, full),
        ("splits_short", loomcharts.split_variation, (j("run_short.csv"), j("splits_short.png")), {"sport": "Run"},
         20_000),
        ("splits_ride", loomcharts.split_variation, (p["streams"], j("splits_ride.png")), {"sport": "Ride"}, full),
        ("splits_nothing", loomcharts.split_variation, (nothing, j("splits_nothing.png")), {}, note),
        ("run_bests", loomcharts.run_bests, (p["bests"], j("run_bests.png")), {"predictions": p["predictions"]}, full),
        ("run_bests_de", loomcharts.run_bests, (p["bests"], j("run_bests_de.png")),
         {"predictions": p["predictions"], "lang": "de"}, full),
        ("run_bests_nopred", loomcharts.run_bests, (p["bests"], j("run_bests_nopred.png")), {}, full),
        ("run_bests_badpred", loomcharts.run_bests, (p["bests"], j("run_bests_badpred.png")),
         {"predictions": j("does_not_exist.csv")}, full),
        ("run_bests_empty", loomcharts.run_bests, (j("bests_empty.csv"), j("run_bests_empty.png")), {}, note),
        ("run_bests_gap", loomcharts.run_bests, (j("bests_gap.csv"), j("run_bests_gap.png")),
         {"lang": "de", "predictions": p["predictions"]}, full),
        ("run_bests_mi", loomcharts.run_bests, (p["bests"], j("run_bests_mi.png")), {"unit": "mi"}, full),
        ("efficiency", loomcharts.efficiency_trend, (j("efficiency.csv"), j("efficiency.png")), {}, full),
        ("efficiency_de", loomcharts.efficiency_trend, (j("efficiency.csv"), j("efficiency_de.png")),
         {"sport": "cycling", "lang": "de"}, full),
        ("efficiency_one", loomcharts.efficiency_trend, (j("efficiency_one.csv"), j("efficiency_one.png")), {}, 15_000),
        ("efficiency_gap", loomcharts.efficiency_trend, (j("efficiency_gap.csv"), j("efficiency_gap.png")), {}, full),
        ("wbal", loomcharts.wbal, (j("wbal.csv"), j("wbal.png")), {"cp": 255, "w_prime": 18500}, full),
        ("wbal_de", loomcharts.wbal, (j("wbal.csv"), j("wbal_de.png")), {"cp": 255, "w_prime": 18500, "lang": "de"}, full),
        ("wbal_empty", loomcharts.wbal, (j("wbal_empty.csv"), j("wbal_empty.png")), {}, note),
        ("grade_pace", loomcharts.grade_pace, (j("grade_pace.csv"), j("grade_pace.png")), {}, full),
        ("grade_pace_de", loomcharts.grade_pace, (j("grade_pace.csv"), j("grade_pace_de.png")), {"lang": "de"}, full),
        ("grade_pace_empty", loomcharts.grade_pace, (j("grade_pace_empty.csv"), j("grade_pace_empty.png")), {}, note),
        ("efficiency_short", loomcharts.efficiency_trend, (j("efficiency_short.csv"), j("efficiency_short.png")), {}, 15_000),
        ("efficiency_empty", loomcharts.efficiency_trend, (j("efficiency_empty.csv"), j("efficiency_empty.png")), {}, note),
    ]
    pd.read_csv(p["bests"]).head(0).to_csv(j("bests_empty.csv"), index=False)
    rng = np.random.default_rng(9)
    eff = pd.DataFrame([{"date": (pd.Timestamp("2025-11-05") + pd.Timedelta(days=4 * i)).strftime("%Y-%m-%d"),
                         "activity_id": i, "sport_type": "Run", "ef": round(1.30 + 0.0004 * i + rng.normal(0, 0.04), 3),
                         "avg_hr": 140, "temp_c": "", "easy": int(i % 4 != 0)} for i in range(90)])
    eff.loc[eff.easy == 0, "ef"] += 0.12
    eff["easy"] = eff["easy"].map(flag)
    eff.to_csv(j("efficiency.csv"), index=False)
    eff.head(1).to_csv(j("efficiency_one.csv"), index=False)
    eff.head(0).to_csv(j("efficiency_empty.csv"), index=False)
    ride = pd.read_csv(p["streams"])
    bal, wb = [], 18500.0
    for w in ride.watts.fillna(0):
        for _ in range(5):  # 5 s rows
            wb = wb - (w - 255) if w > 255 else wb + (255 - w) * (18500 - wb) / 18500
        bal.append(round(wb / 1000, 2))
    pd.DataFrame({"elapsed_s": ride.elapsed_s, "distance_km": ride.distance_km, "watts": ride.watts, "wbal_kj": bal}) \
        .to_csv(j("wbal.csv"), index=False)
    pd.DataFrame(columns=["elapsed_s", "distance_km", "watts", "wbal_kj"]).to_csv(j("wbal_empty.csv"), index=False)
    grades = list(range(-12, 13))
    pd.DataFrame({"grade_pct": grades, "rel_speed": [1 - 0.035 * g if g > 0 else 1 - 0.012 * g - 0.002 * g * g for g in grades],
                  "seconds": [max(60, 3000 - 220 * abs(g)) for g in grades]}) \
        .assign(pace_s_km=lambda f: (380 / f.rel_speed).round(0)) \
        .to_csv(j("grade_pace.csv"), index=False)
    pd.DataFrame(columns=["grade_pct", "rel_speed", "pace_s_km", "seconds"]).to_csv(j("grade_pace_empty.csv"), index=False)
    eff[eff.easy == "true"].head(2).assign(date=["2026-11-03", "2026-11-04"]).to_csv(j("efficiency_short.csv"), index=False)
    gap = eff[(pd.to_datetime(eff.date) < "2026-02-01") | (pd.to_datetime(eff.date) >= "2026-06-01")]
    gap.to_csv(j("efficiency_gap.csv"), index=False)
    # A six-month break, and 1K and 1 mile at near the same pace.
    pd.DataFrame([{"month": m, "effort": e, "distance_m": dm, "secs": s, "activity_id": 1, "date": f"{m}-10"}
                  for m in ("2025-12", "2026-01", "2026-07", "2026-08")
                  for e, dm, s in (("1K", 1000, 290), ("1 mile", 1609, 470), ("Half-Marathon", 21097, 6900))]) \
        .to_csv(j("bests_gap.csv"), index=False)
    # A job's inputs sit in in/; the tools name them bare, and a bare name must be found there.
    os.makedirs(j("in"), exist_ok=True)
    pd.read_csv(p["streams"]).to_csv(os.path.join(d, "in", "bare_streams.csv"), index=False)

    def in_job_dir(fn):
        def call(*args, **kwargs):
            with contextlib.chdir(d):
                return fn(*args, **kwargs)
        return call

    return header_only + activity + outdoor_variants(d, full, note) + [
        # Absolute out: the call runs inside d, and --out may give a relative d.
        ("profile_bare_name", in_job_dir(loomcharts.ride_profile),
         ("bare_streams.csv", os.path.join(os.path.abspath(d), "profile_bare.png")), {}, full),
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


def outdoor_files(d):
    """The outdoor_windows tool's CSVs: today from 08:00 and tomorrow, rain this afternoon, wind tomorrow."""
    rows = []
    for i in range(40):
        t = pd.Timestamp("2026-11-09 08:00") + pd.Timedelta(hours=i)
        h = t.hour
        lit = 1 if 7 <= h < 17 else 0
        wet = t.day == 9 and 13 <= h < 16
        windy = t.day == 10 and 11 <= h < 16
        rows.append({
            "time": t.strftime("%Y-%m-%dT%H:%M"), "daylight": flag(lit),
            "temp_c": round(4 + 6 * max(0.0, 1 - abs(h - 14) / 7), 1),
            "feels_c": round(1 + 6 * max(0.0, 1 - abs(h - 14) / 7), 1),
            "sun_min": (50 if lit and not wet else 0), "rain_mm": (1.2 if wet else 0.0),
            "rain_pct": (85 if wet else 10), "wind_kmh": (32.0 if windy else 9.0 + h % 4),
            "gust_kmh": (52.0 if windy else 20.0 + h % 5), "wind_dir_deg": (250 if windy else 200 + 10 * (h % 6)),
        })
    p = {k: os.path.join(d, f"outdoor_{k}.csv") for k in ("hours", "windows", "days", "limits")}
    pd.DataFrame(rows).to_csv(p["hours"], index=False)
    pd.DataFrame([
        {"date": "2026-11-09", "rank": 1, "from": "09:00", "to": "11:00", "dry": 1, "wind": "", "sun_min": 100,
         "rain_pct": 10, "wind_max_kmh": 12, "gust_max_kmh": 24, "wind_from": "SW", "cold": 1},
        {"date": "2026-11-09", "rank": 2, "from": "16:00", "to": "17:00", "dry": 1, "wind": "", "sun_min": 30,
         "rain_pct": 10, "wind_max_kmh": 9, "gust_max_kmh": 21, "wind_from": "S", "cold": 1},
        {"date": "2026-11-10", "rank": 1, "from": "08:15", "to": "10:15", "dry": 1, "wind": "windy", "sun_min": 90,
         "rain_pct": 10, "wind_max_kmh": 18, "gust_max_kmh": 31, "wind_from": "W", "cold": 0},
    ]).to_csv(p["windows"], index=False)
    week = []
    for i in range(7):
        date = pd.Timestamp("2026-11-09") + pd.Timedelta(days=i)
        week.append({"date": date.strftime("%Y-%m-%d"), "sunrise": "07:21", "sunset": "16:59", "high_c": 8 + i % 4,
                     "low_c": 1 + i % 3, "rain_mm": [3.6, 0.2, 0, 1.4, 0, 6.2, 0][i], "rain_pct": [90, 20, 5, 55, 10, 95, 0][i],
                     "sun_h": [1, 4, 7, 2, 5, 0, 6][i], "wind_max_kmh": [12, 36, 10, 14, 44, 20, 9][i],
                     "gust_max_kmh": [25, 58, 22, 28, 70, 40, 18][i], "wind_from": ["S", "W", "NE", "SW", "W", "S", "E"][i],
                     "rain": ["wet", "dry", "dry", "showers", "dry", "wet", "dry"][i],
                     "wind": ["", "windy", "", "", "strong", "windy", ""][i], "rank": [6, 3, 1, 5, 4, 7, 2][i],
                     "cold": int(8 + i % 4 < 10)})
    pd.DataFrame(week).to_csv(p["days"], index=False)
    limits = {"activity": "ride", "minutes": 120, "date": "2026-11-10", "wind_kmh": 15, "wind_strong_kmh": 39,
              "gust_kmh": 30, "gust_strong_kmh": 45, "cold_c": 10, "cold_sunny_c": 7}
    pd.DataFrame([limits]).to_csv(p["limits"], index=False)
    return p


def outdoor_variants(d, full, note):
    """The going-out charts, full and with what may be missing left out."""
    j = lambda name: os.path.join(d, name)  # noqa: E731
    o = outdoor_files(d)
    bare = _blank(o["hours"], j("outdoor_hours_bare.csv"), ["feels_c", "sun_min", "rain_pct", "gust_kmh",
                                                            "wind_dir_deg", "daylight"])
    pd.read_csv(o["hours"]).head(0).to_csv(j("outdoor_hours_empty.csv"), index=False)
    pd.read_csv(o["days"]).head(0).to_csv(j("outdoor_days_empty.csv"), index=False)
    _blank(o["days"], j("outdoor_days_bare.csv"), ["sun_h", "gust_max_kmh", "wind_from", "rain", "wind", "rank"])
    _blank(o["days"], j("outdoor_days_nolow.csv"), ["low_c"])
    h = pd.read_csv(o["hours"])
    stormy = h["time"].str.startswith("2026-11-09")
    h.loc[stormy, "gust_kmh"] = 70.0
    h.loc[stormy, "rain_mm"] = 4.5
    h.to_csv(j("outdoor_hours_storm_today.csv"), index=False)
    late = pd.read_csv(o["hours"])
    late[late["time"] >= "2026-11-09T22:00"].to_csv(j("outdoor_hours_late.csv"), index=False)
    return [
        ("outdoor_compare_late", loomcharts.outdoor_compare, (j("outdoor_hours_late.csv"), j("outdoor_compare_late.png")),
         {"windows": o["windows"], "limits": o["limits"]}, 20_000),
        ("outdoor_day", loomcharts.outdoor_day, (o["hours"], j("outdoor_day.png")),
         {"windows": o["windows"], "limits": o["limits"]}, full),
        ("outdoor_day_tomorrow", loomcharts.outdoor_day, (o["hours"], j("outdoor_day_tomorrow.png")),
         {"windows": o["windows"], "limits": o["limits"], "date": "2026-11-10"}, full),
        ("outdoor_day_bare", loomcharts.outdoor_day, (bare, j("outdoor_day_bare.png")), {}, 20_000),
        ("outdoor_day_nodate", loomcharts.outdoor_day, (o["hours"], j("outdoor_day_nodate.png")),
         {"date": "2027-01-01"}, note),
        ("outdoor_day_empty", loomcharts.outdoor_day, (j("outdoor_hours_empty.csv"), j("outdoor_day_empty.png")), {},
         note),
        ("outdoor_compare", loomcharts.outdoor_compare, (o["hours"], j("outdoor_compare.png")),
         {"windows": o["windows"], "limits": o["limits"]}, full),
        ("outdoor_compare_bare", loomcharts.outdoor_compare, (bare, j("outdoor_compare_bare.png")),
         {"windows": j("does_not_exist.csv")}, 20_000),
        ("outdoor_compare_storm", loomcharts.outdoor_compare,
         (j("outdoor_hours_storm_today.csv"), j("outdoor_compare_storm.png")),
         {"windows": o["windows"], "limits": o["limits"]}, full),
        ("outdoor_week_nolow", loomcharts.outdoor_week, (j("outdoor_days_nolow.csv"), j("outdoor_week_nolow.png")),
         {"limits": o["limits"]}, 20_000),
        ("outdoor_week", loomcharts.outdoor_week, (o["days"], j("outdoor_week.png")), {"limits": o["limits"]}, full),
        ("outdoor_week_bare", loomcharts.outdoor_week, (j("outdoor_days_bare.csv"), j("outdoor_week_bare.png")), {},
         20_000),
        ("outdoor_week_empty", loomcharts.outdoor_week, (j("outdoor_days_empty.csv"), j("outdoor_week_empty.png")),
         {}, note),
    ]


def png_size(path):
    with open(path, "rb") as f:
        head = f.read(24)
    if head[:8] != b"\x89PNG\r\n\x1a\n" or head[12:16] != b"IHDR":
        raise AssertionError(f"{path}: not a PNG")
    return struct.unpack(">II", head[16:24])


def run(d):
    # The charts draw from the state a job starts in (the image rc, if any).
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
    # From matplotlib's built-in defaults: in the image the rc already holds STYLE,
    # so a no-op apply_style would pass from there.
    mpl.rcdefaults()
    loomcharts.apply_style()
    if mpl.rcParams["axes.facecolor"] != loomcharts.BG:
        failures.append("apply_style")
        print("FAIL apply_style did not set the dark style")
    mpl.rcParams.update(before)
    failures += check_input_path(d)
    failures += check_spider_scale()
    failures += check_zone_gradient()
    failures += check_flags(os.path.join(d, "strava_1_baseline.csv"))
    return failures


def alike_csv(path):
    """Seven runs within a few percent of each other, this one a bit faster and longer: the
    load profile once drew it as a perfect hexagon on the average's ring."""
    rows = []
    for k, f in enumerate((0.97, 1.02, 0.99, 1.04, 0.96, 1.01, 1.0)):
        day = (pd.Timestamp("2026-10-01") + pd.Timedelta(days=4 * k)).strftime("%Y-%m-%d")
        rows.append({"activity_id": 300 + k, "date": day, "name": "Run", "sport_type": "Run",
                     "is_this": 0, "same_route": 0, "distance_km": 7.4 * f, "moving_s": 2990 * f,
                     "avg_kmh": 8.95 / f ** 0.5, "gain_m": 72 * f, "avg_w": 219 * f ** 0.3, "avg_hr": 146 * f ** 0.2})
    rows.append({"activity_id": 400, "date": "2026-11-12", "name": "Run", "sport_type": "Run", "is_this": 1,
                 "same_route": 0, "distance_km": 7.55, "moving_s": 3011, "avg_kmh": 9.03, "gain_m": 79, "avg_w": 222,
                 "avg_hr": 147})
    pd.DataFrame(rows).to_csv(path, index=False)


def check_zone_gradient():
    """A hard effort all in Z4-5 is drawn in their colours from its lowest beat, not faded in from Z1."""
    from matplotlib.colors import to_rgb

    from loomcharts._style import ZONES
    from loomcharts.activity import _zone_gradient

    xs = np.linspace(160, 190, 31)
    cols = _zone_gradient(xs, [160, 163, 163, 163, 180, 191], {4, 5})
    if not np.allclose(cols[0], to_rgb(ZONES[3])) or not np.allclose(cols[-1], to_rgb(ZONES[4])):
        print(f"FAIL zone gradient: starts {cols[0]}, ends {cols[-1]}")
        return ["zone_gradient"]
    print("ok   zone_gradient")
    return []


def check_spider_scale():
    """The load profile's axis scale: spread, not the ratio to the average."""
    from loomcharts.activity import SPIDER_IN, SPIDER_OUT, _spider_scale

    window = [7.2, 7.4, 7.5, 7.6, 7.7]
    low = _spider_scale(window, 6.0)
    flat = _spider_scale([100.0, 100.0], 105.0)
    cases = [
        ("window maximum on the outer ring", low(7.7), SPIDER_OUT),
        ("lowest on the inner ring", low(6.0), SPIDER_IN),
        # 5 % above the average is a quarter of the minimum span (20 %) out from the middle.
        ("5 % off a flat window", flat(105.0) - flat(100.0), (SPIDER_OUT - SPIDER_IN) * 0.25),
    ]
    failures = []
    for name, got, want in cases:
        if not math.isclose(got, want, abs_tol=1e-9):
            failures.append("spider_scale")
            print(f"FAIL spider scale, {name}: {got}, want {want}")
    avg = float(np.mean(window))
    if math.isclose(_spider_scale(window, 7.9)(avg), 0.6, abs_tol=0.02):
        failures.append("spider_scale")
        print("FAIL spider scale: the average sits on a fixed ring")
    if not failures:
        print("ok   spider_scale")
    return failures


def flag(v):
    """A yes/no cell as the sidecar writes it (boolCell)."""
    return "true" if v else "false"


def check_flags(baseline):
    """A model's own code filters the baseline with df[~df["is_this"]]: on 0/1 ints ~ is a bitwise
    not, which pandas took as column names (a KeyError in a live analysis). The sidecar writes
    true/false (its Go tests pin that); this pins that pandas reads that form as bool and that
    the filter then works."""
    df = pd.read_csv(baseline)
    if df["is_this"].dtype != bool or len(df[~df["is_this"]]) != len(df) - 1:
        print(f"FAIL flags: is_this read as {df['is_this'].dtype}")
        return ["flags"]
    print("ok   flags")
    return []


def check_input_path(d):
    """A bare name is looked up in in/ only when the working directory lacks it; a name
    with a directory never is; a missing one is reported as in/<name>."""
    from loomcharts._style import input_path

    root = os.path.join(os.path.abspath(d), "inputs")
    os.makedirs(os.path.join(root, "in", "sub"), exist_ok=True)
    for rel in ("both.csv", os.path.join("in", "both.csv"), os.path.join("in", "only_in.csv"),
                os.path.join("in", "sub", "x.csv")):
        with open(os.path.join(root, rel), "w") as f:
            f.write("a\n1\n")
    want = {"both.csv": "both.csv", "only_in.csv": os.path.join("in", "only_in.csv"),
            os.path.join("sub", "x.csv"): os.path.join("sub", "x.csv"),
            "missing.csv": os.path.join("in", "missing.csv"), "": "", ".": "."}
    failures = []
    with contextlib.chdir(root):
        for src, path in want.items():
            if (got := input_path(src)) != path:
                failures.append("input_path")
                print(f"FAIL input_path({src!r}) = {got!r}, want {path!r}")
    if not failures:
        print("ok   input_path")
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
    want = mpl.RcParams(_style.rc_style())
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
    """Collects matplotlib's log warnings (findfont): in a job they land in the tool output."""

    def __init__(self):
        super().__init__(logging.WARNING)
        self.messages = []

    def emit(self, record):
        self.messages.append(record.getMessage())


def main(argv):
    keep = None
    failures = []
    if "--out" in argv:
        keep = argv[argv.index("--out") + 1]
        os.makedirs(keep, exist_ok=True)
    # Only with --rc (the image): elsewhere the font is missing and every chart
    # warns about it, which a local preview should show, not collect.
    rc = argv[argv.index("--rc") + 1] if "--rc" in argv else None
    logged = _Warnings()
    # Glyph warnings come through warnings.warn, findfont's through logging.
    with warnings.catch_warnings(record=True) if rc else contextlib.nullcontext() as raised:
        if rc:
            warnings.simplefilter("always")
            logging.getLogger("matplotlib").addHandler(logged)
            failures += check_rc(rc)
        if keep:
            failures += run(keep)
        else:
            with tempfile.TemporaryDirectory(prefix="loomcharts-") as d:
                failures += run(d)
    # A UserWarning (missing glyph) reaches a job's output; deprecations stay hidden
    # there by Python's default filters, so they are shown here but fail nothing.
    messages = list(logged.messages)
    for w in raised or []:
        if issubclass(w.category, UserWarning):
            messages.append(str(w.message))
        else:
            print(f"note {w.category.__name__}: {w.message}")
    if messages:
        failures.append("warnings")
        for m in dict.fromkeys(messages):
            print(f"FAIL matplotlib warned: {m}")
    if failures:
        print(f"loomcharts selftest: {len(failures)} failed: {', '.join(failures)}")
        return 1
    print("loomcharts selftest: all charts ok")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
