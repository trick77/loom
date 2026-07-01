# Third-party assets bundled in the PDF generator

## Fonts — `fonts/Go-*.ttf`

The **Go** typeface (Go Regular/Bold/Italic) for body text and **JetBrains Mono**
(Regular/Bold) for code, embedded into the binary and uploaded to the Gotenberg
renderer as `@font-face` assets so exported PDFs use the brand fonts regardless of
the sidecar's system fonts.

- **Go fonts** © Bigelow & Holmes — BSD 3-Clause. See `fonts/LICENSE-Go.txt`.
- **JetBrains Mono** © The JetBrains Mono Project Authors — SIL Open Font License
  1.1. See `fonts/LICENSE-JetBrainsMono.txt`.

## Emoji & symbols

Rendered by the Gotenberg sidecar's Chromium (bundled **Noto Color Emoji**) — no
emoji assets are bundled here. Status markers (✓/✗) are drawn as brand-coloured
HTML, not images.
