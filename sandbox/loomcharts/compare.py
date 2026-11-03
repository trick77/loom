"""Charts that set one activity against earlier ones: power curve, rank strip, same-route gap."""

import math

import numpy as np
import pandas as pd
from matplotlib.lines import Line2D

from ._style import (AXIS, BEST, BG, CONTEXT, GRID, INK, INK2, MEDIAN, MUTED, THIS, empty_chart, fmt_date, fmt_day,
                     fmt_duration, fmt_duration_long, fmt_secs, frame, has, header, new_figure, num, overlap,
                     renderer, save, sport_noun, styled)

TICK_PREFERRED = (1, 5, 15, 60, 300, 1200, 3600, 10800)
TICK_ALL = (1, 2, 5, 10, 15, 30, 60, 120, 300, 600, 1200, 1800, 3600, 5400, 7200, 10800, 14400)


def _window(weeks):
    return f"{int(weeks)}-week" if weeks else "window"


@styled
def power_curve(curve, out, title=None, weeks=None):
    """Mean-max power by duration (log x): this activity, window best and window median.

    curve: strava_<id>_power_curve.csv. Dots mark durations where this activity beats the
    window best. weeks: the baseline window (labels read "6-week best"); omitted: "window best".
    """
    title = title or "Power curve"
    df = frame(curve)
    d = num(df, "duration_s")
    df = df[d > 0].assign(_d=d[d > 0]).sort_values("_d").reset_index(drop=True)
    d = df["_d"].values if len(df) else np.array([])
    this, best, med = (num(df, c).values for c in ("this_w", "window_best_w", "window_median_w"))
    wl = _window(weeks)
    series = [(best, BEST, f"{wl} best"), (med, MEDIAN, f"{wl} median"), (this, THIS, "this activity")]
    series = [s for s in series if (~np.isnan(s[0])).any()]
    if not series:
        return empty_chart(out, title, "No power data for this activity or its window.")

    fig = new_figure(4.3)
    ax = fig.subplots()
    plotted = []
    for vals, col, label in series:
        ok = ~np.isnan(vals)
        ax.plot(d[ok], vals[ok], color=col, label=label, zorder=3 if col == THIS else 2)
        plotted.append(vals[ok])
    allv = np.concatenate(plotted)

    newbest = ~np.isnan(this) & ~np.isnan(best) & (this > best)
    if newbest.any():
        ax.scatter(d[newbest], this[newbest], s=60, color=THIS, edgecolors=BG, linewidths=2, zorder=5)

    ax.set_xscale("log")
    lo_d, hi_d = float(d.min()), float(d.max())
    ticks = [t for t in TICK_PREFERRED if lo_d <= t <= hi_d]
    if len(ticks) < 3:
        ticks = [t for t in TICK_ALL if lo_d <= t <= hi_d] or [lo_d, hi_d]
    ax.set_xticks(ticks)
    ax.set_xticklabels([fmt_duration(t) for t in ticks])
    ax.minorticks_off()
    ax.set_xlim(lo_d / 1.25, hi_d * 1.25)
    ax.grid(axis="x", visible=False)
    ax.set_ylabel("W")
    vmin, vmax = float(allv.min()), float(allv.max())
    span = max(vmax - vmin, 50.0)
    ax.set_ylim(max(0.0, math.floor((vmin - 0.12 * span) / 50) * 50), vmax + 0.12 * span)

    labels = []
    ok_this = ~np.isnan(this)
    if ok_this.any():
        i = int(np.flatnonzero(ok_this)[0])
        text = f"peak {this[i]:.0f} W" if d[i] <= 5 else f"{fmt_duration_long(d[i])}: {this[i]:.0f} W"
        labels.append(ax.annotate(text, (d[i], this[i]), xytext=(8, -10), textcoords="offset points", va="top",
                                  fontsize=8.5, color=INK2))
        mark = int(np.flatnonzero(newbest)[-1]) if newbest.any() else None
        if mark is None:
            near = np.flatnonzero(ok_this & (d == 1200))
            mark = int(near[0]) if len(near) else None
        if mark is not None and mark != i:
            labels.append(ax.annotate(f"{fmt_duration_long(d[mark])}: {this[mark]:.0f} W", (d[mark], this[mark]),
                                      xytext=(0, 26), textcoords="offset points", ha="center", fontsize=8.5,
                                      color=INK2, arrowprops={"arrowstyle": "-", "color": MUTED, "lw": 0.8,
                                                              "shrinkA": 1, "shrinkB": 5}))
    if len(series) >= 2:
        for loc in ("upper right", "lower left", "center right"):
            leg = ax.legend(loc=loc)
            r = renderer(fig)
            lb = leg.get_window_extent(r)
            if not any(overlap(lb, a.get_window_extent(r)) for a in labels):
                break

    sub = "best average power per duration"
    if not ok_this.any():
        sub += " · no power data for this activity"
    if newbest.any():
        names = [fmt_duration_long(x) for x in d[newbest]]
        shown = ", ".join(names[:4]) + (", …" if len(names) > 4 else "")
        sub += f" · dots: new {wl} bests ({shown})"
    header(ax, title, sub)
    return save(fig, out)


