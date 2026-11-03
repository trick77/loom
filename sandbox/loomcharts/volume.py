"""Volume over time: weekly load and a bar summary of listed activities."""

import numpy as np
import pandas as pd
from matplotlib.transforms import blended_transform_factory

from ._style import (BEST, BG, INK2, MUTED, THIS, empty_chart, fmt_day, fmt_range, frame, header, new_figure, num,
                     overlap, renderer, save, sport_noun, styled)

# metric -> (title word, axis unit, scale, value format, aggregate)
METRICS = {
    "tss": ("load", "TSS", 1.0, "{:,.0f}", "sum"),
    "distance_km": ("distance", "km", 1.0, "{:,.0f}", "sum"),
    "moving_s": ("time", "h", 1 / 3600, "{:,.1f}", "sum"),
    "gain_m": ("elevation", "m", 1.0, "{:,.0f}", "sum"),
    "count": ("count", "", 1.0, "{:,.0f}", "sum"),
    "kj": ("work", "kJ", 1.0, "{:,.0f}", "sum"),
    "suffer_score": ("relative effort", "", 1.0, "{:,.0f}", "sum"),
    "avg_kmh": ("avg speed", "km/h", 1.0, "{:,.1f}", "mean"),
    "avg_hr": ("avg HR", "bpm", 1.0, "{:,.0f}", "mean"),
    "avg_w": ("avg power", "W", 1.0, "{:,.0f}", "mean"),
    "weighted_w": ("weighted power", "W", 1.0, "{:,.0f}", "mean"),
}
ALIASES = {"distance": "distance_km", "km": "distance_km", "time": "moving_s", "hours": "moving_s",
           "moving_time": "moving_s", "duration": "moving_s", "elevation": "gain_m", "gain": "gain_m",
           "climbing": "gain_m", "load": "tss", "activities": "count", "rides": "count", "speed": "avg_kmh",
           "hr": "avg_hr", "power": "avg_w", "np": "weighted_w", "effort": "suffer_score"}


def _metric(metric):
    m = str(metric).lower()
    return ALIASES.get(m, m)


def _sports(sport_type):
    if sport_type is None:
        return None
    if isinstance(sport_type, str):
        return {sport_type.lower()}
    return {str(s).lower() for s in sport_type}


def _bars(ax, values, colors, fmt, label_mask, width):
    x = np.arange(len(values))
    bars = ax.bar(x, values, width=width, color=colors, zorder=3)
    labels = []
    for xi, v, show in zip(x, values, label_mask):
        if show and not np.isnan(v):
            labels.append(ax.annotate(fmt.format(v), (xi, v), xytext=(0, 4), textcoords="offset points",
                                      ha="center", va="bottom", fontsize=8.5, color=INK2))
    return x, bars, labels


def _xlim(ax, n, slots=5):
    """Centre a few bars in room for slots, so one week is not one wide slab."""
    extra = max(0, slots - n) / 2
    ax.set_xlim(-0.6 - extra, n - 0.4 + extra)


def _thin_ticks(ax, x, labels, max_labels=16):
    step = max(1, int(np.ceil(len(x) / max_labels)))
    ax.set_xticks(x[::step])
    ax.set_xticklabels(labels[::step])


def _avg_line(fig, ax, value, text, x, bars, labels):
    """Average line with its label where it covers no value label and the fewest tall bars."""
    ax.axhline(value, color=MUTED, lw=1, zorder=4)
    r = renderer(fig)
    lab_boxes = [a.get_window_extent(r) for a in labels]
    tall = [b.get_window_extent(r) for b in bars if b.get_height() > value]
    candidates = [xi + 0.5 for xi in x[:-1]] + list(x)
    best, score = None, None
    for cx in candidates:
        t = ax.annotate(text, (cx, value), xytext=(0, 4), textcoords="offset points", ha="center", va="bottom",
                        fontsize=8.5, color=INK2, zorder=5,
                        bbox={"boxstyle": "square,pad=0.15", "facecolor": BG, "edgecolor": "none", "alpha": 0.85})
        b = t.get_window_extent(r)
        axb = ax.get_window_extent(r)
        s = (100 * sum(overlap(b, o) for o in lab_boxes) + 10 * (b.x0 < axb.x0 or b.x1 > axb.x1)
             + sum(overlap(b, o, pad=0) for o in tall))
        if score is None or s < score:
            if best is not None:
                best.remove()
            best, score = t, s
        else:
            t.remove()
        if s == 0:
            break


