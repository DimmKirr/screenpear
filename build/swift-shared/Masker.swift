import CoreGraphics
import CoreText
import Foundation

/// Port of internal/mask: repaint every line a rule changes with the sampled
/// background and draw the substituted text back in place. Pure Core
/// Graphics, so the same file serves iOS and macOS.
enum Masker {
    struct Replacement: Identifiable, Hashable {
        let lineId: Int
        let from: String
        let to: String
        var id: Int { lineId }
    }

    /// Returns the masked image and what changed. `only` restricts masking
    /// to those line ids (the UI's per-line toggles); empty means all.
    /// With `substituteUnmatched`, lines no rule changes get generated text
    /// of the same shape (Substitute) instead of being left as they are.
    static func apply(_ image: CGImage, lines: [Line], rules: [Rule], only: Set<Int> = [], substituteUnmatched: Bool = true) -> (CGImage, [Replacement])? {
        let w = image.width, h = image.height
        let space = CGColorSpaceCreateDeviceRGB()
        guard let ctx = CGContext(data: nil, width: w, height: h, bitsPerComponent: 8, bytesPerRow: w * 4,
                                  space: space, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue),
              let data = ctx.data else { return nil }
        // Identity transform on purpose: a bitmap context stores row 0 as the
        // TOP row, so the pixel buffer is addressed with top-down y like the
        // OCR boxes, while CG drawing calls take bottom-up y and get a
        // one-line conversion (cg()) below. Flipping the CTM instead stores
        // the image upside down and makeImage() hands that back.
        ctx.draw(image, in: CGRect(x: 0, y: 0, width: w, height: h))
        let cg = { (r: CGRect) -> CGRect in CGRect(x: r.minX, y: CGFloat(h) - r.maxY, width: r.width, height: r.height) }

        let px = data.bindMemory(to: UInt8.self, capacity: w * h * 4)
        let pixel = { (x: Int, y: Int) -> (UInt8, UInt8, UInt8) in
            let i = (y * w + x) * 4
            return (px[i], px[i + 1], px[i + 2])
        }

        // Page-relative weight: median stroke ratio across the lines we read.
        let inks: [Int: Typo.Ink] = Dictionary(uniqueKeysWithValues: lines.compactMap { line -> (Int, Typo.Ink)? in
            let box = line.rect.intersection(CGRect(x: 0, y: 0, width: w, height: h)).integral
            guard !box.isEmpty else { return nil }
            let ink = Typo.binarize(width: Int(box.width), height: Int(box.height)) { x, y in
                let (r, g, b) = pixel(Int(box.minX) + x, Int(box.minY) + y)
                return 0.299 * Double(r) / 255 + 0.587 * Double(g) / 255 + 0.114 * Double(b) / 255
            }
            return (line.id, ink)
        })
        let strokes = inks.values.map(Typo.strokeRatio).filter { $0 > 0 }.sorted()
        let boldThreshold = strokes.isEmpty ? 0 : 1.25 * strokes[strokes.count / 2]

        var replaced: [Replacement] = []
        for line in lines {
            if !only.isEmpty, !only.contains(line.id) { continue }
            var (newText, changed) = Rules.rewrite(line.text, rules)
            if !changed, substituteUnmatched, line.text.contains(where: { $0.isLetter || $0.isNumber }) {
                newText = Substitute.text(for: line.text, seed: line.id)
                changed = true
            }
            guard changed else { continue }
            let box = line.rect.intersection(CGRect(x: 0, y: 0, width: w, height: h)).integral
            guard !box.isEmpty else { continue }

            let bg = borderColor(box: box, width: w, height: h, pixel: pixel)
            let fg = inkColor(box: box, bg: bg, pixel: pixel)
            let vertical = abs(line.angle) > .pi / 4
            let style = inks[line.id].map { Typo.measure(vertical ? Typo.transpose($0) : $0, boldThreshold: boldThreshold) } ?? Typo.Style()

            // Skewed lines: fill and draw in a space rotated about the box
            // centre so the repaint follows the original baseline.
            ctx.saveGState()
            let cgBox = cg(box)
            if abs(line.angle) > 0.005 {
                let centre = CGPoint(x: cgBox.midX, y: cgBox.midY)
                ctx.translateBy(x: centre.x, y: centre.y)
                ctx.rotate(by: CGFloat(-line.angle)) // CG y is up; image angle is screen-clockwise
                ctx.translateBy(x: -centre.x, y: -centre.y)
                // Grow the fill slightly so the rotated original is fully covered.
                ctx.setFillColor(red: bg.0, green: bg.1, blue: bg.2, alpha: 1)
                ctx.fill(cgBox.insetBy(dx: -2, dy: -2))
            } else {
                ctx.setFillColor(red: bg.0, green: bg.1, blue: bg.2, alpha: 1)
                ctx.fill(cgBox)
            }
            if vertical {
                // The context is rotated by the line angle, so lay the text out
                // in the box's own (long) axis: a box of the rotated frame.
                let centre = CGPoint(x: cgBox.midX, y: cgBox.midY)
                let upright = CGRect(x: centre.x - cgBox.height / 2, y: centre.y - cgBox.width / 2, width: cgBox.height, height: cgBox.width)
                drawFitted(newText, in: upright, style: style, color: fg, ctx: ctx)
            } else {
                drawFitted(newText, in: cgBox, style: style, color: fg, ctx: ctx)
            }
            ctx.restoreGState()
            replaced.append(Replacement(lineId: line.id, from: line.text, to: newText))
        }
        guard let out = ctx.makeImage() else { return nil }
        return (out, replaced)
    }

