"""Charts of one activity's streams: elevation profile and the GPS track."""

import math

import numpy as np
import pandas as pd
from matplotlib.collections import LineCollection, PolyCollection
from matplotlib.lines import Line2D
from matplotlib.patches import Rectangle
from matplotlib.transforms import blended_transform_factory

from ._style import (BG, INK, INK2, MUTED, PLAIN, RAMP, THIS, ZONES, empty_chart, fmt_secs, frame, has, header,
                     new_figure, num, overlap, renderer, save, spread_labels, styled)



def _zone_labels(kind):
    """Power folds Strava's Z5-Z7 into Z5+; heart rate has five zones."""
    return ("Z1", "Z2", "Z3", "Z4", "Z5+" if kind == "power zone" else "Z5")


def _sample_secs(df):
    t = num(df, "elapsed_s").dropna()
    if len(t) < 2:
        return 5.0
    d = float(np.median(np.diff(t.values)))
    return d if d > 0 else 5.0


def _moving_mask(df):
    m = num(df, "moving")
    if has(m):
        return (m.fillna(1) > 0).values
    return np.ones(len(df), dtype=bool)


def _quiet_corner(ax, text, xs, ys, **kw):
    """Put text into the corner of ax with the fewest data points under it; return the Text."""
    fig = ax.figure
    r = renderer(fig)
    pts = ax.transData.transform(np.column_stack([xs, ys])) if len(xs) else np.empty((0, 2))
    best, best_n = None, None
    for x, y, ha, va in ((0.995, 0.06, "right", "bottom"), (0.005, 0.06, "left", "bottom"),
                         (0.995, 0.94, "right", "top"), (0.005, 0.94, "left", "top")):
        t = ax.text(x, y, text, transform=ax.transAxes, ha=ha, va=va, **kw)
        b = t.get_window_extent(r).expanded(1.15, 1.4)
        n = int(((pts[:, 0] >= b.x0) & (pts[:, 0] <= b.x1) & (pts[:, 1] >= b.y0) & (pts[:, 1] <= b.y1)).sum())
        if best_n is None or n < best_n:
            if best is not None:
                best.remove()
            best, best_n = t, n
        else:
            t.remove()
        if n == 0:
            break
    return best


def _zone_colours(x, zones, moving, edges):
    """Colour per x bin: the median zone of the moving rows in it (PLAIN where unknown)."""
    cols = []
    idx = np.digitize(x, edges) - 1
    for b in range(len(edges) - 1):
        m = (idx == b) & moving & ~np.isnan(zones)
        if not m.any():
            m = (idx == b) & ~np.isnan(zones)
        if m.any():
            z = int(np.clip(round(float(np.median(zones[m]))), 1, 5))
            cols.append(ZONES[z - 1])
        else:
            cols.append(PLAIN)
    return cols


def _climb_text(c):
    first = [f"Climb {int(c['n'])}" if not math.isnan(c["n"]) else "Climb"]
    bits = []
    if not math.isnan(c["len_km"]):
        bits.append(f"{c['len_km']:.1f} km")
    if not math.isnan(c["grade_pct"]):
        bits.append(f"{c['grade_pct']:.1f} %")
    line1 = first[0] + (": " + " · ".join(bits) if bits else "")
    second = []
    if not math.isnan(c["secs"]):
        second.append(fmt_secs(c["secs"]))
    if not math.isnan(c["vam"]):
        second.append(f"VAM {c['vam']:,.0f}")
    if not math.isnan(c["wkg"]):
        second.append(f"{c['wkg']:.1f} W/kg")
    elif not math.isnan(c["avg_w"]):
        second.append(f"{c['avg_w']:.0f} W")
    return line1 + ("\n" + " · ".join(second) if second else "")


def _read_climbs(climbs):
    if climbs is None:
        return []
    try:
        cf = frame(climbs)
    except (OSError, ValueError):
        return []
    rows = []
    for i in range(len(cf)):
        row = {k: float(num(cf, k).iloc[i]) for k in
               ("n", "from_km", "to_km", "len_km", "gain_m", "grade_pct", "secs", "vam", "avg_w", "wkg", "avg_hr")}
        if math.isnan(row["n"]):
            row["n"] = float(i + 1)
        if not (math.isnan(row["from_km"]) or math.isnan(row["to_km"])):
            rows.append(row)
    return rows