@styled
def weekly_load(weeks, out, metric="tss", sport_type=None, title=None):
    """Columns per week (latest highlighted) with the average of the earlier weeks as a line.

    weeks: strava_weeks.csv. metric: tss (default), moving_s, distance_km, gain_m or count;
    without any TSS (no power) it falls back to moving time. sport_type: one or a list to
    keep (default: all sports summed per week).
    """
    df = frame(weeks)
    want = _sports(sport_type)
    if want is not None and "sport_type" in df.columns:
        df = df[df["sport_type"].astype(str).str.lower().isin(want)]
    start = pd.to_datetime(df["week_start"], errors="coerce") if "week_start" in df.columns else pd.Series([])
    df = df.assign(_w=start.values if len(start) else pd.NaT).dropna(subset=["_w"])
    key = _metric(metric)
    if key not in METRICS:
        key = "tss"
    note = ""
    if key == "tss" and not (num(df, "tss").fillna(0) > 0).any() and (num(df, "moving_s").fillna(0) > 0).any():
        key, note = "moving_s", "no TSS (no power data)"
    word, unit, scale, fmt, _ = METRICS[key]
    head = {"tss": "Weekly load (TSS)", "count": "Activities per week"}.get(key, f"Weekly {word} ({unit})")
    title = title or head
    if df.empty or not (num(df, key).notna().any()):
        return empty_chart(out, title, "No weeks to show.")

    g = df.assign(_v=num(df, key), _n=num(df, "count")).groupby("_w").agg(v=("_v", "sum"), n=("_n", "sum"))
    full = pd.date_range(g.index.min(), g.index.max(), freq="7D")
    g = g.reindex(full, fill_value=0)
    vals = g["v"].values * scale
    n = len(vals)
    one, many = sport_noun(df["sport_type"] if "sport_type" in df.columns else [])

    fig = new_figure(3.9)
    ax = fig.subplots()
    colors = [BEST] * (n - 1) + [THIS]
    show = np.ones(n, dtype=bool) if n <= 12 else np.isin(np.arange(n), [n - 1, int(np.argmax(vals))])
    x, bars, labels = _bars(ax, vals, colors, fmt, show, 0.45 if n <= 8 else 0.62)
    wk = [f"W{d.isocalendar()[1]}" for d in g.index]
    _thin_ticks(ax, x, wk)
    ax.spines["bottom"].set_visible(False)
    _xlim(ax, n)
    top = float(np.nanmax(vals)) if n else 1.0
    ax.set_ylim(0, (top or 1.0) * 1.15)
    if unit and key != "tss":
        ax.set_ylabel(unit)
    if n <= 12:
        tr = blended_transform_factory(ax.transData, ax.transAxes)
        for xi, c in zip(x, g["n"].values):
            c = int(c)
            ax.text(xi, -0.035, f"{c} {one if c == 1 else many}", transform=tr, ha="center", va="top", fontsize=8,
                    color=MUTED)
        ax.tick_params(axis="x", length=0, pad=19)
    else:
        ax.tick_params(axis="x", length=0)
    if n >= 2:
        avg = float(np.mean(vals[:-1]))
        span = f"{wk[0]}–{wk[-2]}" if n > 2 else wk[0]
        _avg_line(fig, ax, avg, f"avg {span}: {fmt.format(avg)}", x, bars, labels)

    sub = ["latest week highlighted"]
    if n <= 12:
        sub.append(f"count of {many} under each week")
    if want is not None:
        sub.append(", ".join(sorted(str(s) for s in (sport_type if not isinstance(sport_type, str) else [sport_type]))))
    elif "sport_type" in df.columns and df["sport_type"].nunique() > 1:
        sub.append("all sports")
    if note:
        sub.append(note)
    header(ax, title, " · ".join(sub))
    return save(fig, out)


