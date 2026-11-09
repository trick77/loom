"""Going out: a day's hours, today against tomorrow, the week ahead (the outdoor_windows tool's CSVs)."""

import math

import numpy as np
import pandas as pd

from ._style import (BEST, CONTEXT, INK2, MEDIAN, MUTED, THIS, empty_chart, fmt_date, fmt_day, frame, header,
                     new_figure, num, save, styled)

SUN = "#c98500"
WIND = "#9085e9"
STRONG = "#e66767"
NIGHT_ALPHA = 0.55
COMPASS = {"N": 0, "NE": 45, "E": 90, "SE": 135, "S": 180, "SW": 225, "W": 270, "NW": 315}
NOUNS = {"run": "run", "ride": "ride"}


def _limits(limits):
    """The activity's wind limits as a dict (km/h); empty when unknown."""
    if limits is None:
        return {}
    try:
        df = frame(limits)
    except (OSError, ValueError):
        return {}
    if df.empty:
        return {}
    row = df.iloc[0]
    out = {"activity": str(row.get("activity", "") or "")}
    if isinstance(row.get("date"), str) and row.get("date"):
        out["date"] = row.get("date")
    for k in ("minutes", "wind_kmh", "wind_strong_kmh", "gust_kmh", "gust_strong_kmh"):
        v = pd.to_numeric(row.get(k), errors="coerce")
        if v is not None and not (isinstance(v, float) and math.isnan(v)):
            out[k] = float(v)
    return out


def _optional(src):
    """A frame from src, or None when it is missing or unreadable."""
    if src is None:
        return None
    try:
        return frame(src)
    except (OSError, ValueError):
        return None


def _hours(hours):
    df = frame(hours)
    t = pd.to_datetime(df["time"], errors="coerce") if "time" in df.columns else pd.Series([], dtype="datetime64[ns]")
    df = df.assign(_t=t).dropna(subset=["_t"])
    # An hour's values are the hour's: drawn at its middle, so the clock-time
    # windows and the night line up with them.
    return df.assign(_date=df["_t"].dt.strftime("%Y-%m-%d"), _h=df["_t"].dt.hour + df["_t"].dt.minute / 60 + 0.5)


def _around_daylight(day):
    """The day's hours from two before its first daylight hour to two after its last."""
    lit = num(day, "daylight")
    if not (lit == 1).any():
        return day
    hs = day["_h"][lit == 1]
    return day[(day["_h"] >= hs.min() - 2) & (day["_h"] <= hs.max() + 2)]


def _clock(s):
    """"15:30" as hours from midnight; NaN when it is not one ("00:00" after a start is 24)."""
    try:
        h, m = str(s).split(":")
        return int(h) + int(m) / 60
    except (ValueError, AttributeError):
        return float("nan")


def _arrows(ax, x, deg, y, every=1):
    """Where the wind blows to, an arrow every so many hours at height y (axes fraction)."""
    ok = ~np.isnan(deg)
    ok[np.arange(len(ok)) % every != 0] = False
    if not ok.any():
        return
    rad = np.radians(deg[ok])
    ax.quiver(x[ok], np.full(ok.sum(), y), -np.sin(rad), -np.cos(rad), transform=ax.get_xaxis_transform(),
              color=INK2, scale=34, width=0.0035, headwidth=4, headlength=4.5, pivot="middle", zorder=5)


def _limit_line(ax, y, color, text):
    ax.axhline(y, color=color, lw=0.9, ls=(0, (4, 3)), zorder=1)
    ax.annotate(text, (1, y), xycoords=("axes fraction", "data"), xytext=(-2, 2), textcoords="offset points",
                ha="right", va="bottom", fontsize=7.5, color=color)


