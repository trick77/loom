#!/usr/bin/env bash
# Regenerates ui/public/apple-touch-icon.png from ui/public/favicon.svg.
#
# Run this by hand and commit the result — it is deliberately NOT wired into
# `npm run build` or CI, which have neither rsvg-convert nor ImageMagick.
#
# The background must be opaque: iOS/iPadOS flattens an apple-touch-icon's alpha
# channel onto white, which is where the white plate behind the tab icon on iPad
# came from. #1f1e1b is the <meta name="theme-color"> value in ui/index.html.
#
# favicon.svg's path runs to the edge of its 0 0 24 24 viewBox, so the glyph is
# rendered at GLYPH px and centred on a SIZE px tile rather than scaled straight
# to full bleed.
set -euo pipefail

SIZE=180
GLYPH=144 # ~80% of SIZE, leaving an even margin
BG="#1f1e1b"

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
public="$here/../public"
src="$public/favicon.svg"
out="$public/apple-touch-icon.png"

for bin in rsvg-convert magick; do
	if ! command -v "$bin" >/dev/null 2>&1; then
		echo "error: $bin not found. Install both: brew install librsvg imagemagick" >&2
		exit 1
	fi
done

tmpdir="$(mktemp -d -t gen-icons.XXXXXX)"
trap 'rm -rf "$tmpdir"' EXIT
tmp="$tmpdir/glyph.png"

# -strip and the excluded date chunks keep the output byte-reproducible: without
# them ImageMagick stamps the current time into the PNG, so re-running this on an
# unchanged favicon.svg would still show up as a binary diff in git.
rsvg-convert --width "$GLYPH" --height "$GLYPH" --output "$tmp" "$src"
magick -size "${SIZE}x${SIZE}" "xc:$BG" "$tmp" -gravity center -composite \
	-alpha remove -alpha off -strip \
	-define png:exclude-chunk=date,time "$out"

echo "wrote $out (${SIZE}x${SIZE}, opaque $BG)"
