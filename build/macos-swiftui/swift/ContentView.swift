import SwiftUI
import UniformTypeIdentifiers

private let accent = Color(red: 0x7a / 255.0, green: 0xc9 / 255.0, blue: 0x43 / 255.0)

struct ContentView: View {
    @Bindable var vm: ScreenpearViewModel

    var body: some View {
        HSplitView {
            stage
                .frame(minWidth: 480, maxWidth: .infinity, maxHeight: .infinity)
            sidebar
                .frame(minWidth: 300, idealWidth: 340, maxWidth: 420, maxHeight: .infinity)
        }
        .toolbar { toolbarItems }
        .onDrop(of: [.fileURL], isTargeted: nil) { providers in
            guard let p = providers.first else { return false }
            _ = p.loadObject(ofClass: URL.self) { url, _ in
                if let url { Task { @MainActor in vm.load(url: url) } }
            }
            return true
        }
    }

    // MARK: toolbar

    @ToolbarContentBuilder
    private var toolbarItems: some ToolbarContent {
        ToolbarItemGroup(placement: .primaryAction) {
            Button { vm.openPanel() } label: { Label("Open", systemImage: "photo.badge.plus") }
                .keyboardShortcut("o")
            Button { vm.analyze() } label: { Label("Analyze", systemImage: "text.viewfinder") }
                .disabled(!vm.hasImage || vm.isBusy)
            Button { vm.mask() } label: { Label("Mask", systemImage: "eye.slash") }
                .disabled(vm.analysis == nil || vm.isBusy)
                .keyboardShortcut(.return, modifiers: .command)
        }
        ToolbarItem(placement: .status) {
            HStack(spacing: 6) {
                if vm.isBusy { ProgressView().controlSize(.small) }
                Text(vm.status)
                    .font(.system(size: 12))
                    .foregroundStyle(vm.isError ? .red : .secondary)
                    .lineLimit(1)
            }
        }
    }

    // MARK: stage

    @ViewBuilder
    private var stage: some View {
        if let image = vm.image {
            Image(nsImage: image)
                .resizable()
                .interpolation(.high)
                .scaledToFit()
                .clipShape(RoundedRectangle(cornerRadius: 8))
                .shadow(color: .black.opacity(0.45), radius: 24, y: 10)
                .overlay {
                    if let a = vm.analysis {
                        GeometryReader { geo in
                            boxes(a, scale: geo.size.width / CGFloat(max(a.width, 1)))
                        }
                    }
                }
                .padding(16)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            dropHint
        }
    }

    private var dropHint: some View {
        VStack(spacing: 6) {
            Image(systemName: "arrow.down.doc")
                .font(.system(size: 36, weight: .light))
                .foregroundStyle(.secondary)
            Text("Drop a screenshot here")
                .font(.title3.weight(.medium))
            Text("or press ⌘O — PNG, JPEG or GIF")
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .overlay(
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .strokeBorder(style: StrokeStyle(lineWidth: 1.5, dash: [6, 5]))
                .foregroundStyle(.secondary.opacity(0.4))
                .padding(20)
        )
    }

    private func boxes(_ a: Analysis, scale: CGFloat) -> some View {
        let rules = vm.rules
        return ZStack(alignment: .topLeading) {
            ForEach(a.lines) { line in
                let hit = Rules.wouldChange(line.text, rules)
                let on = !vm.excluded.contains(line.id)
                Rectangle()
                    .fill(hit && on ? accent.opacity(0.18) : Color.blue.opacity(on ? 0.08 : 0))
                    .overlay(Rectangle().strokeBorder(
                        hit && on ? accent : (on ? Color.blue.opacity(0.7) : Color.white.opacity(0.25)),
                        style: StrokeStyle(lineWidth: 1.5, dash: on ? [] : [4, 3])))
                    .frame(width: CGFloat(line.x1 - line.x0) * scale, height: CGFloat(line.y1 - line.y0) * scale)
                    .offset(x: CGFloat(line.x0) * scale, y: CGFloat(line.y0) * scale)
                    .contentShape(Rectangle())
                    .onTapGesture { vm.toggle(line.id) }
                    .help(line.text)
            }
        }
    }

    // MARK: sidebar

    private var sidebar: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                linesCard
                rulesCard
                if vm.result != nil { resultCard }
            }
            .padding(12)
        }
    }

    private func header(_ title: String, trailing: String? = nil) -> some View {
        HStack {
            Text(title.uppercased())
                .font(.system(size: 11, weight: .semibold, design: .rounded))
                .tracking(0.5)
                .foregroundStyle(.secondary)
            Spacer()
            if let trailing {
                Text(trailing).font(.system(size: 11)).foregroundStyle(.secondary)
            }
        }
    }

    private var linesCard: some View {
        VStack(alignment: .leading, spacing: 6) {
            header("Detected text", trailing: "\(vm.analysis?.lines.count ?? 0) lines")
            if let a = vm.analysis {
                let rules = vm.rules
                VStack(spacing: 0) {
                    ForEach(a.lines) { line in
                        let hit = Rules.wouldChange(line.text, rules)
                        let on = !vm.excluded.contains(line.id)
                        HStack(spacing: 8) {
                            Toggle("", isOn: Binding(get: { on }, set: { _ in vm.toggle(line.id) }))
                                .toggleStyle(.checkbox)
                                .labelsHidden()
                            Text(line.text)
                                .lineLimit(1)
                                .foregroundStyle(hit ? accent : .primary)
                                .textSelection(.enabled)
                            Spacer(minLength: 4)
                            Text(String(format: "%.2f", line.score))
                                .font(.system(size: 11, design: .monospaced))
                                .foregroundStyle(.secondary)
                        }
                        .opacity(on ? 1 : 0.45)
                        .padding(.vertical, 3)
                    }
                }
            } else {
                Text("Open an image to detect text.").foregroundStyle(.secondary)
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .cardSurface()
    }

    private var rulesCard: some View {
        VStack(alignment: .leading, spacing: 6) {
            header("Replacement rules", trailing: "one from=to per line")
            TextEditor(text: $vm.rulesText)
                .font(.system(size: 12, design: .monospaced))
                .scrollContentBackground(.hidden)
                .frame(minHeight: 150)
                .padding(6)
                .background(RoundedRectangle(cornerRadius: 8).fill(.black.opacity(0.2)))
        }
        .padding(12)
        .cardSurface()
    }

    private var resultCard: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                header("Result")
                Button("Reveal") { vm.reveal() }.controlSize(.small)
            }
            if let r = vm.result {
                Text(r.output)
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                ForEach(r.replaced) { rep in
                    HStack(spacing: 6) {
                        Text(rep.from).strikethrough().foregroundStyle(.secondary)
                        Image(systemName: "arrow.right").font(.system(size: 10)).foregroundStyle(.secondary)
                        Text(rep.to)
                    }
                    .font(.system(size: 12))
                    .lineLimit(1)
                }
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .cardSurface()
    }
}
