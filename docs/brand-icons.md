# Project icons

The icon carries the application's empty-state motif: a warm-to-violet rounded
tile, three coral steps and a violet corner dot. The dot is part of the brand,
not a live status indicator.

Sources and exports:

- `assets/ranking-logo-light.svg` and `assets/ranking-logo-dark.svg`: horizontal README banner logos with wordmark for light and dark themes.
- `assets/ranking-logo.svg` and `web/public/assets/brand/logo.svg`: standalone frameless brand mark used in the web header and footer.
- `web/public/assets/brand/project-icon.svg`: single-tile rounded application icon master.
- `web/public/assets/brand/project-icon-512.png`: general project/repository artwork.
- `web/public/assets/brand/project-icon-192.png`: smaller application artwork.
- `web/public/assets/brand/project-icon-180.png`: Apple touch icon.
- `web/public/favicon.svg`: simplified, enlarged motif for small browser tabs.
- `web/public/assets/brand/favicon-{16,32,48}.png`: small raster variants.
- `web/public/assets/brand/favicon.ico`: combined 16/32/48px fallback.

The SVG masters are editable. With Chromium available to Playwright, regenerate
raster exports using:

```sh
npm --prefix web run icons:generate
```

The generator uses Playwright's default Chromium lookup. If the required browser
is unavailable, `npm --prefix web run browser:install` installs it and may download
browser files; it is not required when a compatible installation is already available.

Exports are kept alongside the source; normal application builds do not need
to launch a browser. Vite copies the assets unchanged, and Go serves them under
`/assets/brand/` plus `/favicon.svg`.
