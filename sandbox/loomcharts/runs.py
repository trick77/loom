"""Running bests: monthly progress per distance and the race prediction."""

import math

import numpy as np
import pandas as pd

from ._style import INK2, SERIES, empty_chart, fmt_secs, frame, header, new_figure, num, save, styled
from .activity import MONTHS_DE

TEXT = {
    "en": {"title": "Running bests", "sub": "best pace per month and distance (Strava's best efforts inside runs)",
           "pred": "prediction", "empty": "No best efforts to show.", "pace": "pace (min/{u}), faster up",
           "names": {"half marathon": "half marathon", "marathon": "marathon", "Half-Marathon": "half marathon"}},
    "de": {"title": "Laufbestzeiten", "sub": "beste Pace pro Monat und Distanz (Strava-Bestleistungen in Läufen)",
           "pred": "Prognose", "empty": "Keine Bestleistungen vorhanden.", "pace": "Pace (min/{u}), schneller oben",
           "names": {"half marathon": "Halbmarathon", "marathon": "Marathon", "Half-Marathon": "Halbmarathon",
                     "1 mile": "1 Meile", "2 mile": "2 Meilen", "10 mile": "10 Meilen", "1/2 mile": "½ Meile"}},
}

# Distances drawn, shortest first; others stay in the file.
SHOWN = ("1K", "1 mile", "5K", "10K", "Half-Marathon", "Marathon")
MILE = 1609.344


def _month(m, lang):
    t = pd.Timestamp(m + "-01")
    if lang == "de":
        return f"{MONTHS_DE[t.month - 1]} {t:%y}"
    return f"{t:%b %y}"


def _prediction(predictions, tx):
    """The prediction line from the predictions file, "" without one."""
    if predictions is None:
        return ""
    try:
        p = frame(predictions)
    except (OSError, ValueError):
        return ""
    if not len(p) or "race" not in p.columns:
        return ""
    parts = []
    for _, row in p.iterrows():
        v = pd.to_numeric(pd.Series([row.get("vdot_s")]), errors="coerce").iloc[0]
        if math.isfinite(v):
            parts.append(f"{tx['names'].get(row.race, row.race)} {fmt_secs(v)}")
    if not parts:
        return ""
    vdot = pd.to_numeric(p["vdot"], errors="coerce").dropna() if "vdot" in p.columns else pd.Series([])
    head = f"{tx['pred']} (VDOT {vdot.iloc[0]:.1f})" if len(vdot) else tx["pred"]
    return f"{head}: " + " · ".join(parts)


@styled
def run_bests(bests, out, predictions=None, unit="km", title=None, lang="en"):
    """Best pace per month for each distance (faster up), the latest time at the line's end;
    the race prediction (Daniels VDOT) below.

    bests: in/strava_run_bests.csv (month, effort, distance_m, secs, activity_id, date).
    predictions: in/strava_run_predictions.csv, optional. unit: "km" or "mi" (pace per mile).
    """
    tx = TEXT.get(lang if isinstance(lang, str) else "en", TEXT["en"])
    title = title or tx["title"]
    per = MILE if unit == "mi" else 1000.0
    df = frame(bests)
    if not len(df) or "effort" not in df.columns:
        return empty_chart(out, title, tx["empty"])
    df["secs_n"], df["dist_n"] = num(df, "secs"), num(df, "distance_m")
    df = df[(df.secs_n > 0) & (df.dist_n > 0)]
    names = [n for n in SHOWN if n in set(df.effort)]
    if not names:
        names = list(df.groupby("effort").dist_n.first().sort_values().index[:4])
    if not names:
        return empty_chart(out, title, tx["empty"])
    # Every month from the first to the last, so a break (an injury) keeps
    # its width instead of joining its neighbours.
    seen = pd.PeriodIndex(df.month.astype(str), freq="M")
    months = [str(m) for m in pd.period_range(seen.min(), seen.max(), freq="M")]
    x_of = {m: i for i, m in enumerate(months)}

    fig = new_figure(4.4)
    ax = fig.subplots()
    pred = _prediction(predictions, tx)
    if pred:
        fig.subplots_adjust(bottom=0.2)  # before any label is placed in pixels
    lo, hi = math.inf, -math.inf
    ends = []
    for k, name in enumerate(names):
        d = df[df.effort == name].sort_values("month")
        pace = d.secs_n / d.dist_n * per
        xs = [x_of[str(m)] for m in d.month]
        col = SERIES[k % len(SERIES)]
        ax.plot(xs, pace, color=col, lw=2, marker="o", ms=4)
        lo, hi = min(lo, pace.min()), max(hi, pace.max())
        ends.append((xs[-1], float(pace.iloc[-1]), f"{tx['names'].get(name, name)} {fmt_secs(d.iloc[-1].secs_n)}", col))
    pad = max(10.0, (hi - lo) * 0.12)
    ax.set_ylim(hi + pad, lo - pad)  # faster up
    ax.set_xlim(-0.4, len(months) - 0.4 + max(1.6, len(months) * 0.18))
    ticks = np.linspace(lo, hi, 4)
    ax.set_yticks(ticks)
    ax.set_yticklabels([fmt_secs(t) for t in ticks])
    ax.set_ylabel(tx["pace"].format(u=unit if unit == "mi" else "km"))
    step = max(1, len(months) // 8)
    ax.set_xticks(range(0, len(months), step))
    ax.set_xticklabels([_month(m, lang) for m in months[::step]])

    # End labels pushed apart, on the final layout: distances of near the
    # same pace end on top of each other otherwise.
    disp = [ax.transData.transform((x, y))[1] for x, y, _, _ in ends]
    gap = 12 * fig.dpi / 72
    placed, last = {}, -math.inf
    for i in sorted(range(len(ends)), key=lambda i: disp[i]):
        placed[i] = last = max(disp[i], last + gap)
    for i, (x, y, text, col) in enumerate(ends):
        ax.annotate(text, (x, y), xytext=(8, (placed[i] - disp[i]) * 72 / fig.dpi), textcoords="offset points",
                    va="center", fontsize=8.5, color=col)
    if pred:
        ax.annotate(pred, (0, 0), xycoords="axes fraction", xytext=(0, -38), textcoords="offset points", va="top",
                    fontsize=9.5, color=INK2)
    header(ax, title, tx["sub"])
    return save(fig, out)
