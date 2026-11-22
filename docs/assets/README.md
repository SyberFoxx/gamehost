# Assets

Images referenced by the root `README.md`. All artwork shares one palette: white and light blue
(`#FFFFFF`, `#BAE6FD`), celeste (`#7DD3FC`, `#38BDF8`, `#0EA5E9`), and navy bases (`#102030`,
`#0B1016`, `#1F3142`).

| File | Purpose | Source |
| --- | --- | --- |
| `gamehost-banner.svg` | header artwork and logotype | hand-authored vector, edit this first |
| `gamehost-banner-v4.png` | rendered header image | `rsvg-convert -w 1600 -h 440 gamehost-banner.svg -o gamehost-banner-v4.png` |
| `gamehost-title.svg` | wordmark and tagline below the header | hand-authored vector |
| `gamehost-title.png` | rendered wordmark | `rsvg-convert -w 1000 -h 210 gamehost-title.svg -o gamehost-title.png` |

Render the title as an image: GitHub strips inline `style="color:..."` attributes from README HTML,
so a colored `<h1>` shows as plain text. An `<img>` keeps the gradient in both light and dark mode.

When artwork changes, render it to a **new versioned filename** and update the `src` in `README.md`:
GitHub caches images by URL, so reusing a filename leaves viewers with the previous version. Keep the
source SVG and its current PNG in sync; remove superseded PNGs once no file references them.

The screenshots mentioned in older revisions (`screenshot-status.png`, `screenshot-list.png`) are
not committed yet. Their placeholders remain in the root README.
