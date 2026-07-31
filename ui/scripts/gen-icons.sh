#!/usr/bin/env bash
# Renders every favicon raster from the three SVG sources. Run it by hand after
# editing any of them and commit what it writes.
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
SRC="$ROOT/icons"
OUT="$ROOT/public"
MASTER="$OUT/icon.svg" # the master ships as-is; it is not a copy

for tool in rsvg-convert magick; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "gen-icons: $tool not found — brew install librsvg imagemagick" >&2
		exit 1
	fi
done

# --- from the master ---------------------------------------------------------
# -b none stays even though these come out opaque: the ground is the master's own
# background rect, not something the renderer supplies. Keeping the renderer out
# of it is what lets the check below catch a master that lost that rect.
rsvg-convert -b none -w 192 -h 192 "$MASTER" -o "$OUT/icon-192.png"
rsvg-convert -b none -w 512 -h 512 "$MASTER" -o "$OUT/icon-512.png"

# --- from the tiled sources --------------------------------------------------
# These two inset their glyph (60% and 46% vs the master's 96%) because the tile
# itself is visible: iOS masks it behind a superellipse, and Android crops the
# maskable one to a shape of the launcher's choosing, guaranteeing only the
# central 80% circle.
rsvg-convert -w 180 -h 180 "$SRC/icon-tile.svg" -o "$OUT/apple-touch-icon.png"
rsvg-convert -w 512 -h 512 "$SRC/icon-maskable.svg" -o "$OUT/icon-maskable-512.png"

# --- verify the grounds survived ---------------------------------------------
# Rendering can succeed and still produce the wrong thing — most plausibly a
# source that lost its background rect, which yields a touch icon iOS flattens
# onto black, or a favicon whose corners leak the tab bar's white. That fails
# silently in a viewer and only shows up on a real device, so assert every
# ground here instead.
#
# ALL FOUR are opaque. icon-192/512 were expected transparent back when
# ui/public/icon.svg let the browser composite the mark onto the tab; that is
# what put white in the favicon's corners on a light tab bar, so the master
# carries its own #1f1e1b now. If either of those two goes back to opaque=false,
# the master has lost its background rect — do not "fix" it by relaxing the
# check.
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
check_alpha "$OUT/icon-192.png" true
check_alpha "$OUT/icon-512.png" true
check_alpha "$OUT/apple-touch-icon.png" true
check_alpha "$OUT/icon-maskable-512.png" true

[[ "$fail" == 0 ]] || exit 1
echo "gen-icons: wrote $(cd "$OUT" && ls icon-*.png apple-touch-icon.png | tr '\n' ' ')"
