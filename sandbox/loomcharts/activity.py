"""Charts every activity analysis draws: load profile, fitness and fatigue, heart-rate profile, split variation.

Each takes lang= ("de", "en"; anything else draws English): the labels follow the answer's language.
German labels are Swiss: ss, never the sharp s.
"""

import math

import numpy as np
import pandas as pd
from matplotlib.lines import Line2D
from matplotlib.patches import Patch

from ._style import (AXIS, BEST, BG, CONTEXT, GRID, INK, INK2, MEDIAN, MUTED, THIS, ZONES, empty_chart, fmt_day,
                     fmt_range, fmt_secs, frame, header, new_figure, num, save, sport_noun, styled)

LOW, HIGH, ANAEROBIC = ZONES[3], "#c98500", "#e66767"
SLOWER = "#c98500"
LINE = "#9085e9"

TEXT = {
    "en": {
        "load_profile": "Load profile", "fitness_fatigue": "Fitness and fatigue", "hr_profile": "Heart-rate profile",
        "split_variation": "Split variation",
        "pace": "Avg pace", "speed": "Avg speed", "power": "Avg power", "hr": "Avg HR", "gain": "Elevation gain",
        "distance": "Distance", "duration": "Duration",
        "this": "This {one}", "avg_weeks": "Average, last {weeks} weeks", "avg": "Average of earlier {many}",
        "lp_sub": "against the average of {n} earlier {many}{when}; dashed: the average",
        "lp_empty": "Not enough to compare: fewer than three measures, or no earlier {many}.",
        "ctl": "Fitness (CTL)", "atl": "Fatigue (ATL)", "form": "Form", "before": "before", "after": "after",
        "ff_sub": "42-day and 7-day weighted daily load (TSS, hrTSS) · form = fitness − fatigue",
        "ff_empty": "No daily load to show.",
        "acwr_high": "ACWR {v}: load rose fast", "acwr_low": "ACWR {v}: less load than usual", "acwr": "ACWR {v}",
        "low": "Low aerobic", "high": "High aerobic", "anaerobic": "Anaerobic", "focus": "Load focus",
        "steady": "Steady", "varied": "Varied", "hr_axis": "Heart rate (bpm)", "median": "median", "peak": "peak",
        "hr_sub": "time in zones while moving · dashed: a normal curve with the same mean and spread",
        "hr_empty": "No heart rate recorded.",
        "faster": "Faster", "on_pace": "On pace (±{band:g} %)", "slower": "Slower", "best": "best",
        "sv_sub": "{unit} splits against their median · line: {metric}",
        "sv_empty": "Too short for splits, or no distance recorded.",
        "watts": "power (W)", "bpm": "heart rate (bpm)", "split": "split",
    },
    "de": {
        "load_profile": "Belastungsprofil", "fitness_fatigue": "Fitness & Ermüdung", "hr_profile": "Herzfrequenzprofil",
        "split_variation": "Zwischenzeiten-Variation",
        "pace": "Ø Tempo", "speed": "Ø Geschwindigkeit", "power": "Ø Leistung", "hr": "Ø HF", "gain": "Höhenmeter",
        "distance": "Strecke", "duration": "Dauer",
        "this": "Diese Einheit", "avg_weeks": "Ø letzte {weeks} Wochen", "avg": "Ø frühere Einheiten",
        "lp_sub": "gegen den Durchschnitt von {n} früheren Einheiten{when}; gestrichelt: der Durchschnitt",
        "lp_empty": "Zu wenig zum Vergleichen: weniger als drei Werte oder keine früheren Einheiten.",
        "ctl": "Fitness (CTL)", "atl": "Ermüdung (ATL)", "form": "Form", "before": "vorher", "after": "nachher",
        "ff_sub": "42- und 7-Tage-gewichtete Tagesbelastung (TSS, hrTSS) · Form = Fitness − Ermüdung",
        "ff_empty": "Keine Tagesbelastung vorhanden.",
        "acwr_high": "ACWR {v}: Belastung stark gestiegen", "acwr_low": "ACWR {v}: weniger Belastung als üblich",
        "acwr": "ACWR {v}",
        "low": "Niedrige aerobe", "high": "Hohe aerobe", "anaerobic": "Anaerobe", "focus": "Belastungsfokus",
        "steady": "Gleichmässig", "varied": "Wechselnd", "hr_axis": "Herzfrequenz (bpm)", "median": "Median",
        "peak": "Spitze",
        "hr_sub": "Zeit in den Zonen in Bewegung · gestrichelt: Normalverteilung mit gleichem Mittel und gleicher Streuung",
        "hr_empty": "Keine Herzfrequenz aufgezeichnet.",
        "faster": "Schneller", "on_pace": "Im Tempo (±{band:g} %)", "slower": "Langsamer", "best": "Beste",
        "sv_sub": "{unit}-Abschnitte gegen ihren Median · Linie: {metric}",
        "sv_empty": "Zu kurz für Abschnitte, oder keine Distanz aufgezeichnet.",
        "watts": "Leistung (W)", "bpm": "Herzfrequenz (bpm)", "split": "Abschnitt",
    },
}