def _panels(axes, df, windows, lim, show_labels, right_labels=True, every=1):
    """Temperature and sun, rain, wind: one day's hours on the three axes, windows banded, night shaded.

    show_labels: the left axis labels and the window numbers; right_labels: the rain
    chance's scale on the right; every: an arrow every so many hours. Returns the rain and
    wind axes' tops, so columns sharing them can take the highest.
    """
    t_ax, r_ax, w_ax = axes
    df = _around_daylight(df)
    x = df["_h"].to_numpy()
    lit = num(df, "daylight").to_numpy()
    for ax in axes:
        ax.set_xlim(max(0, x.min() - 0.5) if len(x) else 0, min(24, x.max() + 0.5) if len(x) else 24)
        for xi, d in zip(x, lit):
            if d == 0:
                ax.axvspan(xi - 0.5, xi + 0.5, color=CONTEXT, alpha=NIGHT_ALPHA, lw=0, zorder=0)
    if windows is not None:
        for _, w in windows.iterrows():
            a, b = _clock(w.get("from")), _clock(w.get("to"))
            if math.isnan(a) or math.isnan(b):
                continue
            b = 24 if b <= a else b
            best = str(w.get("rank")) == "1"
            for ax in axes:
                ax.axvspan(a, b, color=MEDIAN, alpha=0.32 if best else 0.14, lw=0, zorder=0.5)
            if show_labels:
                t_ax.annotate(str(w.get("rank", "")), ((a + b) / 2, 1), xycoords=("data", "axes fraction"),
                              xytext=(0, 2), textcoords="offset points", ha="center", va="bottom", fontsize=8,
                              color=MEDIAN, fontweight="bold" if best else "normal")

    temp, feels = num(df, "temp_c").to_numpy(), num(df, "feels_c").to_numpy()
    sun_ax = t_ax.twinx()
    sun_ax.bar(x, num(df, "sun_min").fillna(0).to_numpy(), width=0.8, color=SUN, alpha=0.35, lw=0, zorder=1)
    sun_ax.set_ylim(0, 60 * 2.2)
    sun_ax.set_axis_off()
    t_ax.set_zorder(sun_ax.get_zorder() + 1)
    t_ax.patch.set_visible(False)
    t_ax.plot(x, temp, color=THIS, lw=1.8, zorder=3, label="temperature")
    if not np.isnan(feels).all():
        t_ax.plot(x, feels, color=THIS, lw=1.1, ls=(0, (3, 2)), alpha=0.8, zorder=3, label="feels like")
    if show_labels:
        t_ax.set_ylabel("°C")

    rain, chance = num(df, "rain_mm").fillna(0).to_numpy(), num(df, "rain_pct").to_numpy()
    r_ax.bar(x, rain, width=0.8, color=BEST, lw=0, zorder=2)
    rain_top = max(2.0, float(np.nanmax(rain)) * 1.25 if len(rain) else 2.0)
    r_ax.set_ylim(0, rain_top)
    if not np.isnan(chance).all():
        c_ax = r_ax.twinx()
        c_ax.plot(x, chance, color=MUTED, lw=1.0, zorder=3)
        c_ax.set_ylim(0, 100)
        c_ax.set_yticks([0, 50, 100])
        c_ax.tick_params(colors=MUTED, labelsize=7.5, length=0)
        c_ax.grid(False)
        for side in c_ax.spines.values():
            side.set_visible(False)
        if right_labels:
            c_ax.set_ylabel("chance %", color=MUTED, fontsize=8)
        else:
            c_ax.set_yticklabels([])
    if show_labels:
        r_ax.set_ylabel("rain mm")

    wind, gusts = num(df, "wind_kmh").to_numpy(), num(df, "gust_kmh").to_numpy()
    w_ax.plot(x, wind, color=WIND, lw=1.8, zorder=3)
    if not np.isnan(gusts).all():
        w_ax.plot(x, gusts, color=WIND, lw=1.0, ls=(0, (1, 1.6)), zorder=3)
    top = np.nanmax(np.concatenate([wind, gusts, [10.0]]))
    if "wind_kmh" in lim:
        _limit_line(w_ax, lim["wind_kmh"], SUN, f"windy {lim['wind_kmh']:g}" if show_labels else "")
    if "wind_strong_kmh" in lim and top >= lim.get("wind_kmh", 0):
        _limit_line(w_ax, lim["wind_strong_kmh"], STRONG, f"strong {lim['wind_strong_kmh']:g}" if show_labels else "")
        top = max(top, lim["wind_strong_kmh"])
    wind_top = top * 1.3
    w_ax.set_ylim(0, wind_top)
    _arrows(w_ax, x, num(df, "wind_dir_deg").to_numpy(), 0.9, every)
    if show_labels:
        w_ax.set_ylabel("wind km/h")
    ticks = [h for h in range(0, 25, 3) if (len(x) and x.min() - 0.5 <= h <= x.max() + 0.5)]
    w_ax.set_xticks(ticks)
    w_ax.set_xticklabels([f"{h:02d}" for h in ticks])
    for ax in (t_ax, r_ax):
        ax.tick_params(axis="x", labelbottom=False, length=0)
    return rain_top, wind_top


