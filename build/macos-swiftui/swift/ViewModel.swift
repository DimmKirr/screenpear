import AppKit
import Foundation
import SwiftUI
import UniformTypeIdentifiers

@MainActor
@Observable
final class ScreenpearViewModel {
    var imagePath: String = ""
    var image: NSImage?
    var analysis: Analysis?
    var excluded: Set<Int> = []
    var rulesText: String = ""
    var result: MaskResult?
    var status: String = "Connecting…"
    var isBusy = false
    var isError = false
    var engineReady = false

    private var client: APIClient?

    var rules: [Rule] { Rules.parse(rulesText) }
    var hasImage: Bool { image != nil }

    func start() {
        guard let port = APIClient.resolvePort() else {
            fail("No --port, SCREENPEAR_API_PORT or api-port file. Launch from the Screenpear tray app.")
            return
        }
        let client = APIClient(port: port)
        self.client = client
        Task {
            guard await client.health() else {
                fail("Screenpear backend is not answering on port \(port)")
                return
            }
            if let rules = try? await client.rules() { rulesText = Rules.format(rules) }
            status = "Ready"
            engineReady = true
        }
    }

    func openPanel() {
        let panel = NSOpenPanel()
        panel.allowedContentTypes = [.png, .jpeg, .gif]
        panel.allowsMultipleSelection = false
        panel.canChooseDirectories = false
        panel.title = "Choose a screenshot"
        if panel.runModal() == .OK, let url = panel.url {
            load(url: url)
        }
    }

    func load(url: URL) {
        guard let img = NSImage(contentsOf: url) else {
            fail("Cannot read \(url.lastPathComponent)")
            return
        }
        imagePath = url.path
        image = img
        analysis = nil
        result = nil
        excluded = []
        status = url.lastPathComponent
        isError = false
        analyze()
    }

    func analyze() {
        guard let client, !imagePath.isEmpty else { return }
        busy("Detecting text…")
        Task {
            do {
                let a = try await client.analyze(path: imagePath)
                analysis = a
                done("\(a.lines.count) lines in \(a.elapsedMs) ms")
            } catch {
                fail(error.localizedDescription)
            }
        }
    }

    func mask() {
        guard let client, let analysis else { return }
        busy("Masking…")
        let ids = analysis.lines.map(\.id).filter { !excluded.contains($0) }
        let req = MaskRequest(
            path: imagePath,
            rules: rules,
            lines: analysis.lines,
            lineIds: ids.count == analysis.lines.count ? nil : ids
        )
        Task {
            do {
                let r = try await client.mask(req)
                result = r
                if let img = NSImage(contentsOfFile: r.output) { image = img }
                done("\(r.replaced.count) replacements → \((r.output as NSString).lastPathComponent)")
            } catch {
                fail(error.localizedDescription)
            }
        }
    }

    func toggle(_ id: Int) {
        if excluded.contains(id) { excluded.remove(id) } else { excluded.insert(id) }
    }

    func reveal() {
        guard let result else { return }
        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: result.output)])
    }

    private func busy(_ s: String) { status = s; isBusy = true; isError = false }
    private func done(_ s: String) { status = s; isBusy = false; isError = false }
    private func fail(_ s: String) { status = s; isBusy = false; isError = true }
}
