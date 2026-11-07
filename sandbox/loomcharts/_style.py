"""Palette, rcParams and the small helpers every chart shares."""

import functools
import math
import os

import matplotlib as mpl
import numpy as np
import pandas as pd
from matplotlib.backends.backend_agg import FigureCanvasAgg
from matplotlib.colors import LinearSegmentedColormap
from matplotlib.figure import Figure

# loom's dark surface and ink (ui/src/index.css), plus the validated chart slots.
BG = "#1f1f1e"
INK = "#faf9f5"
INK2 = "#c3c2b7"
MUTED = "#9c9a92"
GRID = "#2c2c2a"
AXIS = "#383835"
THIS = "#d95926"  # this activity
BEST = "#3987e5"  # window best
MEDIAN = "#199e70"  # window median
ZONES = ("#184f95", "#2a78d6", "#5598e7", "#86b6ef", "#cde2fb")  # Z1..Z5+, ordinal
CONTEXT = "#55544f"  # de-emphasised context marks, not a series
PLAIN = "#383835"  # profile fill without zones

RAMP = LinearSegmentedColormap.from_list("loom_ramp", ZONES)
if "loom_ramp" not in mpl.colormaps:  # cmap="loom_ramp" works once loomcharts is imported
    mpl.colormaps.register(RAMP)

# Series colours for hand-written charts, in this order: the dataviz dark slots,
# led by THIS, BEST, MEDIAN so a plain chart opens like the Strava ones. Adjacent
# pairs validated on BG (CVD ΔE >= 8.4, normal vision >= 19.3); the chart
# functions pass their colours explicitly and never read the cycle.
SERIES = (THIS, BEST, MEDIAN, "#c98500", "#d55181", "#008300", "#9085e9", "#e66767")

# The UI's sans, converted to static TTFs in the image build (sandbox/fonts.py).
# The self-test fails the build when it does not resolve: a missing family would
# print a findfont warning into every job's output. It has Latin only (580
# glyphs): DejaVu Sans fills in per glyph (μ, Δ, ✓, Cyrillic) instead of tofu.
FONT = "Loom Sans"
FALLBACK = "DejaVu Sans"

STYLE = {
    "figure.facecolor": BG,
    "axes.facecolor": BG,
    "savefig.facecolor": BG,
    "axes.edgecolor": AXIS,
    "axes.labelcolor": MUTED,
    "axes.titlecolor": INK,
    "axes.titleweight": "bold",
    "axes.titlelocation": "left",
    "xtick.color": MUTED,
    "ytick.color": MUTED,
    "xtick.labelcolor": MUTED,
    "ytick.labelcolor": MUTED,
    "text.color": INK,
    "font.family": [FONT, FALLBACK],
    "font.size": 9.5,
    "axes.grid": True,
    "axes.grid.axis": "y",  # value lines only, as every chart function draws them
    "axes.axisbelow": True,  # grid behind bars and fills
    "grid.color": GRID,
    "grid.linewidth": 1,
    "axes.spines.top": False,
    "axes.spines.right": False,
    "axes.spines.left": False,
    "axes.prop_cycle": mpl.cycler(color=list(SERIES)),
    "lines.linewidth": 2,
    "lines.solid_capstyle": "round",
    "lines.solid_joinstyle": "round",
    "legend.frameon": False,
    "legend.labelcolor": INK2,
    "legend.fontsize": 8.5,
    "legend.title_fontsize": 8.5,
    "image.cmap": "loom_ramp",
    "savefig.dpi": 200,
    "savefig.bbox": "tight",
    "savefig.pad_inches": 0.25,
}

WIDTH = 8.6  # inches; 1,720 px at dpi 200

# Where the image's matplotlibrc differs from STYLE: loom_ramp exists only once
# this module registered it, after the rc file is read, so plain heatmaps get
# the nearest built-in (dark navy to pale blue, like ZONES).
RC_OVERRIDES = {"image.cmap": "Blues_r"}


def rc_style():
    """STYLE as the image's matplotlibrc holds it."""
    return {**STYLE, **RC_OVERRIDES}


def _rc_value(value):
    # A "#" starts a comment in matplotlibrc; matplotlib reads hex colours without it.
    if hasattr(value, "by_key"):
        parts = ", ".join(f"{k}={[str(c).lstrip('#') for c in v]!r}" for k, v in value.by_key().items())
        return f"cycler({parts})"
    if isinstance(value, (list, tuple)):
        return ", ".join(_rc_value(v) for v in value)
    if isinstance(value, str):
        return value.lstrip("#")
    return str(value)


def matplotlibrc():
    """rc_style() as matplotlibrc text: the image installs it as every job's default."""
    lines = ["# Written by loomcharts.rc from loomcharts/_style.py STYLE; edit that, not this."]
    lines += [f"{k}: {_rc_value(v)}" for k, v in rc_style().items()]
    return "\n".join(lines) + "\n"


def apply_style():
    """Set loom's dark chart style globally (rcParams) for hand-written matplotlib charts.

    The chart functions do not need it: they apply the same style only while they draw.
    Save with plt.savefig("out/x.png") (dpi 200 and a tight box are already set).
    """
    mpl.rcParams.update(STYLE)


def styled(fn):
    """Run a chart function inside loom's style without touching global rcParams."""

    @functools.wraps(fn)
    def wrapper(*args, **kwargs):
        with mpl.rc_context(STYLE):
            return fn(*args, **kwargs)

    return wrapper


def new_figure(height, width=WIDTH):
    """A pyplot-free figure (nothing to close, no global state) with an Agg canvas."""
    fig = Figure(figsize=(width, height))
    FigureCanvasAgg(fig)
    return fig


