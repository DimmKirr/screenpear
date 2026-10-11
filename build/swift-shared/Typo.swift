import CoreGraphics
import CoreText
import Foundation

/// Swift mirror of internal/typo: style measurements of a text crop so the
/// repaint matches weight, slant, size and placement. Candidates on Apple
/// platforms are the system font's weights and italic trait (SF), which is
/// what macOS and iOS screenshots are set in.
enum Typo {
    struct Style {
        var bold = false
        var italic = false
        var slantDeg: Double = 0
        var stroke: Double = 0      // median stroke width / ink height
        var inkTop: Int = 0         // first ink row inside the crop
        var inkHeight: Int = 0
    }

    /// Binary ink mask of a crop (true = text pixel). Otsu threshold on
    /// luminance, polarity from the border (the border is background).
    struct Ink {
        let width: Int
        let height: Int
        var bits: [Bool]
        subscript(x: Int, y: Int) -> Bool { bits[y * width + x] }

        var bounds: (minX: Int, minY: Int, maxX: Int, maxY: Int)? {
            var minX = Int.max, minY = Int.max, maxX = -1, maxY = -1
            for y in 0..<height { for x in 0..<width where self[x, y] {
                minX = min(minX, x); maxX = max(maxX, x); minY = min(minY, y); maxY = max(maxY, y)
            } }
            return maxX < 0 ? nil : (minX, minY, maxX + 1, maxY + 1)
        }
    }

    /// Transposed ink: a vertical (rotated) line measured as if upright, so
    /// slant and stroke mean the same thing for every orientation.
    static func transpose(_ ink: Ink) -> Ink {
        var bits = [Bool](repeating: false, count: ink.width * ink.height)
        for y in 0..<ink.height { for x in 0..<ink.width { bits[x * ink.height + y] = ink[x, y] } }
        return Ink(width: ink.height, height: ink.width, bits: bits)
    }

    static func binarize(width: Int, height: Int, luma: (Int, Int) -> Double) -> Ink {
        var hist = [Int](repeating: 0, count: 256)
        var values = [UInt8](repeating: 0, count: width * height)
        for y in 0..<height { for x in 0..<width {
            let v = UInt8(max(0, min(255, luma(x, y) * 255)))
            values[y * width + x] = v; hist[Int(v)] += 1
        } }
        let t = otsu(hist, total: width * height)
        var borderDark = 0, borderN = 0
        for x in 0..<width { for y in [0, height - 1] { if values[y * width + x] < t { borderDark += 1 }; borderN += 1 } }
        let darkBackground = borderDark * 2 > borderN
        return Ink(width: width, height: height, bits: values.map { ($0 < t) != darkBackground })
    }

    private static func otsu(_ hist: [Int], total: Int) -> UInt8 {
        var sum = 0.0
        for i in 0..<256 { sum += Double(i * hist[i]) }
        var sumB = 0.0, wB = 0.0, best = -1.0, bestT = 0
        for t in 0..<256 {
            wB += Double(hist[t]); if wB == 0 { continue }
            let wF = Double(total) - wB; if wF == 0 { break }
            sumB += Double(t * hist[t])
            let mB = sumB / wB, mF = (sum - sumB) / wF
            let v = wB * wF * (mB - mF) * (mB - mF)
            if v > best { best = v; bestT = t }
        }
        return UInt8(bestT)
    }

    /// Shear angle (degrees, positive = leaning right) that makes the
    /// vertical projection sharpest. Sweeps -6..30.
    static func slant(_ ink: Ink) -> Double {
        guard let b = ink.bounds else { return 0 }
        var best = -1.0, bestDeg = 0.0
        var deg = -6.0
        while deg <= 30 {
            let tanv = tan(deg * .pi / 180)
            var cols: [Int: Int] = [:]
            for y in b.minY..<b.maxY {
                let shift = Int((Double(b.maxY - 1 - y) * tanv).rounded())
                for x in b.minX..<b.maxX where ink[x, y] { cols[x - shift, default: 0] += 1 }
            }
            let sharp = cols.values.reduce(0.0) { $0 + Double($1 * $1) }
            if sharp > best { best = sharp; bestDeg = deg }
            deg += 1
        }
        return bestDeg
    }

    /// Median horizontal ink run / ink height: weight, independent of size.
    static func strokeRatio(_ ink: Ink) -> Double {
        guard let b = ink.bounds else { return 0 }
        var runs: [Int] = []
        for y in b.minY..<b.maxY {
            var run = 0
            for x in b.minX...b.maxX {
                if x < b.maxX, ink[x, y] { run += 1; continue }
                if run > 0 { runs.append(run); run = 0 }
            }
        }
        guard !runs.isEmpty else { return 0 }
        runs.sort()
        return Double(runs[runs.count / 2]) / Double(b.maxY - b.minY)
    }

    /// Full measurement of a crop. `boldThreshold` is page-relative: pass
    /// 1.25 × the median stroke ratio of all lines, or 0 for the absolute
    /// default (0.15).
    static func measure(_ ink: Ink, boldThreshold: Double = 0) -> Style {
        var s = Style()
        s.slantDeg = slant(ink)
        s.stroke = strokeRatio(ink)
        s.italic = s.slantDeg >= 6
        s.bold = s.stroke >= (boldThreshold > 0 ? boldThreshold : 0.15)
        if let b = ink.bounds { s.inkTop = b.minY; s.inkHeight = b.maxY - b.minY }
        return s
    }

    /// System font with the requested traits.
    static func font(size: CGFloat, bold: Bool, italic: Bool) -> CTFont {
        let base = CTFontCreateUIFontForLanguage(.system, size, nil) ?? CTFontCreateWithName("Helvetica" as CFString, size, nil)
        var traits: CTFontSymbolicTraits = []
        if bold { traits.insert(.boldTrait) }
        if italic { traits.insert(.italicTrait) }
        if traits.isEmpty { return base }
        return CTFontCreateCopyWithSymbolicTraits(base, size, nil, traits, [.boldTrait, .italicTrait]) ?? base
    }

    /// A CTLine for text in font with extra tracking between glyphs.
    static func line(_ text: String, font: CTFont, color: CGColor, tracking: CGFloat) -> CTLine {
        var attrs: [CFString: Any] = [kCTFontAttributeName: font, kCTForegroundColorAttributeName: color]
        if tracking != 0 { attrs[kCTKernAttributeName] = tracking as CFNumber }
        return CTLineCreateWithAttributedString(CFAttributedStringCreate(nil, text as CFString, attrs as CFDictionary)!)
    }

    /// Ink (image) bounds of a line: height, and top relative to baseline.
    static func inkBounds(of line: CTLine, ctx: CGContext) -> CGRect {
        CTLineGetImageBounds(line, ctx)
    }
}
