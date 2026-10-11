import AppKit
import SwiftUI

/// The macOS main window. A separate process from the Go tray app: it
/// receives the loopback API port via --port (or the api-port file) and does
/// everything through services/api.go. Liquid Glass on macOS 26+, material
/// fallback below.
@main
struct ScreenpearApp: App {
    @State private var vm = ScreenpearViewModel()

    var body: some Scene {
        Window("Screenpear", id: "main") {
            root
                .onAppear { vm.start() }
        }
        .windowResizability(.contentMinSize)
        .defaultSize(width: 1100, height: 760)
        .windowStyle(.hiddenTitleBar)
        .commands {
            CommandGroup(replacing: .newItem) {
                Button("Open…") { vm.openPanel() }.keyboardShortcut("o")
            }
        }
    }

    @ViewBuilder
    private var root: some View {
        #if compiler(>=6.2)
        if #available(macOS 26.0, *) {
            GlassEffectContainer(spacing: 12) {
                ContentView(vm: vm)
            }
            .containerBackground(.thinMaterial, for: .window)
        } else {
            fallback
        }
        #else
        fallback
        #endif
    }

    private var fallback: some View {
        ContentView(vm: vm)
            .background(VisualEffectBackground(material: .underWindowBackground).ignoresSafeArea())
    }
}