def _subtitle(lim):
    noun = NOUNS.get(lim.get("activity", ""), "")
    parts = ["windows in green (1 = best)", "night shaded", "sun as gold bars", "gusts dotted", "arrows: wind blows to"]
    if noun and "minutes" in lim:
        parts.insert(0, f"{lim['minutes']:g} min {noun}")
    return " · ".join(parts)


@styled
def outdoor_day(hours, out, windows=None, limits=None, date=None, title=None):
    """One day hour by hour: temperature and sun, rain, wind against the activity's limits.

    hours: in/outdoor_hours.csv; windows: in/outdoor_windows.csv (the recommended windows,
    banded); limits: in/outdoor_limits.csv (wind lines). date: YYYY-MM-DD, default the day the
    tool was asked about (limits' date), else the first day with hours (today).
    """
    df = _hours(hours)
    lim = _limits(limits)
    wins = _optional(windows)
    if date is None:
        date = lim.get("date") or (df["_date"].iloc[0] if len(df) else None)
    day = df[df["_date"] == str(date)] if len(df) else df
    title = title or (f"When to go out · {fmt_date(date)}" if date else "When to go out")
    if day.empty:
        return empty_chart(out, title, "No hours to show for this day.")
    if wins is not None and "date" in wins.columns:
        wins = wins[wins["date"].astype(str) == str(date)]
    fig = new_figure(6.2)
    axes = fig.subplots(3, 1, sharex=True, gridspec_kw={"height_ratios": [1.25, 0.8, 1.0], "hspace": 0.12})
    _panels(axes, day, wins, lim, True)
    header(axes[0], title, _subtitle(lim), pad=10)
    return save(fig, out)


@styled
def outdoor_compare(hours, out, windows=None, limits=None, title=None):
    """The next two days with daylight left side by side on shared scales: which is better.

    hours: in/outdoor_hours.csv; windows, limits as for outdoor_day. Late in the evening
    today has no daylight left and the two days are tomorrow and the day after.
    """
    df = _hours(hours)
    lim = _limits(limits)
    wins = _optional(windows)
    lit = num(df, "daylight") == 1 if len(df) else pd.Series([], dtype=bool)
    days = list(dict.fromkeys(df["_date"][lit])) if lit.any() else list(dict.fromkeys(df["_date"]))
    dates = days[:2]
    if not dates:
        return empty_chart(out, title or "Two days", "No hours to show.")
    title = title or " against ".join(fmt_day(d) for d in dates)
    fig = new_figure(6.2)
    axes = fig.subplots(3, len(dates), sharey="row", squeeze=False,
                        gridspec_kw={"height_ratios": [1.25, 0.8, 1.0], "hspace": 0.12, "wspace": 0.06})
    tops = []
    for i, date in enumerate(dates):
        w = wins[wins["date"].astype(str) == date] if wins is not None and "date" in wins.columns else None
        tops.append(_panels(axes[:, i], df[df["_date"] == date], w, lim, i == 0, right_labels=i == len(dates) - 1,
                            every=2))
        axes[0, i].set_title(fmt_day(date), fontsize=10, color=INK2, loc="left", pad=14)
    # The rows share their scales: the higher day's, or one day's rain or gusts are cut off.
    axes[1, 0].set_ylim(0, max(t[0] for t in tops))
    axes[2, 0].set_ylim(0, max(t[1] for t in tops))
    header(axes[0, 0], title, _subtitle(lim), pad=24)
    return save(fig, out)