# key, label, formatter, mode: volume (more is more), better, flipped (lower is better), neutral (no rank)
RANK_ROWS = (
    ("distance_km", "Distance", lambda v: f"{v:.1f} km", "volume"),
    ("moving_s", "Moving time", fmt_secs, "volume"),
    ("avg_kmh", "Avg speed", lambda v: f"{v:.1f} km/h", "better"),
    ("gain_m", "Elevation", lambda v: f"{v:,.0f} m", "volume"),
    ("np_w", "Normalized power", lambda v: f"{v:.0f} W", "better"),
    ("tss", "TSS", lambda v: f"{v:.0f}", "volume"),
    ("best_20m_w", "Best 20 min", lambda v: f"{v:.0f} W", "better"),
    ("ef", "Efficiency (NP/HR)", lambda v: f"{v:.2f}", "better"),
    ("decoupling_pct", "HR drift", lambda v: f"{v:.1f} %", "flipped"),
    ("avg_hr", "Avg HR", lambda v: f"{v:.0f} bpm", "neutral"),
)


@styled
def rank_strip(baseline, out, title=None, weeks=None):
    """Where this activity sits among the baseline, one row per metric on its own scale.

    baseline: strava_<id>_baseline.csv (the analysed activity has is_this=1). Right is more
    (volume) or better; HR drift is flipped (right = less drift); avg HR has no rank.
    Rows without a value for this activity are left out. weeks: the window, for the title.
    """
    df = frame(baseline)
    flag = num(df, "is_this").fillna(0) == 1
    this_row = df[flag].iloc[0] if flag.any() else None
    base = df[~flag].reset_index(drop=True)
    sports = list(df["sport_type"]) if "sport_type" in df.columns else []
    one, many = sport_noun(sports)
    title = title or (f"Against your last {int(weeks)} weeks" if weeks else f"Against your recent {many}")

    rows = []
    for key, label, fmt, mode in RANK_ROWS:
        bv = num(base, key).dropna().values
        tv = float(pd.to_numeric(pd.Series([this_row.get(key)]), errors="coerce").iloc[0]) if this_row is not None \
            else math.nan
        if len(bv) == 0 or (this_row is not None and math.isnan(tv)):
            continue
        rows.append((key, label, fmt, mode, bv, tv))
    if not rows:
        return empty_chart(out, title, "No metrics to compare: the baseline is empty.")

    fig = new_figure(0.42 * len(rows) + 1.3)
    fig.subplots_adjust(left=0.22, right=0.76)
    ax = fig.subplots()
    ax.grid(False)
    for s in ax.spines.values():
        s.set_visible(False)
    r = renderer(fig)
    values, ranks, ns = [], [], set()
    for i, (_key, label, fmt, mode, bv, tv) in enumerate(rows):
        y = len(rows) - 1 - i
        allv = bv if math.isnan(tv) else np.append(bv, tv)
        lo, hi = float(allv.min()), float(allv.max())

        def pos(v, lo=lo, hi=hi, mode=mode):
            p = (v - lo) / (hi - lo) if hi > lo else 0.5
            return 1 - p if mode == "flipped" else p

        ax.plot([0, 1], [y, y], color=GRID, lw=1, zorder=1)
        ax.scatter([pos(v) for v in bv], [y] * len(bv), s=34, color=CONTEXT, edgecolors=BG, linewidths=1.5,
                   zorder=2)
        med = float(np.median(bv))
        ax.plot([pos(med)] * 2, [y - 0.28, y + 0.28], color=MEDIAN, lw=2, zorder=3)
        if not math.isnan(tv):
            ax.scatter([pos(tv)], [y], s=80, color=THIS, edgecolors=BG, linewidths=2, zorder=4)
        ax.annotate(label + (" (flipped)" if mode == "flipped" else ""), (0, y), xytext=(-14, 0),
                    textcoords="offset points", ha="right", va="center", fontsize=9.5, color=INK2,
                    annotation_clip=False)
        shown = fmt(med) if math.isnan(tv) else fmt(tv)
        values.append(ax.annotate(shown, (1, y), xytext=(16, 0), textcoords="offset points", ha="left",
                                  va="center", fontsize=9.5, color=INK if not math.isnan(tv) else MUTED,
                                  annotation_clip=False))
        if not math.isnan(tv) and mode != "neutral":
            better = (bv < tv).sum() if mode == "flipped" else (bv > tv).sum()
            ranks.append((y, f"{int(better) + 1}/{len(bv) + 1}"))
            ns.add(len(bv) + 1)
    ax.set_xlim(-0.02, 1.02)
    ax.set_ylim(-0.75, len(rows) - 0.35)
    ax.set_xticks([])
    ax.set_yticks([])
    # Rank column right of the widest value, whatever the font measures.
    widest = max(v.get_window_extent(r).width for v in values) * 72 / fig.dpi
    for y, tag in ranks:
        ax.annotate(tag, (1, y), xytext=(16 + widest + 14, 0), textcoords="offset points", ha="left", va="center",
                    fontsize=9, color=MUTED, annotation_clip=False)

    ax.text(0, -0.7, "← less", fontsize=8.5, color=MUTED, ha="left", va="center")
    more = "more →"
    if any(row[3] == "flipped" for row in rows):
        more += "   (HR drift flipped: right = less drift)"
    ax.text(1, -0.7, more, fontsize=8.5, color=MUTED, ha="right", va="center")

    handles = []
    if this_row is not None:
        handles.append(Line2D([], [], marker="o", ls="", color=THIS, markersize=8, label=f"this {one}"))
    ctx = f"{many}, last {int(weeks)} weeks" if weeks else f"earlier {many}"
    handles += [Line2D([], [], marker="o", ls="", color=CONTEXT, markersize=6, label=ctx),
                Line2D([], [], marker="|", ls="", color=MEDIAN, markersize=10, markeredgewidth=2, label="median")]
    ax.legend(handles=handles, ncol=3, loc="lower left", bbox_to_anchor=(0, 1.0), borderaxespad=0.2,
              handletextpad=0.4, columnspacing=1.6)

    sub = [f"{len(base)} {many if len(base) != 1 else one}"]
    dates = pd.to_datetime(base["date"], errors="coerce").dropna() if "date" in base.columns else pd.Series([])
    if len(dates):
        lo, hi = dates.min(), dates.max()
        sub[0] += f", {fmt_day(lo)} – {fmt_day(hi)}" if lo != hi else f", {fmt_day(lo)}"
    sub.append("each row on its own scale")
    if this_row is None:
        sub.append("values: median")
    if len(ns) == 1:
        sub.append(f"rank among {ns.pop()}")
    header(ax, title, " · ".join(sub), pad=20)
    return save(fig, out)


