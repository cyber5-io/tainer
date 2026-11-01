# Tainer menu bar app (macOS)

Native SwiftUI `MenuBarExtra` app (no dock icon), bundle id `io.cyber5.tainer.menu`, macOS 14+.

    swift build
    swift test
    scripts/make-app.sh           # assembles build/Tainer Menu.app
    scripts/make-app.sh --check   # same, then asserts bundle structure without launching

Brand rasters in `Sources/TainerMenu/Resources/` are generated from `packaging/brand/` with
`swift packaging/brand/svg2png.swift <in.svg> <out.png> <height-px>`.