@styled
def outdoor_week(days, out, limits=None, title=None):
    """The week ahead, a column a day: high and low, sun hours, rain, the day's strongest wind.

    days: in/outdoor_days.csv (rank 1 = best day to go out, marked); limits:
    in/outdoor_limits.csv (wind lines). Wind is the day's strongest, night included.
    """
    df = frame(days)
    lim = _limits(limits)
    title = title or "The week for going out"
    if df.empty or "date" not in df.columns:
        return empty_chart(out, title, "No days to show.")
    n = len(df)
    x = np.arange(n)
    high, low = num(df, "high_c").to_numpy(), num(df, "low_c").to_numpy()
    rank = num(df, "rank").to_numpy()
    fig = new_figure(5.8)
    t_ax, r_ax, w_ax = fig.subplots(3, 1, sharex=True, gridspec_kw={"height_ratios": [1.2, 0.8, 1.0],
                                                                    "hspace": 0.14})
    best = rank == 1
    for ax in (t_ax, r_ax, w_ax):
        for xi in x[best]:
            ax.axvspan(xi - 0.45, xi + 0.45, color=MEDIAN, alpha=0.2, lw=0, zorder=0)
    t_ax.vlines(x, low, high, color=THIS, lw=6, zorder=3)
    for xi, s in zip(x, num(df, "sun_h").to_numpy()):
        if not math.isnan(s):
            t_ax.annotate(f"{s:.0f} h sun", (xi, 0), xycoords=("data", "axes fraction"), xytext=(0, 4),
                          textcoords="offset points", ha="center", va="bottom", fontsize=8, color=SUN)
    for xi, h, lo in zip(x, high, low):
        if not math.isnan(h):
            t_ax.annotate(f"{h:.0f}°", (xi, h), xytext=(0, 4), textcoords="offset points", ha="center", fontsize=8,
                          color=INK2)
        if not math.isnan(lo):
            t_ax.annotate(f"{lo:.0f}°", (xi, lo), xytext=(0, -11), textcoords="offset points", ha="center",
                          fontsize=8, color=MUTED)
    temps = np.concatenate([high, low])
    if not np.isnan(temps).all():
        lo_t, hi_t = np.nanmin(temps), np.nanmax(temps)
        span = max(hi_t - lo_t, 5.0)
        t_ax.set_ylim(lo_t - span * 0.6, hi_t + span * 0.35)
    t_ax.set_ylabel("°C")
    for xi in x[best]:
        t_ax.annotate("best", (xi, 1), xycoords=("data", "axes fraction"), xytext=(0, 2), textcoords="offset points",
                      ha="center", va="bottom", fontsize=8, color=MEDIAN, fontweight="bold")

    rain = num(df, "rain_mm").fillna(0).to_numpy()
    r_ax.bar(x, rain, width=0.55, color=BEST, lw=0)
    r_ax.set_ylim(0, max(4.0, float(rain.max()) * 1.3))
    r_ax.set_ylabel("rain mm")
    if "rain" in df.columns:
        for xi, level in zip(x, df["rain"].astype(str)):
            if level in ("showers", "wet"):
                r_ax.annotate(level, (xi, 1), xycoords=("data", "axes fraction"), xytext=(0, -10),
                              textcoords="offset points", ha="center", fontsize=7.5, color=BEST)

    wind, gusts = num(df, "wind_max_kmh").to_numpy(), num(df, "gust_max_kmh").to_numpy()
    levels = df["wind"].fillna("").astype(str).to_numpy() if "wind" in df.columns else np.array([""] * n)
    colors = [STRONG if lv == "strong" else SUN if lv == "windy" else WIND for lv in levels]
    w_ax.bar(x, np.nan_to_num(wind), width=0.55, color=colors, lw=0, zorder=2)
    if not np.isnan(gusts).all():
        w_ax.scatter(x, gusts, marker="_", s=180, color=colors, zorder=3, linewidths=1.6)
    top = np.nanmax(np.concatenate([wind, gusts, [10.0]]))
    if "wind_kmh" in lim:
        _limit_line(w_ax, lim["wind_kmh"], SUN, f"windy {lim['wind_kmh']:g}")
    if "wind_strong_kmh" in lim and top >= lim.get("wind_kmh", 0):
        _limit_line(w_ax, lim["wind_strong_kmh"], STRONG, f"strong {lim['wind_strong_kmh']:g}")
        top = max(top, lim["wind_strong_kmh"])
    w_ax.set_ylim(0, top * 1.35)
    if "wind_from" in df.columns:
        deg = np.array([COMPASS.get(str(v), np.nan) for v in df["wind_from"]], dtype=float)
        _arrows(w_ax, x.astype(float), deg, 0.9)
    w_ax.set_ylabel("wind km/h")
    w_ax.set_xticks(x)
    w_ax.set_xticklabels([f"{pd.Timestamp(d):%a} {pd.Timestamp(d).day}" if pd.notna(pd.to_datetime(d, errors="coerce"))
                          else str(d) for d in df["date"]])
    w_ax.set_xlim(-0.6, n - 0.4)
    for ax in (t_ax, r_ax):
        ax.tick_params(axis="x", labelbottom=False, length=0)
    noun = NOUNS.get(lim.get("activity", ""), "")
    sub = ["best day in green", "wind bars: day's strongest, gusts as ticks", "arrows: wind blows to"]
    if noun:
        sub.insert(0, f"for a {noun}")
    header(t_ax, title, " · ".join(sub), pad=10)
    return save(fig, out)
