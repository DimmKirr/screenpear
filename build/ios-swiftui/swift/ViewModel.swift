import CoreGraphics
import Foundation
import ImageIO
import Observation
import PhotosUI
import SwiftUI
import UIKit

@MainActor
@Observable
final class ScreenpearViewModel {
    var original: CGImage?
    var masked: CGImage?
    var sourceName = ""
    var lines: [Line] = []
    var excluded: Set<Int> = []
    var rulesText = Rules.format(Rules.defaults)
    var replacements: [Masker.Replacement] = []
    var status = "Pick, paste or share a screenshot"
    var isBusy = false
    var isError = false
    var showMasked = true
    /// Lines no rule matches get generated same-shape text. Off means
    /// "dictionary only", the desktop app's original behaviour.
    var substituteUnmatched = true
    var engineID: EngineID = EngineID(rawValue: UserDefaults.standard.string(forKey: "engine") ?? "") ?? .vision {
        didSet { UserDefaults.standard.set(engineID.rawValue, forKey: "engine") }
    }

    let models = ModelStore()

    var rules: [Rule] { Rules.parse(rulesText) }
    /// Image size the boxes refer to, when they should be drawn (never over
    /// the masked result, where they would cover the repaint).
    var analysisForOverlay: (width: Int, height: Int)? {
        guard let original, !lines.isEmpty, !(showMasked && masked != nil) else { return nil }
        return (original.width, original.height)
    }
    var display: CGImage? { showMasked ? (masked ?? original) : original }
    var hasImage: Bool { original != nil }

    private var engine: OCREngine {
        switch engineID {
        case .vision: return VisionOCREngine()
        case .ppocr: return PPOCREngine(store: models)
        }
    }

    // MARK: input

    func load(_ image: UIImage, name: String) {
        guard let cg = image.normalized(maxSide: 4096) else {
            fail("Cannot decode \(name)")
            return
        }
        original = cg
        masked = nil
        lines = []
        excluded = []
        replacements = []
        sourceName = name
        showMasked = true
        isError = false
        analyze()
    }

    func load(url: URL) {
        let access = url.startAccessingSecurityScopedResource()
        defer { if access { url.stopAccessingSecurityScopedResource() } }
        guard let data = try? Data(contentsOf: url), let img = UIImage(data: data) else {
            fail("Cannot read \(url.lastPathComponent)")
            return
        }
        load(img, name: url.lastPathComponent)
    }

    func load(item: PhotosPickerItem?) {
        guard let item else { return }
        Task {
            if let data = try? await item.loadTransferable(type: Data.self), let img = UIImage(data: data) {
                load(img, name: "Photo")
            } else {
                fail("Could not load the selected photo")
            }
        }
    }

    func paste() {
        if let img = UIPasteboard.general.image {
            load(img, name: "Clipboard")
        } else {
            fail("Clipboard has no image")
        }
    }

    // MARK: pipeline

    func analyze() {
        guard let original else { return }
        if engineID == .ppocr, !models.missing(for: .ppocr).isEmpty {
            models.requestDownload(for: .ppocr)
            status = "PP-OCR models needed; confirm the download"
            return
        }
        busy("Detecting text…")
        let engine = engine
        Task {
            do {
                let found = try await engine.recognize(original)
                lines = found
                excluded = Set(found.filter(\.isSymbol).map(\.id))
                done("\(found.count) lines · \(engineID.title)")
            } catch {
                fail(error.localizedDescription)
            }
        }
    }

    func mask() {
        guard let original, !lines.isEmpty else { return }
        busy("Masking…")
        let only = excluded.isEmpty ? Set<Int>() : Set(lines.map(\.id)).subtracting(excluded)
        let rules = rules
        let src = original, lines = lines
        Task {
            // Pixel work off the main actor; the result lands back here.
            let substitute = substituteUnmatched
            let result = await Task.detached { Masker.apply(src, lines: lines, rules: rules, only: only, substituteUnmatched: substitute) }.value
            guard let result else { fail("Masking failed"); return }
            let (img, replaced) = result
            masked = img
            replacements = replaced
            showMasked = true
            done("\(replaced.count) replacements")
        }
    }

    /// Whether masking would change this line, for the UI highlight.
    func willChange(_ line: Line) -> Bool {
        Rules.wouldChange(line.text, rules) || (substituteUnmatched && line.text.contains(where: { $0.isLetter || $0.isNumber }))
    }

    func toggle(_ id: Int) {
        if excluded.contains(id) { excluded.remove(id) } else { excluded.insert(id) }
    }

    var maskedUIImage: UIImage? { masked.map { UIImage(cgImage: $0) } }

    func saveToPhotos() {
        guard let img = maskedUIImage else { return }
        UIImageWriteToSavedPhotosAlbum(img, nil, nil, nil)
        status = "Saved to Photos"
    }

    private func busy(_ s: String) { status = s; isBusy = true; isError = false }
    private func done(_ s: String) { status = s; isBusy = false; isError = false }
    private func fail(_ s: String) { status = s; isBusy = false; isError = true }
}

extension UIImage {
    /// Orientation-corrected CGImage capped to maxSide pixels on the longer
    /// edge. Uses ImageIO which applies the EXIF transform natively without
    /// allocating a full-resolution RGBA bitmap, and downsizes large camera
    /// photos so OCR and masking stay within iOS memory limits.
    func normalized(maxSide: Int) -> CGImage? {
        guard let data = jpegData(compressionQuality: 1.0) ?? pngData(),
              let source = CGImageSourceCreateWithData(data as CFData, nil) else {
            return cgImage
        }
        let opts: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: maxSide,
        ]
        return CGImageSourceCreateThumbnailAtIndex(source, 0, opts as CFDictionary)
    }
}
