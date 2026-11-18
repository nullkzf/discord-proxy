import AppKit

// usage: make-icon <out.png (1024)> <out.ico>
// An original icon (a green rounded square with two chat bubbles) — deliberately NOT Discord's logo.
// The .ico holds 16/24/32/48/64/128/256 px PNG images.
let a = CommandLine.arguments
let cs = CGColorSpace(name: CGColorSpace.sRGB)!

func render(_ size: Int) -> CGImage {
    let ctx = CGContext(data: nil, width: size, height: size, bitsPerComponent: 8, bytesPerRow: 0, space: cs,
                        bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    let s = CGFloat(size)
    let inset = s * 0.04
    let rect = CGRect(x: inset, y: inset, width: s - 2 * inset, height: s - 2 * inset)
    ctx.addPath(CGPath(roundedRect: rect, cornerWidth: s * 0.22, cornerHeight: s * 0.22, transform: nil))
    ctx.clip()
    let grad = CGGradient(colorsSpace: cs, colors: [
        CGColor(red: 0.30, green: 0.86, blue: 0.47, alpha: 1),
        CGColor(red: 0.08, green: 0.55, blue: 0.27, alpha: 1)] as CFArray, locations: [0, 1])!
    ctx.drawLinearGradient(grad, start: CGPoint(x: 0, y: s), end: CGPoint(x: s, y: 0), options: [])
    let cfg = NSImage.SymbolConfiguration(pointSize: s * 0.5, weight: .semibold)
    if let sym = NSImage(systemSymbolName: "bubble.left.and.bubble.right.fill", accessibilityDescription: nil)?
        .withSymbolConfiguration(cfg) {
        let tinted = NSImage(size: sym.size)
        tinted.lockFocus()
        sym.draw(at: .zero, from: .zero, operation: .sourceOver, fraction: 1)
        NSColor.white.set()
        NSRect(origin: .zero, size: sym.size).fill(using: .sourceIn)
        tinted.unlockFocus()
        if let cg = tinted.cgImage(forProposedRect: nil, context: nil, hints: nil) {
            let w = s * 0.62, h = w * CGFloat(cg.height) / CGFloat(cg.width)
            ctx.draw(cg, in: CGRect(x: (s - w) / 2, y: (s - h) / 2, width: w, height: h))
        }
    }
    return ctx.makeImage()!
}
func png(_ img: CGImage) -> Data {
    NSBitmapImageRep(cgImage: img).representation(using: .png, properties: [:])!
}
func le32(_ v: Int) -> [UInt8] { [UInt8(v & 255), UInt8((v >> 8) & 255), UInt8((v >> 16) & 255), UInt8((v >> 24) & 255)] }
func le16(_ v: Int) -> [UInt8] { [UInt8(v & 255), UInt8((v >> 8) & 255)] }

try! png(render(1024)).write(to: URL(fileURLWithPath: a[1]))

let sizes = [16, 24, 32, 48, 64, 128, 256]
let images = sizes.map { png(render($0)) }
var ico = [UInt8]() + le16(0) + le16(1) + le16(sizes.count)
var offset = 6 + 16 * sizes.count
for (i, s) in sizes.enumerated() {
    ico += [UInt8(s == 256 ? 0 : s), UInt8(s == 256 ? 0 : s), 0, 0] + le16(1) + le16(32) + le32(images[i].count) + le32(offset)
    offset += images[i].count
}
for img in images { ico += [UInt8](img) }
try! Data(ico).write(to: URL(fileURLWithPath: a[2]))
