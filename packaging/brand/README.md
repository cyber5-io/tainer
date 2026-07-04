# Brand assets (build sources)

`tainer-logo-outlined-{dark,light}.svg` — the tainer.dev/ logo with the
wordmark **outlined to paths** (extracted from SF Mono via CoreText), so
rendering is identical on any machine with no font installed. The
`-dark` variant carries the light wordmark (for dark backgrounds), and
vice versa.

The upstream `tainer-logo.svg` on cyber5.io uses a live `<text>`
element and silently falls back to whatever font the renderer finds —
these outlined versions are the reproducible source for every raster
we ship (menu app rows, installer art). Regenerate rasters with the
native renderer:

    swift packaging/brand/svg2png.swift <in.svg> <out.png> <height-px>