def _gap_columns(df):
    """(column, date) for every gap_s_<date> column, most recent first, undated last."""
    cols = [(c, pd.to_datetime(c[len("gap_s_"):], errors="coerce")) for c in df.columns
            if isinstance(c, str) and c.startswith("gap_s_")]
    known = [cw for cw in cols if not pd.isna(cw[1])]
    unknown = [cw for cw in cols if pd.isna(cw[1])]
    return sorted(known, key=lambda cw: cw[1], reverse=True) + unknown


@styled
def same_route_gap(same_route, out, title=None):
    """Time gap to each earlier ride of the same route by distance (below 0 = this one ahead).

    same_route: strava_<id>_same_route.csv. The two most recent rides get colour and an end
    label; older ones are drawn as muted context.
    """
    title = title or "Same route: time gap"
    df = frame(same_route)
    x = num(df, "distance_km").values
    cols = [cw for cw in _gap_columns(df) if has(num(df, cw[0]).loc[~np.isnan(x)], 2)]
    if not cols or (~np.isnan(x)).sum() < 2:
        return empty_chart(out, title, "No earlier rides of this route to compare with.")
    gaps = {c: num(df, c).values for c, _ in cols}
    maxabs = max(float(np.nanmax(np.abs(g[~np.isnan(x)]))) for g in gaps.values())
    in_min = maxabs >= 120
    scale, unit = (60.0, "min") if in_min else (1.0, "s")
    years = {w.year for _, w in cols if not pd.isna(w)}

    def name(c, when):
        if pd.isna(when):
            return c[len("gap_s_"):]
        return fmt_day(when) if len(years) <= 1 else fmt_date(when)

    fig = new_figure(4.0)
    ax = fig.subplots()
    x0, x1 = float(np.nanmin(x)), float(np.nanmax(x))
    ends = []
    handles = {}
    for k, (c, when) in enumerate(reversed(cols)):  # oldest first, so the recent lines sit on top
        rank = len(cols) - 1 - k
        g = gaps[c]
        ok = ~np.isnan(x) & ~np.isnan(g)
        if rank < 2:
            col = (BEST, MEDIAN)[rank]
            (handles[rank],) = ax.plot(x[ok], g[ok] / scale, color=col, label=f"vs {name(c, when)}",
                                       zorder=3 + (1 - rank))
            last = np.flatnonzero(ok)[-1]
            ends.append((x[last], g[last] / scale))
        else:
            (handles[2],) = ax.plot(x[ok], g[ok] / scale, color=CONTEXT, lw=1.2, zorder=2,
                                    label=f"older {'rides' if len(cols) > 3 else 'ride'}")
    ax.axhline(0, color=AXIS, lw=1, zorder=1)
    ax.set_xlim(x0, x1 + 0.1 * (x1 - x0))
    ax.grid(axis="x", visible=False)
    ax.set_xlabel("km")
    ax.set_ylabel(f"{unit} (below 0 = ahead)")
    ax.set_ylim(*ax.get_ylim())

    if ends:
        disp = ax.transData.transform(np.array(ends))
        gap_px = 12 * fig.dpi / 72
        order = np.argsort(disp[:, 1])
        ys = disp[order, 1].copy()
        for i in range(1, len(ys)):
            ys[i] = max(ys[i], ys[i - 1] + gap_px)
        disp[order, 1] = ys
        back = ax.transData.inverted().transform(disp)
        for (ex, ev), (_, ly) in zip(ends, back):
            text = f"{ev:+.1f} {unit}" if in_min else f"{ev:+.0f} {unit}"
            ax.annotate(text.replace("-", "−"), (ex, ly), xytext=(6, 0), textcoords="offset points", va="center",
                        fontsize=8.5, color=INK2)
    if len(handles) >= 2:
        ax.legend(handles=[handles[k] for k in sorted(handles)], loc="best")
    n = len(cols)
    header(ax, title, f"this activity against {n} earlier {'ride' if n == 1 else 'rides'} of the route, by distance")
    return save(fig, out)
