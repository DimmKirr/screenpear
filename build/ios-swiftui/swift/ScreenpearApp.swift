import SwiftUI
import UIKit

/// iOS entry point: SwiftUI App lifecycle, no Wails, no Go. Images arrive
/// through the share sheet / Files ("Open in Screenpear", declared via
/// CFBundleDocumentTypes in Info.plist), PhotosPicker, or the clipboard.
///
/// Demo hook for the screenshot test (simctl has no tap primitive):
///   xcrun simctl launch booted com.automationd.screenpear \
///       -ScreenpearDemoImage /abs/path.png -ScreenpearDemoStep masked
/// loads the file on launch and, with step=masked, runs the mask too.
@main
struct ScreenpearApp: App {
    @State private var vm = ScreenpearViewModel()

    var body: some Scene {
        WindowGroup {
            ContentView(vm: vm)
                .onOpenURL { url in vm.load(url: url) }
                .task { await runDemoIfRequested() }
        }
    }

    private func runDemoIfRequested() async {
        let defaults = UserDefaults.standard
        guard let path = defaults.string(forKey: "ScreenpearDemoImage"), !path.isEmpty else { return }
        vm.load(url: URL(fileURLWithPath: path))
        guard defaults.string(forKey: "ScreenpearDemoStep") == "masked" else { return }
        // Wait for recognition to land, then mask.
        for _ in 0..<100 where vm.lines.isEmpty {
            try? await Task.sleep(for: .milliseconds(100))
        }
        vm.mask()
    }
}