def _t(lang):
    return TEXT.get(lang if isinstance(lang, str) else "en", TEXT["en"])


MONTHS_DE = ("Jan", "Feb", "Mär", "Apr", "Mai", "Jun", "Jul", "Aug", "Sep", "Okt", "Nov", "Dez")


def _day(d, lang):
    """1 Nov, or 1. Nov in German."""
    t = pd.Timestamp(d)
    if lang == "de":
        return f"{t.day}. {MONTHS_DE[t.month - 1]}"
    return fmt_day(t)


def _range(lo, hi, lang):
    if lang != "de":
        return fmt_range(lo, hi)
    lo, hi = pd.Timestamp(lo), pd.Timestamp(hi)
    if lo.normalize() == hi.normalize():
        return f"{_day(hi, lang)} {hi.year}"
    first = _day(lo, lang) if lo.year == hi.year else f"{_day(lo, lang)} {lo.year}"
    return f"{first} – {_day(hi, lang)} {hi.year}"


def _kind(sport):
    """pace (per km), swim (per 100 m) or speed (km/h) for a Strava sport_type."""
    s = str(sport or "")
    if s == "Swim":
        return "swim"
    if s.endswith("Run") or s in ("Walk", "Hike"):
        return "pace"
    return "speed"


def _tempo(kmh, kind):
    if not (isinstance(kmh, (int, float)) and math.isfinite(kmh)) or kmh <= 0:
        return "–"
    if kind == "swim":
        return f"{fmt_secs(360 / kmh)} /100m"
    if kind == "pace":
        return f"{fmt_secs(3600 / kmh)} /km"
    return f"{kmh:.1f} km/h"