    private typealias RGB = (CGFloat, CGFloat, CGFloat)

    /// Most common colour on a 2 px ring just outside the box: the page
    /// background the text sits on. OCR boxes hug the glyphs, so the ring
    /// is almost never text.
    private static func borderColor(box: CGRect, width: Int, height: Int, pixel: (Int, Int) -> (UInt8, UInt8, UInt8)) -> RGB {
        let ring = box.insetBy(dx: -2, dy: -2).intersection(CGRect(x: 0, y: 0, width: width, height: height)).integral
        var counts: [UInt32: Int] = [:]
        for y in Int(ring.minY)..<Int(ring.maxY) {
            for x in Int(ring.minX)..<Int(ring.maxX) where !box.contains(CGPoint(x: x, y: y)) {
                let (r, g, b) = pixel(x, y)
                counts[UInt32(r) << 16 | UInt32(g) << 8 | UInt32(b), default: 0] += 1
            }
        }
        guard let best = counts.max(by: { $0.value < $1.value })?.key else { return (1, 1, 1) }
        return (CGFloat(best >> 16 & 0xff) / 255, CGFloat(best >> 8 & 0xff) / 255, CGFloat(best & 0xff) / 255)
    }

    /// Ink colour: the mean of the pixels that differ most from the
    /// background (top 15% by luminance distance). A single extreme pixel is
    /// an anti-aliasing artefact; the average of the darkest/lightest band
    /// is the glyph colour as rendered.
    private static func inkColor(box: CGRect, bg: RGB, pixel: (Int, Int) -> (UInt8, UInt8, UInt8)) -> RGB {
        let bgL = luma(bg)
        var samples: [(CGFloat, RGB)] = []
        for y in Int(box.minY)..<Int(box.maxY) {
            for x in Int(box.minX)..<Int(box.maxX) {
                let (r, g, b) = pixel(x, y)
                let c: RGB = (CGFloat(r) / 255, CGFloat(g) / 255, CGFloat(b) / 255)
                samples.append((abs(luma(c) - bgL), c))
            }
        }
        guard !samples.isEmpty else { return bgL > 0.5 ? (0.13, 0.13, 0.13) : (0.93, 0.93, 0.93) }
        samples.sort { $0.0 > $1.0 }
        let band = samples.prefix(max(1, samples.count * 15 / 100)).filter { $0.0 > 0.15 }
        guard !band.isEmpty else { return bgL > 0.5 ? (0.13, 0.13, 0.13) : (0.93, 0.93, 0.93) }
        let n = CGFloat(band.count)
        return (band.reduce(0) { $0 + $1.1.0 } / n, band.reduce(0) { $0 + $1.1.1 } / n, band.reduce(0) { $0 + $1.1.2 } / n)
    }

