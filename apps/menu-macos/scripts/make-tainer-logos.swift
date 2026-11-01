// Derives the menu app's tainer lockup rasters from the authentic source artwork.
//   swift scripts/make-tainer-logos.swift <source.png> <resources-dir>
// light: clean downscale to 60px tall (@2x of a 30pt header). dark: dark, low-chroma pixels
// (the near-black navy wordmark; HSV saturation is high for it, so chroma is used) are remapped to near-white; coloured pixels are left untouched.
import AppKit

let args = CommandLine.arguments
guard args.count == 3, let src = NSBitmapImageRep(data: try Data(contentsOf: URL(fileURLWithPath: args[1]))) else {
    FileHandle.standardError.write(Data("usage: make-tainer-logos.swift <source.png> <resources-dir>\n".utf8)); exit(1)
}
let outH = 60
let outW = Int((Double(src.pixelsWide) / Double(src.pixelsHigh) * Double(outH)).rounded())

func render() -> NSBitmapImageRep {
    let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: outW, pixelsHigh: outH, bitsPerSample: 8, samplesPerPixel: 4,
                               hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
    let ctx = NSGraphicsContext(bitmapImageRep: rep)!
    ctx.imageInterpolation = .high
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = ctx
    src.draw(in: NSRect(x: 0, y: 0, width: outW, height: outH))
    NSGraphicsContext.restoreGraphicsState()
    return rep
}

func save(_ rep: NSBitmapImageRep, _ name: String) throws {
    try rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: args[2]).appendingPathComponent(name))
}

let light = render()
try save(light, "logo-tainer-light@2x.png")

// Remap on the downscaled raster; premultiplied-safe via colorAt/setColor.
let dark = render()
let white = NSColor(deviceRed: 0.961, green: 0.969, blue: 0.980, alpha: 1)
for y in 0..<outH { for x in 0..<outW {
    guard let c = dark.colorAt(x: x, y: y)?.usingColorSpace(.deviceRGB), c.alphaComponent > 0 else { continue }
    let r = c.redComponent, g = c.greenComponent, b = c.blueComponent
    let mx = max(r, g, b), mn = min(r, g, b)
    let lum = 0.2126 * r + 0.7152 * g + 0.0722 * b
    let chroma = mx - mn
    if lum < 0.25 && chroma < 0.2 {
        dark.setColor(white.withAlphaComponent(c.alphaComponent), atX: x, y: y)
    }
} }
try save(dark, "logo-tainer-dark@2x.png")
print("wrote \(outW)x\(outH) light + dark")