@styled
def load_profile(baseline, out, title=None, weeks=None, lang="en"):
    """This activity against the average of its window, as a spider: one axis per measure.

    baseline: in/strava_<id>_baseline.csv. Axes: speed or pace (faster outward), power, heart
    rate, elevation gain, distance, duration; the window's average sits on the dashed ring
    (60 % of the radius), this activity at value / average of it. An axis without a value for
    this activity or the window is left out; under three axes a notice is drawn instead.
    """
    tx = _t(lang)
    title = title or tx["load_profile"]
    df = frame(baseline)
    flag = num(df, "is_this").fillna(0) == 1
    sports = list(df["sport_type"]) if "sport_type" in df.columns else []
    one, many = sport_noun(sports)
    if not flag.any():
        return empty_chart(out, title, tx["lp_empty"].format(many=many))
    this, base = df[flag].iloc[0], df[~flag]
    kind = _kind(this.get("sport_type"))
    axes = [
        ("avg_kmh", tx["speed"] if kind == "speed" else tx["pace"], lambda v: _tempo(v, kind)),
        ("avg_w", tx["power"], lambda v: f"{v:.0f} W"),
        ("avg_hr", tx["hr"], lambda v: f"{v:.0f} bpm"),
        ("gain_m", tx["gain"], lambda v: f"{v:,.0f} m"),
        ("distance_km", tx["distance"], lambda v: f"{v:.1f} km" if kind != "swim" else f"{v * 1000:,.0f} m"),
        ("moving_s", tx["duration"], fmt_secs),
    ]
    rows = []
    for key, label, fmt in axes:
        tv = float(pd.to_numeric(pd.Series([this.get(key)]), errors="coerce").iloc[0])
        bv = num(base, key).dropna()
        avg = float(bv.mean()) if len(bv) else math.nan
        if not (math.isfinite(tv) and math.isfinite(avg)) or avg <= 0 or tv < 0:
            continue
        rows.append((label, fmt(tv), min(1.0, 0.6 * tv / avg)))
    if len(rows) < 3:
        return empty_chart(out, title, tx["lp_empty"].format(many=many))

    fig = new_figure(5.6, 7.2)
    ax = fig.add_subplot(projection="polar")
    n = len(rows)
    ang = np.linspace(0, 2 * np.pi, n, endpoint=False)
    ax.set_theta_offset(np.pi / 2)
    ax.set_theta_direction(-1)
    ax.set_ylim(0, 1.0)
    ax.set_yticks([])
    ax.set_xticks(ang)
    ax.set_xticklabels([f"{label}\n{val}" for label, val, _ in rows], fontsize=9, color=INK2)
    ax.tick_params(axis="x", pad=14)
    ax.grid(False)
    ax.spines["polar"].set_visible(False)
    closed = np.append(ang, ang[0])
    for ring in (0.3, 0.6, 0.9):
        ax.plot(closed, [ring] * (n + 1), color=GRID if ring != 0.6 else AXIS, lw=1, zorder=1)
    for a in ang:
        ax.plot([a, a], [0, 1.0], color=GRID, lw=1, zorder=1)
    ax.plot(closed, [0.6] * (n + 1), color=INK2, lw=1.4, ls=(0, (4, 3)), zorder=3)
    r = np.array([v for _, _, v in rows])
    rc = np.append(r, r[0])
    ax.fill(closed, rc, color=THIS, alpha=0.28, zorder=2)
    ax.plot(closed, rc, color=THIS, lw=2, zorder=4)
    ax.scatter(ang, r, s=30, color=THIS, edgecolors=BG, linewidths=1.5, zorder=5)

    avg_label = tx["avg_weeks"].format(weeks=int(weeks)) if weeks else tx["avg"].format(many=many)
    handles = [Patch(facecolor=THIS, alpha=0.6, edgecolor=THIS, label=tx["this"].format(one=one)),
               Line2D([], [], color=INK2, lw=1.4, ls=(0, (4, 3)), label=avg_label)]
    fig.legend(handles=handles, loc="lower center", ncol=2, bbox_to_anchor=(0.5, 0.0))
    dates = pd.to_datetime(base["date"], errors="coerce").dropna() if "date" in base.columns else pd.Series([])
    when = f", {_range(dates.min(), dates.max(), lang)}" if len(dates) else ""
    many_word = many if len(base) != 1 else one
    sub = tx["lp_sub"].format(n=len(base), many=many_word, when=when)
    fig.subplots_adjust(top=0.80, bottom=0.14)
    header(ax, title, sub, pad=34)
    return save(fig, out)


