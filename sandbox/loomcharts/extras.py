"""Charts drawn on request: W′ balance over a ride, the runner's own pace per grade."""

import numpy as np

from ._style import AXIS, BEST, CONTEXT, INK2, MUTED, THIS, empty_chart, fmt_secs, frame, header, new_figure, num, save, styled

TEXT = {
    "en": {"wbal": "W′ balance", "wbal_sub": "anaerobic work capacity left (kJ), power above CP drains it · CP {cp} W, W′ {wp} kJ",
           "power": "power (W)", "left": "W′ left (kJ)", "km": "km", "time": "time",
           "grade": "Pace per grade", "grade_sub": "the runner's own median pace per grade, from their recent runs · faster up",
           "grade_x": "grade (%)", "pace": "pace (min/km)", "flat": "flat", "empty": "Nothing to show."},
    "de": {"wbal": "W′-Balance", "wbal_sub": "verbleibende anaerobe Kapazität (kJ), Leistung über CP leert sie · CP {cp} W, W′ {wp} kJ",
           "power": "Leistung (W)", "left": "W′ übrig (kJ)", "km": "km", "time": "Zeit",
           "grade": "Pace nach Steigung", "grade_sub": "eigene Median-Pace pro Steigung aus den letzten Läufen · schneller oben",
           "grade_x": "Steigung (%)", "pace": "Pace (min/km)", "flat": "flach", "empty": "Nichts zu zeigen."},
}


def _t(lang):
    return TEXT.get(lang if isinstance(lang, str) else "en", TEXT["en"])


@styled
def wbal(wbal, out, cp=None, w_prime=None, title=None, lang="en"):
    """W′ balance over a ride (kJ left, lower panel) under its power (upper panel, CP as a line).

    wbal: in/strava_<id>_wbal.csv (elapsed_s, distance_km, watts, wbal_kj). cp, w_prime: the
    fit activity_analysis printed (W, J).
    """
    tx = _t(lang)
    title = title or tx["wbal"]
    df = frame(wbal)
    bal, w = num(df, "wbal_kj"), num(df, "watts")
    if bal.notna().sum() < 2:
        return empty_chart(out, title, tx["empty"])
    d = num(df, "distance_km")
    by_km = d.notna().sum() > len(df) * 0.9
    x = d if by_km else num(df, "elapsed_s") / 60

    fig = new_figure(4.8)
    gs = fig.add_gridspec(2, 1, height_ratios=(1, 1.3), hspace=0.12)
    top, low = fig.add_subplot(gs[0]), fig.add_subplot(gs[1], sharex=None)
    top.plot(x, w, color=CONTEXT, lw=0.8)
    if cp:
        top.axhline(cp, color=THIS, lw=1.2, ls=(0, (4, 3)))
        top.annotate(f"CP {cp:.0f} W", (1, cp), xycoords=("axes fraction", "data"), xytext=(-4, 4), textcoords="offset points",
                     ha="right", fontsize=8.5, color=THIS)
    top.set_ylabel(tx["power"])
    top.set_xticklabels([])
    low.fill_between(x, bal, color=BEST, alpha=0.25, lw=0)
    low.plot(x, bal, color=BEST, lw=1.8)
    low.axhline(0, color=AXIS, lw=1)
    i = int(np.nanargmin(bal.values))
    low.annotate(f"{bal.iloc[i]:.1f} kJ", (x.iloc[i], bal.iloc[i]), xytext=(0, -14), textcoords="offset points", ha="center",
                 fontsize=8.5, color=INK2)
    top_kj = (w_prime / 1000) if w_prime else float(bal.max())
    low.set_ylim(min(0.0, float(bal.min())) - top_kj * 0.08, top_kj * 1.08)
    low.set_ylabel(tx["left"])
    low.set_xlabel(tx["km"] if by_km else tx["time"] + " (min)")
    for ax in (top, low):
        ax.set_xlim(float(x.min()), float(x.max()))
    sub = tx["wbal_sub"].format(cp=f"{cp:.0f}" if cp else "–", wp=f"{w_prime / 1000:.1f}" if w_prime else "–")
    header(top, title, sub)
    return save(fig, out)


@styled
def grade_pace(gp, out, title=None, lang="en"):
    """The runner's own median pace per grade (faster up), dots sized by the seconds behind them.

    gp: in/strava_<id>_grade_pace.csv (grade_pct, rel_speed, pace_s_km, seconds).
    """
    tx = _t(lang)
    title = title or tx["grade"]
    df = frame(gp)
    g, pace, secs = num(df, "grade_pct"), num(df, "pace_s_km"), num(df, "seconds").fillna(0)
    ok = g.notna() & pace.notna()
    if ok.sum() < 2:
        return empty_chart(out, title, tx["empty"])
    g, pace, secs = g[ok], pace[ok], secs[ok]
    fig = new_figure(4.2)
    ax = fig.subplots()
    ax.plot(g, pace, color=BEST, lw=1.6, zorder=2)
    size = 12 + 80 * (secs / max(1.0, float(secs.max())))
    ax.scatter(g, pace, s=size, color=BEST, zorder=3)
    flat = pace[g == 0]
    if len(flat):
        ax.axhline(float(flat.iloc[0]), color=MUTED, lw=1, ls=(0, (2, 2)))
        ax.annotate(f"{tx['flat']} {fmt_secs(flat.iloc[0])}", (float(g.min()), float(flat.iloc[0])), xytext=(0, 5),
                    textcoords="offset points", fontsize=8.5, color=INK2)
    ax.axvline(0, color=AXIS, lw=1)
    lo, hi = float(pace.min()), float(pace.max())
    pad = max(10.0, (hi - lo) * 0.1)
    ax.set_ylim(hi + pad, lo - pad)
    ticks = np.linspace(lo, hi, 4)
    ax.set_yticks(ticks)
    ax.set_yticklabels([fmt_secs(t) for t in ticks])
    ax.set_xlabel(tx["grade_x"])
    ax.set_ylabel(tx["pace"])
    header(ax, title, tx["grade_sub"])
    return save(fig, out)
