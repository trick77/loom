#!/usr/bin/env bash
# Renders every favicon raster from the two SVG sources — ui/public/icon.svg and
# ui/icons/icon-maskable.svg. Run it by hand after editing either and commit what
# it writes.
#
# The outputs are COMMITTED rather than generated during the build, so neither
# `npm run build` nor CI needs an image toolchain. That is the whole reason this
# is not a package.json script.
#
# librsvg does the rasterising, not ImageMagick's own SVG support. ImageMagick is
# used only to re-read the results and assert their alpha. It is not used to
# composite anything: an earlier version of this script built the touch icon by
# laying a glyph over an ImageMagick-generated tile, which is why it needed
# -strip and png:exclude-chunk=date,time (ImageMagick stamps the current time
# into its output; librsvg does not). Nothing is composited now, so rsvg-convert
# writes each file directly and its output is already byte-identical run to run.
#
# There is no favicon.ico here. Loom declares an SVG icon in <head>, and the
# clients that go looking for a bare /favicon.ico are RSS readers, Windows
# bookmark thumbnails and old IE — none of which loom targets. backend/web
# answers that path with a 404 instead, along with /favicon.svg and the two
# web-app-manifest-*.png paths this icon set has moved away from.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/icons" # every source lives here now
OUT="$ROOT/public"
MASTER="$SRC/icon.svg"          # renders the rasters; never ships itself
FAVICON="$SRC/icon-favicon.svg" # the master on a ground; ships as-is
TAB='#33312c'                   # the tab icon's ground, see icon-favicon.svg

for tool in rsvg-convert magick; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "gen-icons: $tool not found — brew install librsvg imagemagick" >&2
		exit 1
	fi
done

# --- icon.svg: served directly as the modern tab icon ------------------------
# Its own source, not the master: Safari plates favicons on its favourites bar,
# so this one carries a ground where the master does not.
cp "$FAVICON" "$OUT/icon.svg"

# --- from the master ---------------------------------------------------------
# All three are transparent: the master carries no ground, and -b none keeps the
# renderer from inventing one. The touch icon is in here rather than rendered
# from its own inset source because with no visible tile there is nothing for an
# inset to breathe against — it is the same artwork, just larger. iOS's
# superellipse mask does not argue otherwise: the spiral is round, so the
# corners the mask cuts hold no ink.
rsvg-convert -b none -w 192 -h 192 "$MASTER" -o "$OUT/icon-192.png"
rsvg-convert -b none -w 512 -h 512 "$MASTER" -o "$OUT/icon-512.png"
rsvg-convert -b none -w 180 -h 180 "$MASTER" -o "$OUT/apple-touch-icon.png"

# --- the maskable source -----------------------------------------------------
# The one icon that still needs its own file, and for one reason only: Android
# CROPS an adaptive icon to the central 80% circle, so its glyph is inset to 46%
# where the master's runs to 96%. It is transparent like everything else, which
# means Android fills the remainder with a launcher colour rather than loom's —
# the same trade the master makes with Safari's white tab bar.
rsvg-convert -b none -w 512 -h 512 "$SRC/icon-maskable.svg" -o "$OUT/icon-maskable-512.png"

# --- verify the alpha ---------------------------------------------------------
# Rendering can succeed and still produce the wrong thing — most plausibly a
# source that grew a background rect, or a render that lost -b none and let the
# renderer supply a ground. Either fails silently in a viewer and only shows up
# on a real device, so assert it here instead.
#
# ALL FOUR are transparent: every SVG and PNG icon loom ships has a transparent
# background, and neither source carries a ground rect. The client composites
# the mark — onto white on Safari desktop, onto a launcher colour on Android.
# Both are accepted, not bugs. The opaque canvas this replaced put a hard dark
# square behind the mark everywhere the icon is drawn large, which is worse in
# every context that was actually looked at.
#
# So an opaque=true here means a source has grown a background rect back. Take
# the rect out; do not relax the check.
fail=0
check_alpha() { # <file> <expected true|false>
	local got
	# ImageMagick 7 prints "True"/"False"; 6 printed "true"/"false". Fold the
	# case so this script is not pinned to one major version.
	got="$(magick identify -format '%[opaque]' "$1" | tr '[:upper:]' '[:lower:]')"
	if [[ "$got" != "$2" ]]; then
		echo "gen-icons: $(basename "$1") is opaque=$got, expected $2" >&2
		fail=1
	fi
}
check_alpha "$OUT/icon-192.png" false
check_alpha "$OUT/icon-512.png" false
check_alpha "$OUT/apple-touch-icon.png" false
check_alpha "$OUT/icon-maskable-512.png" false

# icon.svg ships as SVG, so there is no raster to read — render one here just to
# assert it. Two samples, because it has to be a rounded tile and not a square:
# the canvas corner transparent, and a point on the tile clear of the spiral
# filled with $TAB.
#
# -alpha on before both reads. Without it a fully opaque image carries no alpha
# channel, and then %[hex:...] returns six digits instead of eight and
# %[fx:...a] does not report 1 — the comparisons would be measuring
# ImageMagick's channel bookkeeping rather than the icon.
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
rsvg-convert -b none -w 512 -h 512 "$OUT/icon.svg" -o "$TMP/icon-favicon.png"
fav_corner="$(magick "$TMP/icon-favicon.png" -alpha on -format '%[fx:p{0,0}.a]' info:)"
fav_ground="$(magick "$TMP/icon-favicon.png" -alpha on -format '%[hex:p{256,40}]' info: | tr '[:upper:]' '[:lower:]')"
if [[ "$fav_corner" != "0" ]]; then
	echo "gen-icons: icon.svg's canvas corner has alpha $fav_corner, expected 0" >&2
	fail=1
fi
if [[ "$fav_ground" != "${TAB#\#}ff" ]]; then
	echo "gen-icons: icon.svg's tile is #$fav_ground, expected ${TAB}ff" >&2
	fail=1
fi

[[ "$fail" == 0 ]] || exit 1
echo "gen-icons: wrote $(cd "$OUT" && ls icon-*.png apple-touch-icon.png | tr '\n' ' ')"