@styled
def fitness_fatigue(load, out, title=None, lang="en"):
    """Fitness (CTL) and fatigue (ATL) before and after the activity, form, and their last 120 days.

    load: in/strava_<id>_load.csv (or strava_load.csv from list_activities: then the last
    day's values only, no before). The ACWR (ATL/CTL) is named when given.
    """
    tx = _t(lang)
    title = title or tx["fitness_fatigue"]
    df = frame(load)
    ctl, atl = num(df, "ctl"), num(df, "atl")
    if not len(df) or not ctl.notna().any():
        return empty_chart(out, title, tx["ff_empty"])
    dates = pd.to_datetime(df["date"], errors="coerce") if "date" in df.columns else pd.Series(range(len(df)))
    cb, ab = num(df, "ctl_before"), num(df, "atl_before")
    at = int(cb.last_valid_index()) if cb.notna().any() else len(df) - 1
    c1, a1 = float(ctl.iloc[at]), float(atl.iloc[at])
    c0, a0 = float(cb.iloc[at]), float(ab.iloc[at])
    has_before = math.isfinite(c0) and math.isfinite(a0)

    fig = new_figure(5.4)
    gs = fig.add_gridspec(2, 1, height_ratios=(1.15, 1.0), hspace=0.55)
    top, low = fig.add_subplot(gs[0]), fig.add_subplot(gs[1])

    # Bars: before and after, CTL and ATL.
    groups = [(tx["ctl"], c0, c1, BEST), (tx["atl"], a0, a1, THIS)]
    width = 0.34
    top.grid(False)
    for s in ("left", "bottom"):
        top.spines[s].set_visible(False)
    vmax = max(v for g in groups for v in g[1:3] if math.isfinite(v)) or 1.0
    for i, (name, b0, b1, col) in enumerate(groups):
        x = i * 1.6
        if has_before:
            top.bar(x - width / 2 - 0.02, b0, width, color=CONTEXT)
            top.text(x - width / 2 - 0.02, b0 + vmax * 0.04, f"{b0:.0f}", ha="center", va="bottom", fontsize=9,
                     color=MUTED)
            top.text(x - width / 2 - 0.02, -vmax * 0.06, tx["before"], ha="center", va="top", fontsize=8, color=MUTED)
            bx = x + width / 2 + 0.02
        else:
            bx = x
        top.bar(bx, b1, width, color=col)
        top.text(bx, b1 + vmax * 0.04, f"{b1:.0f}", ha="center", va="bottom", fontsize=11, fontweight="bold", color=INK)
        if has_before:
            top.text(bx, -vmax * 0.06, tx["after"], ha="center", va="top", fontsize=8, color=MUTED)
        top.text(x, vmax * 1.42, name, ha="center", va="bottom", fontsize=9.5, color=INK2)
    form0, form1 = c0 - a0, c1 - a1
    ftxt = f"{form1:+.0f}".replace("-", "−")
    if has_before:
        ftxt = f"{form0:+.0f} → {form1:+.0f}".replace("-", "−")
    top.text(3.4, vmax * 0.55, f"{tx['form']}\n{ftxt}", ha="center", va="center", fontsize=12, fontweight="bold",
             color=MEDIAN if form1 >= 0 else THIS, linespacing=1.4)
    top.set_xlim(-0.8, 4.2)
    top.set_ylim(-vmax * 0.2, vmax * 1.55)
    top.set_xticks([])
    top.set_yticks([])

    # Lines: the last 120 days.
    low.plot(dates, ctl, color=BEST, lw=2, label=tx["ctl"])
    low.plot(dates, atl, color=THIS, lw=1.5, label=tx["atl"])
    low.axvline(dates.iloc[at], color=AXIS, lw=1)
    low.set_xlim(dates.iloc[0], dates.iloc[-1])
    low.legend(loc="upper left", ncol=2)
    ticks = pd.date_range(dates.iloc[0], dates.iloc[-1], periods=5)
    low.set_xticks(ticks)
    low.set_xticklabels([_day(t, lang) for t in ticks])

    sub = tx["ff_sub"]
    acwr = num(df, "acwr").iloc[at]
    if math.isfinite(acwr):
        key = "acwr_high" if acwr > 1.5 else "acwr_low" if acwr < 0.8 else "acwr"
        sub += " · " + tx[key].format(v=f"{acwr:.2f}")
    header(top, title, sub, pad=10)
    return save(fig, out)


def _zone_edges(hr, zone):
    """Lower bounds of zones 2..5 seen in the data (NaN for a zone never reached)."""
    edges = []
    for z in (2, 3, 4, 5):
        v = hr[zone >= z]
        edges.append(float(v.min()) if len(v) else math.nan)
    return edges


