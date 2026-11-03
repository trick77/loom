"""loomcharts: loom's ready-made activity charts (dark theme, PNG at dpi 200).

Each function reads the CSV files the Strava tools attach (a path or a DataFrame), writes
one PNG and returns its path. Write into out/ so the chart reaches the chat:

    import loomcharts
    loomcharts.ride_profile("strava_123_streams.csv", "out/profile.png", climbs="strava_123_climbs.csv")

Empty columns are fine: a chart leaves out what is missing (no power -> zones from heart
rate, no heart rate -> no HR panel) and writes a short notice image when nothing is left.
Every function takes title= to replace the default title.

Charts
    ride_profile(streams, out, climbs=None, title=None)
        elevation coloured by power zone (else HR zone), climbs labelled, HR panel below
    power_curve(curve, out, title=None, weeks=None)
        log-time mean-max power: this activity, window best, window median; dots = new bests
    rank_strip(baseline, out, title=None, weeks=None)
        each metric on its own strip: earlier activities, median, this one, value and rank
    weekly_load(weeks, out, metric="tss", sport_type=None, title=None)
        columns per week, latest highlighted, average of the earlier weeks; metric tss,
        moving_s, distance_km, gain_m or count; sport_type filters (default: all summed)
    same_route_gap(same_route, out, title=None)
        time gap to earlier rides of the same route by distance (below 0 = ahead)
    route_track(streams, out, color_by="watts", title=None)
        GPS track without a map, coloured by watts, speed_kmh, hr, altitude_m, cadence,
        power_zone or hr_zone
    activities_summary(activities, out, metric="distance_km", by="week", title=None)
        bars per week, month, sport_type or activity of a summed or averaged metric
    weeks= is the comparison window in weeks (it appears in labels: "6-week best");
    leave it out when the window is not known.

Own charts
    apply_style() sets the same dark style for plain matplotlib (pyplot) code; the
    colours are loomcharts.THIS, BEST, MEDIAN, ZONES (Z1..Z5+), CONTEXT, INK, INK2, MUTED.
    The chart functions above do not change global matplotlib settings.

CSV contract (5 s resample in streams; empty cell = no value)
    strava_<id>_streams.csv     elapsed_s, distance_km, lat, lng, altitude_m, speed_kmh, hr,
                                cadence, watts, grade_pct, temp_c, moving (0/1),
                                power_zone, hr_zone (1-5)
    strava_<id>_climbs.csv      n, from_km, to_km, len_km, gain_m, grade_pct, secs, vam,
                                avg_w, wkg, avg_hr
    strava_<id>_power_curve.csv duration_s, this_w, window_best_w, window_median_w
    strava_<id>_baseline.csv    activity_id, date, name, sport_type, is_this (1 = analysed),
                                same_route, distance_km, moving_s, avg_kmh, gain_m, np_w,
                                tss, ef, decoupling_pct, best_20m_w, avg_hr
    strava_<id>_same_route.csv  distance_km, gap_s_<YYYY-MM-DD> per earlier ride
                                (seconds behind; negative = ahead)
    strava_weeks.csv            week_start, iso_week, sport_type, count, distance_km,
                                moving_s, gain_m, tss (one row per week and sport)
    strava_activities.csv       activity_id, date, name, sport_type, trainer, distance_km,
                                moving_s, gain_m, avg_kmh, avg_hr, avg_w, weighted_w, kj,
                                suffer_score
"""

from ._style import BEST, BG, CONTEXT, INK, INK2, MEDIAN, MUTED, THIS, ZONES, apply_style
from .compare import power_curve, rank_strip, same_route_gap
from .ride import ride_profile, route_track
from .volume import activities_summary, weekly_load

__all__ = [
    "activities_summary", "apply_style", "power_curve", "rank_strip", "ride_profile", "route_track",
    "same_route_gap", "weekly_load", "BEST", "BG", "CONTEXT", "INK", "INK2", "MEDIAN", "MUTED", "THIS", "ZONES",
]
