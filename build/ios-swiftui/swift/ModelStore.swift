import CryptoKit
import Foundation
import Observation

/// Downloadable model catalog with explicit consent, pinned SHA-256 digests
/// and a durable cache under Application Support. Nothing is fetched until
/// the user confirms in the consent sheet; the pins are copied verbatim from
/// go-textshot's models.go so iOS and desktop run identical weights.
@MainActor
@Observable
final class ModelStore {
    struct Model: Identifiable, Hashable {
        let id: String
        let name: String
        let url: URL
        let sha256: String
        let size: Int64
        let engine: EngineID
    }

    enum State: Equatable {
        case absent
        case downloading(Double)
        case verifying
        case ready
        case failed(String)
    }

    static let catalog: [Model] = [
        Model(id: "ppocrv6-det",
              name: "PP-OCRv6 small det",
              url: URL(string: "https://huggingface.co/PaddlePaddle/PP-OCRv6_small_det_onnx/resolve/28fe5895c24fd108c19eb3e8479f4ab385fbfc62/inference.onnx")!,
              sha256: "d73e0058b7a8086bbd57f3d10b8bcd4ff95363f67e06e2762b5e814fe9c9410e",
              size: 9_880_512, engine: .ppocr),
        Model(id: "ppocrv6-rec",
              name: "PP-OCRv6 small rec",
              url: URL(string: "https://huggingface.co/PaddlePaddle/PP-OCRv6_small_rec_onnx/resolve/b8f84f0b80c529de40b4fbb3544b84fa7233a513/inference.onnx")!,
              sha256: "5435fd747c9e0efe15a96d0b378d5bd157e9492ed8fd80edf08f30d02fa24634",
              size: 21_159_378, engine: .ppocr),
        Model(id: "migan-pipeline-v2",
              name: "MI-GAN inpainting (background fill)",
              url: URL(string: "https://huggingface.co/andraniksargsyan/migan/resolve/406830d0fa60666da0071c342ad2fbc8f30c5c64/migan_pipeline_v2.onnx")!,
              sha256: "6f1f3530a1a2324b19752018ce756088b07973cda8d7d890034ace5c8a48c40b",
              size: 28_079_181, engine: .ppocr),
    ]

    private(set) var states: [String: State] = [:]
    /// Set when a download is requested; the consent sheet binds to it.
    var pendingConsent: [Model] = []
    /// Remembered "always allow" answer; off by default so the first fetch
    /// of every install asks.
    var autoDownload: Bool {
        get { UserDefaults.standard.bool(forKey: "modelsAutoDownload") }
        set { UserDefaults.standard.set(newValue, forKey: "modelsAutoDownload") }
    }

    let directory: URL

    init() {
        let support = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        directory = support.appendingPathComponent("models", isDirectory: true)
        try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        for m in Self.catalog {
            states[m.id] = FileManager.default.fileExists(atPath: path(for: m).path) ? .ready : .absent
        }
    }

    func path(for model: Model) -> URL {
        directory.appendingPathComponent("\(model.id)-\(model.sha256.prefix(12)).onnx")
    }

    func state(of model: Model) -> State { states[model.id] ?? .absent }

    func missing(for engine: EngineID) -> [Model] {
        Self.catalog.filter { $0.engine == engine && state(of: $0) != .ready }
    }

    var totalMissingBytes: Int64 { pendingConsent.reduce(0) { $0 + $1.size } }

    /// Entry point for the UI: asks first unless "always allow" is on.
    func requestDownload(for engine: EngineID) {
        let missing = missing(for: engine)
        guard !missing.isEmpty else { return }
        if autoDownload {
            download(missing)
        } else {
            pendingConsent = missing
        }
    }

    func consentGiven(always: Bool) {
        if always { autoDownload = true }
        let models = pendingConsent
        pendingConsent = []
        download(models)
    }

    func consentDeclined() { pendingConsent = [] }

    func download(_ models: [Model]) {
        for m in models where state(of: m) != .ready {
            states[m.id] = .downloading(0)
            Task { await self.fetch(m) }
        }
    }

    func delete(_ model: Model) {
        try? FileManager.default.removeItem(at: path(for: model))
        states[model.id] = .absent
    }

    private func fetch(_ model: Model) async {
        do {
            let (tmp, response) = try await URLSession.shared.download(from: model.url, delegate: ProgressDelegate { [weak self] p in
                Task { @MainActor in self?.states[model.id] = .downloading(p) }
            })
            guard let http = response as? HTTPURLResponse, (200..<300).contains(http.statusCode) else {
                throw URLError(.badServerResponse)
            }
            states[model.id] = .verifying
            let digest = try await Task.detached { try Self.sha256(of: tmp) }.value
            guard digest == model.sha256 else {
                try? FileManager.default.removeItem(at: tmp)
                throw ModelError.digestMismatch(expected: model.sha256, got: digest)
            }
            let dest = path(for: model)
            try? FileManager.default.removeItem(at: dest)
            try FileManager.default.moveItem(at: tmp, to: dest)
            states[model.id] = .ready
        } catch {
            states[model.id] = .failed(error.localizedDescription)
        }
    }

    nonisolated static func sha256(of url: URL) throws -> String {
        let handle = try FileHandle(forReadingFrom: url)
        defer { try? handle.close() }
        var hasher = SHA256()
        while let chunk = try handle.read(upToCount: 1 << 20), !chunk.isEmpty {
            hasher.update(data: chunk)
        }
        return hasher.finalize().map { String(format: "%02x", $0) }.joined()
    }

    enum ModelError: LocalizedError {
        case digestMismatch(expected: String, got: String)
        var errorDescription: String? {
            switch self {
            case let .digestMismatch(expected, got):
                return "Downloaded file does not match the pinned SHA-256 (expected \(expected.prefix(12))…, got \(got.prefix(12))…). It was discarded."
            }
        }
    }
}

/// Reports download progress for URLSession.download(from:delegate:).
final class ProgressDelegate: NSObject, URLSessionTaskDelegate, URLSessionDownloadDelegate {
    private let onProgress: (Double) -> Void
    init(_ onProgress: @escaping (Double) -> Void) { self.onProgress = onProgress }

    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask,
                    didWriteData bytesWritten: Int64, totalBytesWritten: Int64, totalBytesExpectedToWrite: Int64) {
        guard totalBytesExpectedToWrite > 0 else { return }
        onProgress(Double(totalBytesWritten) / Double(totalBytesExpectedToWrite))
    }

    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didFinishDownloadingTo location: URL) {}
}
