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
# used only to re-read the results and assert their grounds. It is not used to
# composite anything: an earlier version of this script built the touch icon by
# laying a transparent glyph over an ImageMagick-generated tile, which is why it
# needed -strip and png:exclude-chunk=date,time (ImageMagick stamps the current
# time into its output; librsvg does not). The grounds now come from the sources
# themselves, so rsvg-convert writes each file directly and its output is already
# byte-identical run to run.
#
# There is no favicon.ico here. Loom declares an SVG icon in <head>, and the
# clients that go looking for a bare /favicon.ico are RSS readers, Windows
# bookmark thumbnails and old IE — none of which loom targets. backend/web
# answers that path with a 404 instead, along with /favicon.svg and the two
# web-app-manifest-*.png paths this icon set has moved away from.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/icons" # only the maskable source lives here now
OUT="$ROOT/public"
MASTER="$OUT/icon.svg" # the master ships as-is; it is not a copy

for tool in rsvg-convert magick; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "gen-icons: $tool not found — brew install librsvg imagemagick" >&2
		exit 1
	fi
done

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
# The one icon that still needs its own file, for two reasons the master cannot
# serve at once: the glyph is inset to 46% because Android CROPS an adaptive
# icon to the central 80% circle, and the ground is opaque because whatever is
# transparent there gets filled with a launcher colour rather than loom's.
rsvg-convert -w 512 -h 512 "$SRC/icon-maskable.svg" -o "$OUT/icon-maskable-512.png"

# --- verify the grounds survived ---------------------------------------------
# Rendering can succeed and still produce the wrong thing — most plausibly a
# source that lost its background rect, which yields a touch icon iOS flattens
# onto black, or a favicon whose corners leak the tab bar's white. That fails
# silently in a viewer and only shows up on a real device, so assert every
# ground here instead.
#
# Only the maskable one is opaque. The other three come from ui/public/icon.svg,
# which carries no ground on purpose: the client composites the mark, and on
# Safari desktop that means onto white. That is accepted, not a bug — the opaque
# canvas it replaced put a hard dark square behind the mark everywhere the icon
# is drawn large. If any of the three reports opaque=true, the master has grown
# a background rect back; if the maskable one reports false, it has lost its
# own. Neither is fixed by relaxing the check.
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
check_alpha "$OUT/icon-maskable-512.png" true

[[ "$fail" == 0 ]] || exit 1
echo "gen-icons: wrote $(cd "$OUT" && ls icon-*.png apple-touch-icon.png | tr '\n' ' ')"
