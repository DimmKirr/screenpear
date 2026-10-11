import CoreGraphics
import Foundation
import Vision

/// What the app needs from a text recognizer. Two implementations: Apple
/// Vision (on device, ships with iOS, nothing to download) and PP-OCRv6
/// (the desktop app's models, downloaded on demand through ModelStore).
protocol OCREngine: Sendable {
    var id: EngineID { get }
    func recognize(_ image: CGImage) async throws -> [Line]
}

enum EngineID: String, CaseIterable, Codable, Identifiable {
    case vision
    case ppocr
    var id: String { rawValue }

    var title: String {
        switch self {
        case .vision: return "Apple Vision"
        case .ppocr: return "PP-OCRv6"
        }
    }

    var summary: String {
        switch self {
        case .vision: return "Built into iOS. On device, no download."
        case .ppocr: return "Same models as the desktop app (~30 MB). Downloaded once, on your say-so."
        }
    }
}

/// Apple Vision text recognition. Boxes come back normalised with a
/// bottom-left origin; converted here to image pixels, top-left, so the
/// rest of the app (and the Go side) share one coordinate system.
struct VisionOCREngine: OCREngine {
    let id = EngineID.vision

    func recognize(_ image: CGImage) async throws -> [Line] {
        var lines = try recognizeUpright(image)
        // Orientation sweep: Vision reads left-to-right only, so a vertical
        // label is invisible. Turn the image upright for each direction and
        // keep the hits that do not overlap an upright line, tagged with
        // their reading angle (radians; +pi/2 reads top-to-bottom).
        let w = image.width, h = image.height
        for (turn, angle) in [(Rotation.ccw, Double.pi / 2), (Rotation.cw, -Double.pi / 2)] {
            guard let turned = rotate(image, turn) else { continue }
            for found in try recognizeUpright(turned) {
                let r = unrotate(found.rect, width: w, height: h, turn: turn)
                if lines.contains(where: { overlapsHalf($0.rect, r) }) { continue }
                lines.append(Line(id: lines.count, text: found.text, score: found.score,
                                  x0: Int(r.minX), y0: Int(r.minY), x1: Int(r.maxX), y1: Int(r.maxY), angle: angle))
            }
        }
        return lines
    }

    private func recognizeUpright(_ image: CGImage) throws -> [Line] {
        let request = VNRecognizeTextRequest()
        request.recognitionLevel = .accurate
        request.usesLanguageCorrection = false
        let handler = VNImageRequestHandler(cgImage: image, options: [:])
        try handler.perform([request])
        let w = CGFloat(image.width), h = CGFloat(image.height)
        var lines: [Line] = []
        for obs in request.results ?? [] {
            guard let candidate = obs.topCandidates(1).first else { continue }
            let b = obs.boundingBox
            let rect = CGRect(x: b.minX * w, y: (1 - b.maxY) * h, width: b.width * w, height: b.height * h).integral
            // Baseline skew from the quad's top edge, in image (y-down) space.
            let dx = (obs.topRight.x - obs.topLeft.x) * w
            let dy = -(obs.topRight.y - obs.topLeft.y) * h
            let angle = dx == 0 ? 0 : atan2(dy, dx)
            lines.append(Line(id: lines.count, text: candidate.string, score: candidate.confidence,
                              x0: Int(rect.minX), y0: Int(rect.minY), x1: Int(rect.maxX), y1: Int(rect.maxY),
                              angle: Double(angle)))
        }
        // Reading order: top to bottom, then left to right, like textshot.
        lines.sort { a, b in a.y0 != b.y0 ? a.y0 < b.y0 : a.x0 < b.x0 }
        return lines.enumerated().map { i, l in
            Line(id: i, text: l.text, score: l.score, x0: l.x0, y0: l.y0, x1: l.x1, y1: l.y1, angle: l.angle)
        }
    }
}

enum Rotation { case cw, ccw }

/// A copy of the image turned a quarter turn.
func rotate(_ image: CGImage, _ turn: Rotation) -> CGImage? {
    let w = image.width, h = image.height
    guard let ctx = CGContext(data: nil, width: h, height: w, bitsPerComponent: 8, bytesPerRow: h * 4,
                              space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { return nil }
    ctx.translateBy(x: CGFloat(h) / 2, y: CGFloat(w) / 2)
    ctx.rotate(by: turn == .ccw ? .pi / 2 : -.pi / 2)
    ctx.draw(image, in: CGRect(x: -CGFloat(w) / 2, y: -CGFloat(h) / 2, width: CGFloat(w), height: CGFloat(h)))
    return ctx.makeImage()
}

/// Maps a box found on the turned image back onto the original (y-down).
func unrotate(_ r: CGRect, width w: Int, height h: Int, turn: Rotation) -> CGRect {
    switch turn {
    case .ccw: // x' = y, y' = w-1-x
        return CGRect(x: CGFloat(w) - r.maxY, y: r.minX, width: r.height, height: r.width)
    case .cw: // x' = h-1-y, y' = x
        return CGRect(x: r.minY, y: CGFloat(h) - r.maxX, width: r.height, height: r.width)
    }
}

func overlapsHalf(_ a: CGRect, _ b: CGRect) -> Bool {
    let i = a.intersection(b)
    guard !i.isNull, !i.isEmpty else { return false }
    return i.width * i.height * 2 > min(a.width * a.height, b.width * b.height)
}

/// PP-OCRv6 through ONNX Runtime. The models are data and are fetched by
/// ModelStore after consent; the runtime itself must be linked into the app
/// (onnxruntime-c xcframework) because iOS cannot load downloaded code.
///
/// Inference is not wired yet: this engine validates that the models are
/// present and reports the exact missing piece, so the UI and the download
/// flow are real while the ONNX session is a follow-up (KIRR step 2).
struct PPOCREngine: OCREngine {
    let id = EngineID.ppocr
    let store: ModelStore

    func recognize(_ image: CGImage) async throws -> [Line] {
        let missing = await store.missing(for: .ppocr)
        if !missing.isEmpty {
            throw OCRError.modelsMissing(missing.map(\.name))
        }
        throw OCRError.runtimeNotLinked
    }
}

enum OCRError: LocalizedError {
    case modelsMissing([String])
    case runtimeNotLinked

    var errorDescription: String? {
        switch self {
        case .modelsMissing(let names):
            return "PP-OCR models not downloaded: \(names.joined(separator: ", ")). Open Models to fetch them."
        case .runtimeNotLinked:
            return "PP-OCR models are ready but ONNX Runtime is not linked into this build yet. Apple Vision is active instead."
        }
    }
}