@styled
def hr_profile(streams, out, title=None, lang="en"):
    """Heart rate while moving: the load focus (time in low aerobic, high aerobic, anaerobic) and
    the distribution of heart rate, filled by zone, with median, peak and a normal curve.

    streams: in/strava_<id>_streams.csv. Zones are the CSV's hr_zone (the sport's): Z1-2 low
    aerobic, Z3-4 high aerobic, Z5 anaerobic. "Steady" when the middle half spans 10 bpm or less.
    """
    tx = _t(lang)
    title = title or tx["hr_profile"]
    df = frame(streams)
    mv = num(df, "moving").fillna(1) == 1
    hr = num(df, "hr")[mv]
    zone = num(df, "hr_zone")[mv]
    ok = hr.notna() & (hr > 30)
    hr, zone = hr[ok].values, zone[ok].values
    if len(hr) < 12:
        return empty_chart(out, title, tx["hr_empty"])
    has_zones = np.isfinite(zone).sum() >= len(zone) * 0.9

    fig = new_figure(5.2 if has_zones else 3.8)
    if has_zones:
        gs = fig.add_gridspec(2, 1, height_ratios=(0.8, 2.2), hspace=0.45)
        bars, ax = fig.add_subplot(gs[0]), fig.add_subplot(gs[1])
        z = np.nan_to_num(zone, nan=1)
        shares = [((z <= 2).mean(), tx["low"], LOW), (((z >= 3) & (z <= 4)).mean(), tx["high"], HIGH),
                  ((z >= 5).mean(), tx["anaerobic"], ANAEROBIC)]
        bars.grid(False)
        for s in bars.spines.values():
            s.set_visible(False)
        for i, (share, name, col) in enumerate(reversed(shares)):
            bars.barh(i, 1.0, height=0.36, color=GRID)
            bars.barh(i, share, height=0.36, color=col)
            bars.text(-0.01, i, name, ha="right", va="center", fontsize=9, color=INK2)
            bars.text(1.01, i, f"{share * 100:.0f} %", ha="left", va="center", fontsize=9.5, color=INK)
        bars.set_xlim(0, 1)
        bars.set_ylim(-0.6, 2.6)
        bars.set_xticks([])
        bars.set_yticks([])
        fig.subplots_adjust(left=0.17, right=0.92)
    else:
        ax = fig.add_subplot()

    lo, hi = int(math.floor(hr.min())) - 3, int(math.ceil(hr.max())) + 3
    bins = np.arange(lo, hi + 1)
    counts, _ = np.histogram(hr, bins=np.append(bins, hi + 1))
    k = np.exp(-0.5 * (np.arange(-8, 9) / 2.5) ** 2)
    dens = np.convolve(counts, k / k.sum(), mode="same")
    dens = dens / dens.sum()
    edges = _zone_edges(hr, zone) if has_zones else []
    if has_zones:
        bounds = [lo] + [e for e in edges] + [hi + 1]
        for i in range(5):
            a, b = bounds[i], bounds[i + 1]
            if not (math.isfinite(a) and math.isfinite(b)) or b <= a:
                continue
            m = (bins >= a - 0.5) & (bins <= b + 0.5)
            ax.fill_between(bins[m], dens[m], color=ZONES[i], alpha=0.9, lw=0)
        for i, e in enumerate(edges):
            if math.isfinite(e) and lo < e < hi:
                ax.axvline(e, color=MUTED, lw=1, ls=(0, (2, 2)))
                ax.annotate(f"Z{i + 2}", (e, 1), xycoords=("data", "axes fraction"), xytext=(0, 4),
                            textcoords="offset points", ha="center", fontsize=8.5, color=MUTED)
    else:
        ax.fill_between(bins, dens, color=ZONES[2], alpha=0.9, lw=0)
    ax.plot(bins, dens, color=INK2, lw=1.2)
    mu, sd = float(np.mean(hr)), float(np.std(hr))
    if sd > 0:
        normal = np.exp(-0.5 * ((bins - mu) / sd) ** 2)
        ax.plot(bins, normal / normal.sum(), color=INK, lw=1, ls=(0, (4, 3)), alpha=0.7)
    med = float(np.median(hr))
    peak = float(bins[int(np.argmax(dens))])
    ax.set_ylim(0, float(dens.max()) * 1.18)
    ax.set_yticks([])
    ax.set_xlim(lo, hi)
    # No axis label: median and peak hang below the ticks, where it would sit.
    ax.annotate(tx["hr_axis"], (1, 0), xycoords="axes fraction", xytext=(0, -22), textcoords="offset points",
                ha="right", va="top", fontsize=8.5, color=MUTED)
    ax.grid(False)
    # Median and peak a few bpm apart would print over each other: one label then.
    close = abs(med - peak) < 4
    marks = [(med, f"{tx['median']} {med:.0f} · {tx['peak']} {peak:.0f} bpm" if close else f"{med:.0f} bpm\n{tx['median']}",
              INK2, "center")]
    if not close:
        left = med < peak
        marks = [(med, f"{med:.0f} bpm\n{tx['median']}", INK2, "right" if left else "left"),
                 (peak, f"{peak:.0f} bpm\n{tx['peak']}", THIS, "left" if left else "right")]
    for v, text, col, ha in marks:
        ax.annotate(text, (v, 0), xytext=(0, -26), textcoords="offset points", ha=ha, va="top", fontsize=8.5,
                    color=col, arrowprops={"arrowstyle": "-", "color": MUTED, "lw": 0.8})
    q1, q3 = np.percentile(hr, [25, 75])
    verdict = tx["steady"] if q3 - q1 <= 10 else tx["varied"]
    sub = f"{verdict} · {tx['hr_sub']}"
    header(bars if has_zones else ax, title, sub, pad=18 if has_zones else 4)
    if has_zones:
        bars.annotate(tx["focus"], (0, 1), xycoords="axes fraction", xytext=(0, 4), textcoords="offset points",
                      fontsize=9, color=MUTED, va="bottom")
    return save(fig, out)


