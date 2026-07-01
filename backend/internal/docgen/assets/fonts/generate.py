#!/usr/bin/env python3
"""Regenerate the bundled Loom Sans PDF fonts.

Loom Sans = the Go typeface (Bigelow & Holmes, BSD) with a small set of symbol
glyphs (checkmarks, crosses, ballot boxes, arrows, stars) grafted in from DejaVu
Sans (Bitstream Vera license). The Go fonts ship no dingbats, so a plain ✓/✗
renders as a .notdef box ("tofu") in gofpdf, which maroto uses for PDF export.
Grafting the missing glyphs lets those symbols render as real glyphs.

Check/cross *markers* are drawn as colour emoji images at render time (see
../emoji), so the graft only has to cover the mid-prose / miscellaneous cases and
symbols other than check/cross.

Deterministic: pinned upstream versions, no local state. Requires `fonttools`.

    pip install 'fonttools==4.55.3'
    python3 generate.py

Outputs GoPlus-{Regular,Bold,Italic,BoldItalic}.ttf next to this script. Commit
the results; the app embeds them via //go:embed.
"""
import io
import urllib.request
import tarfile

from fontTools import subset
from fontTools.merge import Merger
from fontTools.ttLib import TTFont

# Pinned sources.
GO_TAG = "v0.5.0"  # golang.org/x/image tag carrying the Go fonts (see backend/go.mod)
GO_BASE = f"https://raw.githubusercontent.com/golang/image/{GO_TAG}/font/gofont/ttfs"
DEJAVU_URL = ("https://github.com/dejavu-fonts/dejavu-fonts/releases/download/"
              "version_2_37/dejavu-fonts-ttf-2.37.tar.bz2")

# Canonical symbols to graft. Check/cross variants the model emits (✅ U+2705,
# ❌ U+274C, ✔, ✘, ✕, …) are normalised to U+2713 / U+2717 at render time, so
# only the canonical glyphs are needed here, plus a few common extras.
SYMS = [0x2713, 0x2714, 0x2717, 0x2718, 0x2716, 0x2611, 0x2612, 0x2610,
        0x2605, 0x2606, 0x2192, 0x2190, 0x2191, 0x2193, 0x21D2, 0x25A0,
        0x25A1, 0x25CF, 0x25CB, 0x2022, 0x26A0, 0x2699]

FACES = [
    ("Go-Regular.ttf", "GoPlus-Regular.ttf"),
    ("Go-Bold.ttf", "GoPlus-Bold.ttf"),
    ("Go-Italic.ttf", "GoPlus-Italic.ttf"),
    ("Go-Bold-Italic.ttf", "GoPlus-BoldItalic.ttf"),
]


def fetch(url):
    with urllib.request.urlopen(url) as r:
        return r.read()


def load_go_faces():
    return {name: TTFont(io.BytesIO(fetch(f"{GO_BASE}/{name}"))) for name, _ in FACES}


def load_dejavu():
    tar = tarfile.open(fileobj=io.BytesIO(fetch(DEJAVU_URL)), mode="r:bz2")
    member = next(m for m in tar.getmembers() if m.name.endswith("ttf/DejaVuSans.ttf"))
    return TTFont(io.BytesIO(tar.extractfile(member).read()))


def symbol_subset(dejavu):
    opt = subset.Options()
    opt.glyph_names = True
    s = subset.Subsetter(opt)
    s.populate(unicodes=SYMS)
    s.subset(dejavu)
    for tag in ("MATH", "FFTM", "GPOS", "GSUB", "GDEF", "BASE", "kern", "morx"):
        if tag in dejavu:
            del dejavu[tag]
    buf = io.BytesIO()
    dejavu.save(buf)
    return buf.getvalue()


def rename(font, family):
    for rec in font["name"].names:
        if rec.nameID in (1, 4, 16):
            rec.string = family
        elif rec.nameID == 6:
            rec.string = family.replace(" ", "")


def main():
    dejavu = load_dejavu()
    assert dejavu["head"].unitsPerEm == 2048, "unitsPerEm must match Go (2048)"
    sym_bytes = symbol_subset(dejavu)

    go = load_go_faces()
    for src, out in FACES:
        with open("_sym.ttf", "wb") as f:
            f.write(sym_bytes)
        with open("_base.ttf", "wb") as f:
            go[src].save(f)
        merged = Merger().merge(["_base.ttf", "_sym.ttf"])
        rename(merged, "Loom Sans")
        merged.save(out)
        print("wrote", out)


if __name__ == "__main__":
    main()
