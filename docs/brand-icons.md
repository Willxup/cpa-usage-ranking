# Project icons

The icon carries the application's empty-state motif: a warm-to-violet rounded
tile, three coral steps and a violet corner dot. The dot is part of the brand,
not a live status indicator.

Sources and exports:

- `web/public/assets/brand/project-icon.svg`: scalable project icon, transparent outside the rounded outer tile.
- `web/public/assets/brand/project-icon-512.png`: general project/repository artwork.
- `web/public/assets/brand/project-icon-192.png`: smaller application artwork.
- `web/public/assets/brand/project-icon-180.png`: Apple touch icon.
- `web/public/favicon.svg`: simplified, enlarged motif for small browser tabs.
- `web/public/assets/brand/favicon-{16,32,48}.png`: small raster variants.
- `web/public/assets/brand/favicon.ico`: combined 16/32/48px fallback.

The SVG masters are editable. Regenerate raster exports with the existing
Playwright Chromium installation (no additional runtime dependency):

```sh
npm --prefix web run browser:install
npm --prefix web run icons:generate
```

Exports are kept alongside the source; normal application builds do not need
to launch a browser. Vite copies the assets unchanged, and Go serves them under
`/assets/brand/` plus `/favicon.svg`.