def _split_len(total_km, kind):
    if kind == "swim":
        return 0.1
    for L in (1.0, 5.0, 10.0):
        if total_km / L <= 30:
            return L
    return 10.0


@styled
def split_variation(streams, out, metric="watts", sport=None, title=None, lang="en"):
    """Each split's pace against the median split: faster up, slower down, a band for on pace;
    a line for power (or heart rate) per split.

    streams: in/strava_<id>_streams.csv. sport: the Strava sport_type (pace per km for runs,
    per 100 m and 100 m splits for swims, km/h otherwise). Splits: 1 km, or 5 or 10 km when
    that keeps them to 30; a last part split counts from half a split. metric: watts or hr
    (falls back to hr without power).
    """
    tx = _t(lang)
    title = title or tx["split_variation"]
    df = frame(streams)
    kind = _kind(sport) if sport else "pace"
    d = num(df, "distance_km")
    mv = num(df, "moving").fillna(1) == 1
    spd = num(df, "speed_kmh")
    ok = d.notna() & mv & spd.notna()
    if ok.sum() < 10:
        return empty_chart(out, title, tx["sv_empty"])
    d0, d1 = float(d[ok].min()), float(d[ok].max())
    L = _split_len(d1 - d0, kind)
    n_full = int((d1 - d0) // L)
    rest = (d1 - d0) - n_full * L
    n = n_full + (1 if rest >= L / 2 else 0)
    if n < 2:
        return empty_chart(out, title, tx["sv_empty"])
    metric = metric if metric in ("watts", "hr") else "watts"
    mvals = num(df, metric)
    if metric == "watts" and not mvals[ok].notna().any():
        metric, mvals = "hr", num(df, "hr")
    speeds, line = [], []
    for k in range(n):
        m = ok & (d >= d0 + k * L) & (d < d0 + (k + 1) * L if k < n - 1 else d <= d1)
        speeds.append(float(spd[m].mean()) if m.any() else math.nan)
        line.append(float(mvals[m].mean()) if m.any() and mvals[m].notna().any() else math.nan)
    speeds = np.array(speeds)
    med = float(np.nanmedian(speeds))
    dev = (speeds - med) / med * 100
    band = 2.0

    fig = new_figure(4.4)
    ax = fig.add_subplot()
    x = np.arange(1, n + 1)
    cols = [BEST if v > band else SLOWER if v < -band else CONTEXT for v in dev]
    ax.axhspan(-band, band, facecolor=GRID, edgecolor=AXIS, hatch="///", zorder=0, lw=0)
    ax.bar(x, dev, width=0.55, color=cols, zorder=2)
    ax.axhline(0, color=AXIS, lw=1, zorder=1)
    lim = max(6.0, float(np.nanmax(np.abs(dev))) * 1.25)
    ax.set_ylim(-lim, lim)
    ax.set_xlim(0.4, n + 0.6)
    step = max(1, n // 15)
    ax.set_xticks(x[::step])
    unit = f"{L * 1000:.0f} m" if L < 1 else f"{L:g} km"
    ax.set_xlabel(f"{unit} {tx['split']}")
    ax.set_ylabel("%")
    if any(math.isfinite(v) for v in line):
        ax2 = ax.twinx()
        ax2.plot(x, line, color=LINE, lw=1.8, marker="o", ms=4, zorder=3)
        ax2.grid(False)
        ax2.spines["right"].set_visible(False)
        ax2.tick_params(axis="y", colors=LINE, labelcolor=LINE)
        vals = [v for v in line if math.isfinite(v)]
        pad = max(5.0, (max(vals) - min(vals)) * 0.4)
        ax2.set_ylim(min(vals) - pad, max(vals) + pad)
    handles = [Patch(color=BEST, label=tx["faster"]), Patch(color=CONTEXT, label=tx["on_pace"].format(band=band)),
               Patch(color=SLOWER, label=tx["slower"])]
    ax.legend(handles=handles, loc="lower center", bbox_to_anchor=(0.5, -0.36), ncol=3)
    best = float(np.nanmax(speeds))
    mname = tx["watts"] if metric == "watts" else tx["bpm"]
    sub = (f"{tx['median'].capitalize()} {_tempo(med, kind)} · {tx['best']} {_tempo(best, kind)} · "
           + tx["sv_sub"].format(unit=unit, metric=mname))
    fig.subplots_adjust(bottom=0.24)
    header(ax, title, sub)
    return save(fig, out)