@styled
def ride_profile(streams, out, climbs=None, title=None):
    """Elevation area coloured by power zone (HR zone without power), climbs labelled, HR panel below.

    streams: strava_<id>_streams.csv; climbs: strava_<id>_climbs.csv (optional).
    Without altitude only the HR panel is drawn; without HR only the elevation.
    """
    title = title or "Ride profile"
    df = frame(streams)
    dist = num(df, "distance_km")
    if has(dist, 2):
        x, xlabel = dist.values, "km"
    else:
        x, xlabel = (num(df, "elapsed_s") / 60).values, "min"
    keep = ~np.isnan(x)
    if keep.sum() < 2:
        return empty_chart(out, title, "No distance or time data in this activity.")
    df, x = df[keep].reset_index(drop=True), x[keep]
    order = np.argsort(x, kind="stable")
    df, x = df.iloc[order].reset_index(drop=True), x[order]
    alt, hr = num(df, "altitude_m").values, num(df, "hr").values
    moving = _moving_mask(df)
    zones, zone_kind = None, None
    for col, kind in (("power_zone", "power zone"), ("hr_zone", "heart-rate zone")):
        z = num(df, col)
        if has(z):
            zones, zone_kind = z.values, kind
            break
    show_alt = int((~np.isnan(alt)).sum()) >= 2
    show_hr = int((~np.isnan(hr)).sum()) >= 2
    if not show_alt and not show_hr:
        return empty_chart(out, title, "No elevation or heart-rate data in this activity.")

    x0, x1 = float(x.min()), float(x.max())
    if x1 <= x0:
        x1 = x0 + 1
    if show_alt and show_hr:
        fig = new_figure(4.9)
        a1, a2 = fig.subplots(2, 1, sharex=True, gridspec_kw={"height_ratios": [2.3, 1], "hspace": 0.12})
    else:
        fig = new_figure(3.7 if show_alt else 2.8)
        ax = fig.subplots()
        a1, a2 = (ax, None) if show_alt else (None, ax)

    sub = []
    if xlabel == "km":
        sub.append(f"{x1:.1f} km")
    sub.append(f"{fmt_secs(moving.sum() * _sample_secs(df))} moving")

    if a1 is not None:
        ok = ~np.isnan(alt)
        xa, aa = x[ok], alt[ok]
        lo, hi = float(aa.min()), float(aa.max())
        span = max(hi - lo, 40.0)
        base = lo - 0.08 * span
        top = hi + 0.5 * span
        width = (x1 - x0) / 300
        if xlabel == "km":
            width = max(width, 0.1)
        edges = np.arange(x0, x1 + width, width)
        cols = (_zone_colours(x, zones, moving, edges) if zones is not None else [PLAIN] * (len(edges) - 1))
        polys = []
        for b in range(len(edges) - 1):
            a, c = edges[b], min(edges[b + 1], x1)
            inside = (xa > a) & (xa < c)
            pts = [(a, base), (a, float(np.interp(a, xa, aa)))]
            pts += list(zip(xa[inside], aa[inside]))
            pts += [(c, float(np.interp(c, xa, aa))), (c, base)]
            polys.append(pts)
        # Edges in the face colour close the hairline gaps antialiasing leaves between bins.
        a1.add_collection(PolyCollection(polys, facecolors=cols, edgecolors=cols, linewidths=0.4, zorder=2))
        a1.plot(xa, aa, color=INK2, lw=1.2, zorder=3)
        a1.set_xlim(x0, x1)
        a1.set_ylim(base, top)
        a1.set_ylabel("m")
        a1.tick_params(axis="x", labelbottom=a2 is None)
        if a2 is None:
            a1.set_xlabel(xlabel)

        legend = None
        if zones is not None:
            handles = [Rectangle((0, 0), 1, 1, color=c) for c in ZONES]
            legend = a1.legend(handles, _zone_labels(zone_kind), title=zone_kind, ncol=5, loc="upper right",
                               handlelength=1.0, columnspacing=1.0, handletextpad=0.5, borderaxespad=0.3)
            legend.get_title().set_color(MUTED)
            sub.append(f"elevation coloured by {zone_kind}")

        rows = _read_climbs(climbs) if xlabel == "km" else []
        if len(rows) > 5:  # label selectively: the five biggest
            rows = sorted(rows, key=lambda c: -(0 if math.isnan(c["gain_m"]) else c["gain_m"]))[:5]
        rows.sort(key=lambda c: c["from_km"])
        labels = []
        for c in rows:
            seg = (xa >= c["from_km"]) & (xa <= c["to_km"])
            if seg.any():
                k = int(np.argmax(np.where(seg, aa, -np.inf)))
                top_x, peak = float(xa[k]), float(aa[k])
            else:
                top_x, peak = c["to_km"], float(np.interp(c["to_km"], xa, aa))
            frac = (top_x - x0) / (x1 - x0)
            ha = "left" if frac < 0.12 else "right" if frac > 0.88 else "center"
            labels.append(a1.annotate(_climb_text(c), (top_x, peak), xytext=(0, 14), textcoords="offset points",
                                      ha=ha, va="bottom", fontsize=8, color=INK2, linespacing=1.3,
                                      arrowprops={"arrowstyle": "-", "color": MUTED, "lw": 0.8, "shrinkA": 1,
                                                  "shrinkB": 2}))
        # Room for the labels: below the top edge and clear of the legend.
        for _ in range(8):
            for ann in labels:
                ann.xyann = (0, 14)
            boxes = spread_labels(fig, labels)
            axbox = a1.get_window_extent(renderer(fig))
            legbox = legend.get_window_extent(renderer(fig)) if legend is not None else None
            if not any(b.y1 > axbox.y1 - 2 or (legbox is not None and overlap(b, legbox)) for b in boxes):
                break
            top += 0.2 * span
            a1.set_ylim(base, top)

    if a2 is not None:
        ok = ~np.isnan(hr)
        line = pd.Series(hr).rolling(3, min_periods=1, center=True).mean().values
        a2.plot(x[ok], line[ok], color=THIS, lw=1.6)
        lo, hi = float(np.nanmin(hr)), float(np.nanmax(hr))
        a2.set_ylim(math.floor((lo - 5) / 10) * 10, math.ceil((hi + 5) / 10) * 10)
        a2.set_xlim(x0, x1)
        a2.set_ylabel("bpm")
        a2.set_xlabel(xlabel)
        hm = hr[moving & ok] if (moving & ok).any() else hr[ok]
        _quiet_corner(a2, f"HR · avg {np.mean(hm):.0f}, max {np.max(hr[ok]):.0f}", x[ok], line[ok],
                      fontsize=8.5, color=INK2)
        if a1 is None:
            sub.append("no elevation data")

    header(a1 if a1 is not None else a2, title, " · ".join(sub))
    return save(fig, out)