@styled
def activities_summary(activities, out, metric="distance_km", by="week", title=None):
    """Bars of a metric over listed activities, grouped by week, month, sport_type or activity.

    activities: strava_activities.csv. metric: distance_km (default), moving_s, gain_m, count,
    kj, suffer_score (summed) or avg_kmh, avg_hr, avg_w, weighted_w (averaged per group).
    """
    df = frame(activities)
    key = _metric(metric)
    if key not in METRICS:
        key = "distance_km"
    by = str(by).lower()
    if by not in ("week", "month", "sport_type", "activity"):
        by = "week"
    word, unit, scale, fmt, agg = METRICS[key]
    title = title or (f"{word[0].upper() + word[1:]} per {by.replace('_', ' ')}" if key != "count"
                      else f"Activities per {by.replace('_', ' ')}")
    date = pd.to_datetime(df["date"], errors="coerce") if "date" in df.columns else pd.Series(pd.NaT, df.index)
    try:
        date = date.dt.tz_localize(None)
    except (TypeError, AttributeError):
        pass
    v = pd.Series(1.0, index=df.index) if key == "count" else num(df, key)
    df = df.assign(_d=date, _v=v)
    if by != "sport_type":
        df = df.dropna(subset=["_d"])
    if df.empty or not df["_v"].notna().any():
        return empty_chart(out, title, "No activities with this metric to show.")
    one, many = sport_noun(df["sport_type"] if "sport_type" in df.columns else [])

    if by == "activity":
        df = df.dropna(subset=["_v"]).sort_values("_d")
        vals = df["_v"].values * scale
        names = [fmt_day(d) for d in df["_d"]]
    elif by == "sport_type":
        col = df["sport_type"].fillna("unknown") if "sport_type" in df.columns else pd.Series("all", df.index)
        g = df.assign(_s=col).groupby("_s")["_v"].agg(agg).sort_values(ascending=False)
        vals, names = g.values * scale, list(g.index)
    else:
        if by == "week":
            period = (df["_d"] - pd.to_timedelta(df["_d"].dt.day_of_week, unit="D")).dt.normalize()
            freq = "7D"
        else:
            period = df["_d"].dt.to_period("M").dt.to_timestamp()
            freq = "MS"
        g = df.assign(_p=period).groupby("_p")["_v"].agg(agg)
        full = pd.date_range(g.index.min(), g.index.max(), freq=freq)
        g = g.reindex(full, fill_value=0 if agg == "sum" else np.nan)
        vals = g.values * scale
        if by == "week":
            names = [f"W{d.isocalendar()[1]}" for d in g.index]
        else:
            years = {d.year for d in g.index}
            names = [f"{d:%b}" if len(years) == 1 else f"{d:%b %y}" for d in g.index]

    n = len(vals)
    fig = new_figure(3.8)
    ax = fig.subplots()
    show = np.ones(n, dtype=bool) if n <= 16 else np.isin(np.arange(n), [int(np.nanargmax(vals))])
    x, _, _ = _bars(ax, vals, [BEST] * n, fmt, show, 0.55 if n <= 10 else 0.7)
    _thin_ticks(ax, x, names, max_labels=12 if by == "activity" else 16)
    ax.spines["bottom"].set_visible(False)
    ax.tick_params(axis="x", length=0)
    _xlim(ax, n)
    top = float(np.nanmax(vals)) if n else 1.0
    ax.set_ylim(0, (top or 1.0) * 1.15)
    if unit:
        ax.set_ylabel(unit)
    dated = df["_d"].dropna()
    sub = [f"{len(df)} {one if len(df) == 1 else many}"]
    if len(dated):
        sub[0] += f", {fmt_range(dated.min(), dated.max())}"
    if agg == "mean" and by != "activity":
        sub.append(f"mean per {by.replace('_', ' ')}")
    header(ax, title, " · ".join(sub))
    return save(fig, out)
