"""Aerobic efficiency over the months: the easy activities, their monthly median."""

import pandas as pd
from matplotlib.lines import Line2D

from ._style import BEST, BG, CONTEXT, THIS, empty_chart, frame, header, new_figure, num, save, styled
from .activity import _day

TEXT = {
    "en": {"title": "Aerobic efficiency", "empty": "No activity with heart rate to show.",
           "running": "speed per heart beat (m/min per bpm, climb counted as distance)",
           "cycling": "power per heart beat (W per bpm)",
           "easy": "easy", "hard": "harder", "median": "monthly median, easy only", "up": "higher = fitter"},
    "de": {"title": "Aerobe Effizienz", "empty": "Keine Aktivität mit Herzfrequenz vorhanden.",
           "running": "Tempo pro Herzschlag (m/min pro bpm, Steigung als Strecke)",
           "cycling": "Leistung pro Herzschlag (W pro bpm)",
           "easy": "locker", "hard": "härter", "median": "Monatsmedian, nur locker", "up": "höher = fitter"},
}


@styled
def efficiency_trend(eff, out, sport="running", title=None, lang="en"):
    """Aerobic efficiency (EF) of every activity as a dot, easy ones coloured, and the monthly
    median of the easy ones as a line: up is more speed or power for the same heart rate.

    eff: in/strava_efficiency.csv (date, activity_id, sport_type, ef, avg_hr, temp_c, easy).
    """
    tx = TEXT.get(lang if isinstance(lang, str) else "en", TEXT["en"])
    title = title or tx["title"]
    df = frame(eff)
    df["ef_n"] = num(df, "ef")
    df["when"] = pd.to_datetime(df["date"], errors="coerce") if "date" in df.columns else pd.NaT
    df = df[df.ef_n.notna() & df.when.notna()]
    if not len(df):
        return empty_chart(out, title, tx["empty"])
    easy = num(df, "easy").fillna(1) == 1

    fig = new_figure(4.2)
    ax = fig.subplots()
    ax.scatter(df.when[~easy], df.ef_n[~easy], s=22, color=CONTEXT, edgecolors=BG, linewidths=1, zorder=2)
    ax.scatter(df.when[easy], df.ef_n[easy], s=30, color=THIS, edgecolors=BG, linewidths=1, zorder=3)
    e = df[easy]
    if len(e):
        # Every month of the span, empty ones NaN: a break is not bridged
        # by a straight line, and one month alone still shows its marker.
        groups = e.groupby(e.when.dt.to_period("M"))
        monthly, at = groups.ef_n.median(), groups.when.mean()
        span = pd.period_range(monthly.index.min(), monthly.index.max(), freq="M")
        monthly = monthly.reindex(span)
        # A month's point sits at its activities' mean date (inside the axis
        # however short the span), an empty month at its middle, as a gap.
        mid = [at[p] if p in at.index else p.to_timestamp() + pd.Timedelta(days=14) for p in monthly.index]
        ax.plot(mid, monthly.values, color=BEST, lw=2.2, marker="o", ms=5, zorder=4)
    lo, hi = float(df.ef_n.min()), float(df.ef_n.max())
    pad = max((hi - lo) * 0.15, 0.02)
    ax.set_ylim(lo - pad, hi + pad)
    first, last = df.when.min().normalize(), df.when.max().normalize()
    # At least two weeks of axis, ticks on whole days: a short span gave
    # five ticks within one day, the same date twice.
    if last - first < pd.Timedelta(days=14):
        mid = first + (last - first) / 2
        first, last = (mid - pd.Timedelta(days=7)).normalize(), (mid + pd.Timedelta(days=7)).normalize()
    ticks = pd.DatetimeIndex(pd.date_range(first, last, periods=5).normalize()).unique()
    ax.set_xlim(first - pd.Timedelta(days=3), last + pd.Timedelta(days=3))
    ax.set_xticks(ticks)
    ax.set_xticklabels([f"{_day(t, lang)} {t:%y}" for t in ticks])
    handles = [Line2D([], [], marker="o", ls="", color=THIS, markersize=6, label=tx["easy"])]
    if (~easy).any():
        handles.append(Line2D([], [], marker="o", ls="", color=CONTEXT, markersize=5, label=tx["hard"]))
    handles.append(Line2D([], [], color=BEST, lw=2.2, marker="o", ms=5, label=tx["median"]))
    ax.legend(handles=handles, loc="upper left", ncol=3)
    unit = tx["cycling"] if sport == "cycling" else tx["running"]
    header(ax, title, f"{unit} · {tx['up']}")
    return save(fig, out)