COLOR_ALIASES = {"power": "watts", "w": "watts", "watt": "watts", "speed": "speed_kmh", "kmh": "speed_kmh",
                 "heart_rate": "hr", "heartrate": "hr", "altitude": "altitude_m", "elevation": "altitude_m",
                 "zone": "power_zone", "grade": "grade_pct"}
COLOR_UNITS = {"watts": ("power", "W"), "speed_kmh": ("speed", "km/h"), "hr": ("heart rate", "bpm"),
               "altitude_m": ("altitude", "m"), "cadence": ("cadence", "rpm"), "grade_pct": ("grade", "%"),
               "temp_c": ("temperature", "°C")}


def _scale_length(span_km):
    for v in (50, 20, 10, 5, 2, 1, 0.5, 0.2, 0.1):
        if v <= span_km / 4:
            return v
    return 0.05


@styled
def route_track(streams, out, color_by="watts", title=None):
    """The GPS track (lat/lng, no basemap) coloured by color_by, start and finish marked.

    color_by: watts (default), speed_kmh, hr, altitude_m, cadence, power_zone or hr_zone
    (aliases: power, speed, elevation). Falls back to watts, then speed, then a plain line.
    """
    title = title or "Route"
    df = frame(streams)
    lat, lng = num(df, "lat").values, num(df, "lng").values
    ok = ~np.isnan(lat) & ~np.isnan(lng)
    if ok.sum() < 2:
        return empty_chart(out, title, "No GPS track in this activity (indoor, or recorded without GPS).")
    df = df[ok].reset_index(drop=True)
    lat, lng = lat[ok], lng[ok]
    lat0, lng0 = float(np.mean(lat)), float(np.mean(lng))
    xs = (lng - lng0) * 111.32 * math.cos(math.radians(lat0))
    ys = (lat - lat0) * 110.574
    xspan, yspan = max(float(np.ptp(xs)), 0.05), max(float(np.ptp(ys)), 0.05)

    key = COLOR_ALIASES.get(str(color_by).lower(), str(color_by).lower())
    tried = [key] + [k for k in ("watts", "speed_kmh") if k != key]
    key = next((k for k in tried if has(num(df, k), 2)), None)

    h = min(max(7.2 * yspan / xspan, 3.4), 8.0) + 0.9
    fig = new_figure(h)
    ax = fig.subplots()
    ax.set_axis_off()
    ax.set_aspect("equal", adjustable="box")
    pts = np.column_stack([xs, ys])
    segs = np.stack([pts[:-1], pts[1:]], axis=1)
    sub = [f"{float(num(df, 'distance_km').max()):.1f} km"] if has(num(df, "distance_km")) else []

    if key in ("power_zone", "hr_zone"):
        z = num(df, key).values
        zc = np.nanmean(np.column_stack([z[:-1], z[1:]]), axis=1)
        cols = [ZONES[int(np.clip(round(v), 1, 5)) - 1] if not math.isnan(v) else MUTED for v in zc]
        ax.add_collection(LineCollection(segs, colors=cols, linewidths=2.2, capstyle="round"))
        kind = "power zone" if key == "power_zone" else "heart-rate zone"
        handles = [Line2D([], [], color=c, lw=3) for c in ZONES]
        # Below the map, right of the scale bar: inside it would sit on the track.
        leg = ax.legend(handles, _zone_labels(kind), title=kind, ncol=5, loc="upper right",
                        bbox_to_anchor=(1, 0.0), handlelength=1.2, columnspacing=1.0)
        leg.get_title().set_color(MUTED)
        sub.append(f"coloured by {kind}")
    elif key is not None:
        v = num(df, key)
        window = max(1, int(round(30 / _sample_secs(df))))
        v = v.rolling(window, min_periods=1, center=True).mean().interpolate(limit_direction="both").values
        vc = (v[:-1] + v[1:]) / 2
        lo, hi = np.nanpercentile(vc, 2), np.nanpercentile(vc, 98)
        if hi <= lo:
            hi = lo + 1
        lc = LineCollection(segs, cmap=RAMP, linewidths=2.2, capstyle="round")
        lc.set_array(np.clip(vc, lo, hi))
        lc.set_clim(lo, hi)
        ax.add_collection(lc)
        name, unit = COLOR_UNITS.get(key, (key, ""))
        cb = fig.colorbar(lc, ax=ax, orientation="vertical", fraction=0.025, pad=0.02, shrink=0.55)
        cb.outline.set_visible(False)
        cb.ax.tick_params(colors=MUTED, labelsize=8, length=0)
        cb.set_label(f"{name} ({unit}, 30 s)" if unit else name, color=MUTED, fontsize=8.5)
        sub.append(f"coloured by {name}")
    else:
        ax.add_collection(LineCollection(segs, colors=INK2, linewidths=2, capstyle="round"))

    pad = 0.06 * max(xspan, yspan)
    ax.set_xlim(xs.min() - pad, xs.max() + pad)
    ax.set_ylim(ys.min() - pad, ys.max() + pad)

    loop = math.hypot(xs[-1] - xs[0], ys[-1] - ys[0]) < 0.3
    ax.scatter([xs[0]], [ys[0]], s=70, color=INK, edgecolors=BG, linewidths=2, zorder=5)
    if not loop:
        ax.scatter([xs[-1]], [ys[-1]], s=70, marker="s", color=INK, edgecolors=BG, linewidths=2, zorder=5)
    marks = [(xs[0], ys[0], "start / finish" if loop else "start")] + ([] if loop else [(xs[-1], ys[-1], "finish")])
    r = renderer(fig)
    track = ax.transData.transform(pts)
    for mx, my, label in marks:
        best, best_n = None, None
        for dx, dy, ha, va in ((9, 7, "left", "bottom"), (-9, 7, "right", "bottom"), (9, -7, "left", "top"),
                               (-9, -7, "right", "top")):
            t = ax.annotate(label, (mx, my), xytext=(dx, dy), textcoords="offset points", ha=ha, va=va,
                            fontsize=8.5, color=INK2, zorder=6)
            b = t.get_window_extent(r)
            n = int(((track[:, 0] >= b.x0) & (track[:, 0] <= b.x1) & (track[:, 1] >= b.y0)
                     & (track[:, 1] <= b.y1)).sum())
            if best_n is None or n < best_n:
                if best is not None:
                    best.remove()
                best, best_n = t, n
            else:
                t.remove()
            if n == 0:
                break

    length = _scale_length(xspan)
    sx = xs.min()
    tr = blended_transform_factory(ax.transData, ax.transAxes)
    ax.plot([sx, sx + length], [-0.02, -0.02], transform=tr, color=MUTED, lw=1.5, solid_capstyle="butt",
            clip_on=False)
    ax.text(sx + length / 2, -0.035, f"{length:g} km", transform=tr, ha="center", va="top", fontsize=8,
            color=MUTED)
    header(ax, title, " · ".join(sub + ["north up"]))
    return save(fig, out)
