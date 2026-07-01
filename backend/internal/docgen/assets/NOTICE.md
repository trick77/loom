# Third-party assets bundled in the PDF generator

## Fonts — `fonts/Go-*.ttf`

The **Go** typeface (Go Regular/Bold/Italic and Go Mono), embedded into the binary
and uploaded to the Gotenberg renderer as `@font-face` assets so exported PDFs use
the brand typeface regardless of the sidecar's system fonts.

- **Go fonts** © Bigelow & Holmes — BSD 3-Clause. See `fonts/LICENSE-Go.txt`.

## Emoji & symbols

Rendered by the Gotenberg sidecar's Chromium (bundled **Noto Color Emoji**) — no
emoji assets are bundled here. Status markers (✓/✗) are drawn as brand-coloured
HTML, not images.
