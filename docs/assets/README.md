# Assets

Images referenced by the root `README.md`.

| File | Purpose | Source |
| --- | --- | --- |
| `gamehost-banner.svg` | header artwork and logotype | hand-authored vector, edit this first |
| `gamehost-banner-v3.png` | rendered header image | `rsvg-convert -w 1600 -h 440 gamehost-banner.svg -o gamehost-banner-v3.png` |

The root README renders `gamehost-banner-v3.png`. When the artwork changes, render it to a new
versioned filename and update the `src` in `README.md`: GitHub caches images by URL, so reusing the
same filename leaves viewers with the previous version.

The screenshots mentioned in older revisions (`screenshot-status.png`, `screenshot-list.png`) are
not committed yet. Their placeholders remain in the root README.