def save(fig, out):
    """Write fig as PNG to out (".png" appended if missing, parent made); return the path."""
    out = os.fspath(out)
    if not out.lower().endswith(".png"):
        out += ".png"
    parent = os.path.dirname(out)
    if parent:
        os.makedirs(parent, exist_ok=True)
    fig.savefig(out, format="png")
    return out


def header(ax, title, subtitle=None, pad=0):
    """Title and muted subtitle above ax, left-aligned to the plot area, pad points higher."""
    y = 12 + pad
    if subtitle:
        ax.annotate(subtitle, (0, 1), xycoords="axes fraction", xytext=(0, y), textcoords="offset points",
                    fontsize=9.5, color=MUTED, va="baseline", annotation_clip=False)
        y += 19
    if title:
        ax.annotate(title, (0, 1), xycoords="axes fraction", xytext=(0, y), textcoords="offset points",
                    fontsize=13, fontweight="bold", color=INK, va="baseline", annotation_clip=False)


def frame(src):
    """A DataFrame from a CSV path, a file object or a DataFrame (copied).

    A bare file name that is not in the working directory is read from in/, where a job's
    input files sit: the tools name their files bare, and a model passing that name got a
    FileNotFoundError.
    """
    if isinstance(src, pd.DataFrame):
        return src.copy()
    if isinstance(src, (str, os.PathLike)):
        name = os.fspath(src)
        inside = os.path.join("in", name)
        if not os.path.exists(name) and os.path.dirname(name) == "" and os.path.exists(inside):
            src = inside
    return pd.read_csv(src)


def num(df, name):
    """Column name as float (NaN where empty, missing or not a number)."""
    if name not in df.columns:
        return pd.Series(np.nan, index=df.index, dtype=float)
    return pd.to_numeric(df[name], errors="coerce").astype(float)


def has(series, at_least=1):
    return int(series.notna().sum()) >= at_least


def empty_chart(out, title, message):
    """A placeholder PNG that says what is missing, so a call never fails on absent data."""
    fig = new_figure(2.4)
    ax = fig.add_subplot()
    ax.set_axis_off()
    ax.text(0.5, 0.45, message, ha="center", va="center", fontsize=10, color=INK2, transform=ax.transAxes)
    header(ax, title)
    return save(fig, out)


def fmt_secs(s):
    """1:02:03 or 2:03."""
    if s is None or (isinstance(s, float) and math.isnan(s)):
        return "–"
    s = int(round(s))
    return f"{s // 3600}:{s % 3600 // 60:02d}:{s % 60:02d}" if s >= 3600 else f"{s // 60}:{s % 60:02d}"


def fmt_duration(s):
    """Short duration label for power-curve ticks: 5s, 1m, 20m, 1h, 1.5h."""
    s = float(s)
    if s < 60:
        return f"{s:g}s"
    if s < 3600:
        return f"{s / 60:g}m"
    return f"{s / 3600:g}h"


def fmt_duration_long(s):
    s = float(s)
    if s < 60:
        return f"{s:g} s"
    if s < 3600:
        return f"{s / 60:g} min"
    return f"{s / 3600:g} h"


def fmt_date(d):
    """1 Nov 2026 (or the input unchanged if it is not a date)."""
    try:
        t = pd.Timestamp(d)
    except (ValueError, TypeError):
        return str(d)
    return f"{t.day} {t:%b %Y}"


def fmt_range(lo, hi):
    """22 Sep – 30 Oct 2026, 28 Dec 2025 – 3 Jan 2026, or one date."""
    lo, hi = pd.Timestamp(lo), pd.Timestamp(hi)
    if lo.normalize() == hi.normalize():
        return fmt_date(lo)
    return f"{fmt_day(lo) if lo.year == hi.year else fmt_date(lo)} – {fmt_date(hi)}"


def fmt_day(d):
    try:
        t = pd.Timestamp(d)
    except (ValueError, TypeError):
        return str(d)
    return f"{t.day} {t:%b}"


SPORT_NOUNS = {"ride": ("ride", "rides"), "run": ("run", "runs"), "swim": ("swim", "swims"),
               "walk": ("walk", "walks"), "hike": ("hike", "hikes")}


def sport_noun(sport_types):
    """("ride", "rides") when every sport_type is a kind of ride, else run, …, activity."""
    kinds = set()
    for s in sport_types:
        if not isinstance(s, str) or not s:
            continue
        low = s.lower()
        kinds.add(next((k for k in SPORT_NOUNS if low.endswith(k)), "activity"))
    if len(kinds) == 1 and "activity" not in kinds:
        return SPORT_NOUNS[kinds.pop()]
    return ("activity", "activities")


def renderer(fig):
    return fig.canvas.get_renderer()


def overlap(a, b, pad=2.0):
    """Two display-space bboxes overlap (with pad pixels of air)."""
    return a.x0 < b.x1 + pad and a.x1 + pad > b.x0 and a.y0 < b.y1 + pad and a.y1 + pad > b.y0


def spread_labels(fig, labels, step=3.0, rounds=40):
    """Lift offset-point annotations (sorted left to right) until none overlaps an earlier one.

    labels: annotations positioned with textcoords="offset points"; each is raised by
    step points per round while it collides. Returns the labels' final bboxes.
    """
    r = renderer(fig)
    placed = []
    for ann in labels:
        for _ in range(rounds):
            box = ann.get_window_extent(r)
            if not any(overlap(box, p) for p in placed):
                break
            dx, dy = ann.xyann
            ann.xyann = (dx, dy + step)
        placed.append(ann.get_window_extent(r))
    return placed
