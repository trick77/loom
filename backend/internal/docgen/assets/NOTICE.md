# Third-party assets bundled in the PDF generator

## Fonts — `fonts/GoPlus-*.ttf` ("Loom Sans")

Derived fonts embedded into the binary and used for all PDF body text.

Loom Sans is the **Go** typeface with a small set of symbol glyphs (checkmarks,
crosses, ballot boxes, arrows, stars) grafted in from **DejaVu Sans**. The Go
fonts carry no dingbats, so symbols like ✓/✗ would otherwise render as empty
`.notdef` boxes. Regenerate with `fonts/generate.py`.

- **Go fonts** © Bigelow & Holmes — BSD 3-Clause. See `fonts/LICENSE-Go.txt`.
- **DejaVu Sans** — Bitstream Vera / Arev license (permissive). See
  `fonts/LICENSE-DejaVu.txt`. The Bitstream Vera license forbids reusing the
  "Bitstream" / "Vera" names for modified fonts; the derived font is named
  "Loom Sans", so this is satisfied.

## Emoji — `emoji/check.png`, `emoji/cross.png`

Twemoji ✅ (U+2705) and ❌ (U+274C), drawn inline as the green-check / red-cross
status markers in tables and bullet lists.

- **Twemoji** © 2020 Twitter, Inc and other contributors — graphics licensed
  **CC-BY 4.0** (https://creativecommons.org/licenses/by/4.0/). See
  `emoji/LICENSE-Twemoji.txt`.