    private static func luma(_ c: RGB) -> CGFloat { 0.299 * c.0 + 0.587 * c.1 + 0.114 * c.2 }

    /// Repaint in the system font with the measured weight and slant, at the
    /// size whose ink height equals the original ink height, with the ink
    /// top on the original ink top, and tracking so the run spans the full
    /// box width (text wider than the box is scaled down to fit).
    /// `box` is in CG (bottom-up) coordinates.
    private static func drawFitted(_ text: String, in box: CGRect, style: Typo.Style, color: RGB, ctx: CGContext) {
        let cgColor = CGColor(colorSpace: CGColorSpaceCreateDeviceRGB(), components: [color.0, color.1, color.2, 1])!
        let targetInkHeight = CGFloat(max(style.inkHeight, 1))
        // Ink height is linear in point size for one string: probe once.
        let probe: CGFloat = 64
        let probeLine = Typo.line(text, font: Typo.font(size: probe, bold: style.bold, italic: style.italic), color: cgColor, tracking: 0)
        let probeInk = Typo.inkBounds(of: probeLine, ctx: ctx)
        var size = probeInk.height > 0 ? probe * targetInkHeight / probeInk.height : box.height * 0.82
        var font = Typo.font(size: size, bold: style.bold, italic: style.italic)
        var lineRef = Typo.line(text, font: font, color: cgColor, tracking: 0)
        var width = CGFloat(CTLineGetTypographicBounds(lineRef, nil, nil, nil))
        var tracking: CGFloat = 0
        let glyphs = max(text.count, 1)
        if width > box.width {
            size *= box.width / width
            font = Typo.font(size: size, bold: style.bold, italic: style.italic)
        } else if glyphs > 1 {
            // OCR boxes carry padding: track up to 0.12 em, centre the rest.
            tracking = min((box.width - width) / CGFloat(glyphs - 1), 0.12 * size)
        }
        lineRef = Typo.line(text, font: font, color: cgColor, tracking: tracking)
        width = CGFloat(CTLineGetTypographicBounds(lineRef, nil, nil, nil))
        let left = box.minX + max(0, (box.width - width) / 2)
        // Place the ink top on the original ink top: ink bounds are relative
        // to the baseline, so baseline = top-of-box - inkTop - inkBounds.maxY.
        let ink = Typo.inkBounds(of: lineRef, ctx: ctx)
        let inkTopY = box.maxY - CGFloat(style.inkTop)      // CG y of the original ink top
        let baseline = inkTopY - ink.maxY
        ctx.saveGState()
        ctx.textMatrix = .identity
        ctx.textPosition = CGPoint(x: left, y: baseline)
        CTLineDraw(lineRef, ctx)
        ctx.restoreGState()
    }

    /// Size follows the box height and shrinks until the string fits the
    /// width; drawn left-aligned, vertically centred, in the system font.
    /// `box` is in CG (bottom-up) coordinates.
    private static func drawText(_ text: String, in box: CGRect, color: RGB, ctx: CGContext) {
        var size = max(box.height * 0.82, 6)
        let cgColor = CGColor(colorSpace: CGColorSpaceCreateDeviceRGB(), components: [color.0, color.1, color.2, 1])!
        while true {
            let font = CTFontCreateUIFontForLanguage(.system, size, nil) ?? CTFontCreateWithName("Helvetica" as CFString, size, nil)
            let attrs: [CFString: Any] = [kCTFontAttributeName: font, kCTForegroundColorAttributeName: cgColor]
            let attributed = CFAttributedStringCreate(nil, text as CFString, attrs as CFDictionary)!
            let lineRef = CTLineCreateWithAttributedString(attributed)
            var ascent: CGFloat = 0, descent: CGFloat = 0
            let width = CGFloat(CTLineGetTypographicBounds(lineRef, &ascent, &descent, nil))
            if width <= box.width || size <= 6 {
                // Centre the ascent..descent span inside the box (bottom-up).
                let baseline = box.minY + (box.height - ascent - descent) / 2 + descent
                ctx.saveGState()
                ctx.textMatrix = .identity
                ctx.textPosition = CGPoint(x: box.minX, y: baseline)
                CTLineDraw(lineRef, ctx)
                ctx.restoreGState()
                return
            }
            size -= 1
        }
    }
}
