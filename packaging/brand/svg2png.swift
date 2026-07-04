import AppKit

// svg2png <in.svg> <out.png> <height>
let args = CommandLine.arguments
guard args.count == 4, let h = Int(args[3]),
      let img = NSImage(contentsOfFile: args[1]) else {
    FileHandle.standardError.write("usage/load failure\n".data(using: .utf8)!)
    exit(1)
}
let aspect = img.size.width / img.size.height
let w = Int((CGFloat(h) * aspect).rounded())
let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: w, pixelsHigh: h,
    bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
    colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
img.draw(in: NSRect(x: 0, y: 0, width: w, height: h),
         from: .zero, operation: .copy, fraction: 1.0)
NSGraphicsContext.restoreGraphicsState()
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: args[2]))
print("\(args[2]) \(w)x\(h)")
