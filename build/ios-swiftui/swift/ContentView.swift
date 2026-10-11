import PhotosUI
import SwiftUI

private let accent = Color(red: 0x7a / 255.0, green: 0xc9 / 255.0, blue: 0x43 / 255.0)

struct ContentView: View {
    @Bindable var vm: ScreenpearViewModel
    @State private var pickerItem: PhotosPickerItem?
    @State private var showRules = false
    @State private var showModels = false

    var body: some View {
        NavigationStack {
            Group {
                if vm.hasImage { workspace } else { empty }
            }
            .navigationTitle("Screenpear")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { toolbarItems }
            .sheet(isPresented: $showRules) { rulesSheet }
            .sheet(isPresented: $showModels) { ModelsView(store: vm.models, engine: $vm.engineID) }
            .sheet(isPresented: Binding(get: { !vm.models.pendingConsent.isEmpty },
                                        set: { if !$0 { vm.models.consentDeclined() } })) {
                ModelConsentSheet(store: vm.models)
            }
            .onChange(of: pickerItem) { _, item in vm.load(item: item) }
            .safeAreaInset(edge: .bottom) { statusBar }
        }
    }

    // MARK: toolbar

    @ToolbarContentBuilder
    private var toolbarItems: some ToolbarContent {
        ToolbarItemGroup(placement: .topBarLeading) {
            PhotosPicker(selection: $pickerItem, matching: .images) {
                Label("Photos", systemImage: "photo.on.rectangle")
            }
            Button { vm.paste() } label: { Label("Paste", systemImage: "doc.on.clipboard") }
        }
        ToolbarItemGroup(placement: .topBarTrailing) {
            Button { showRules = true } label: { Label("Rules", systemImage: "list.bullet.rectangle") }
            Button { showModels = true } label: { Label("Models", systemImage: "cpu") }
            if let img = vm.maskedUIImage {
                ShareLink(item: Image(uiImage: img), preview: SharePreview("Masked screenshot", image: Image(uiImage: img))) {
                    Label("Share", systemImage: "square.and.arrow.up")
                }
            }
        }
    }

    // MARK: empty state

    private var empty: some View {
        ContentUnavailableView {
            Label("No screenshot yet", systemImage: "arrow.down.doc")
        } description: {
            Text("Share an image to Screenpear, pick one from Photos, or paste from the clipboard.")
        } actions: {
            PhotosPicker(selection: $pickerItem, matching: .images) { Text("Choose from Photos") }
                .buttonStyle(.borderedProminent)
            Button("Paste image") { vm.paste() }
        }
    }

    // MARK: workspace

    private var workspace: some View {
        ScrollView {
            VStack(spacing: 14) {
                preview
                if vm.masked != nil {
                    Picker("View", selection: $vm.showMasked) {
                        Text("Original").tag(false)
                        Text("Masked").tag(true)
                    }
                    .pickerStyle(.segmented)
                }
                linesCard
                if !vm.replacements.isEmpty { resultCard }
            }
            .padding()
        }
    }

    /// Standard SwiftUI image fitting: resizable + scaledToFit lets the
    /// image pick its own size inside the available width and height, so
    /// portrait and landscape screenshots both show whole. The overlay's
    /// GeometryReader reports the rendered size, which gives the box scale.
    private var preview: some View {
        Group {
            if let cg = vm.display {
                Image(decorative: cg, scale: 1)
                    .resizable()
                    .interpolation(.high)
                    .scaledToFit()
                    .clipShape(RoundedRectangle(cornerRadius: 10))
                    .overlay {
                        if let a = vm.analysisForOverlay {
                            GeometryReader { geo in
                                boxes(scale: geo.size.width / CGFloat(max(a.width, 1)))
                            }
                        }
                    }
            }
        }
        .frame(maxWidth: .infinity)
    }

    private func boxes(scale: CGFloat) -> some View {
        ForEach(vm.lines) { line in
            let hit = vm.willChange(line)
            let on = !vm.excluded.contains(line.id)
            Rectangle()
                .fill(hit && on ? accent.opacity(0.2) : Color.blue.opacity(on ? 0.08 : 0))
                .overlay(Rectangle().strokeBorder(
                    hit && on ? accent : (on ? Color.blue.opacity(0.7) : Color.white.opacity(0.3)),
                    style: StrokeStyle(lineWidth: 1.5, dash: on ? [] : [4, 3])))
                .frame(width: line.rect.width * scale, height: line.rect.height * scale)
                .offset(x: line.rect.minX * scale, y: line.rect.minY * scale)
                .onTapGesture { vm.toggle(line.id) }
        }
    }

    private var linesCard: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("DETECTED TEXT").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                Spacer()
                Text("\(vm.lines.count) lines").font(.caption).foregroundStyle(.secondary)
            }
            if vm.lines.isEmpty {
                Text(vm.isBusy ? "Working…" : "Nothing detected yet.").foregroundStyle(.secondary)
            }
            ForEach(vm.lines) { line in
                let hit = vm.willChange(line)
                let on = !vm.excluded.contains(line.id)
                HStack(spacing: 10) {
                    Image(systemName: on ? "checkmark.circle.fill" : "circle")
                        .foregroundStyle(on ? accent : .secondary)
                    Text(line.text).lineLimit(1).foregroundStyle(hit ? accent : .primary)
                    Spacer()
                    Text(String(format: "%.2f", line.score)).font(.caption.monospaced()).foregroundStyle(.secondary)
                }
                .opacity(on ? 1 : 0.45)
                .contentShape(Rectangle())
                .onTapGesture { vm.toggle(line.id) }
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14))
    }

    private var resultCard: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("REPLACED").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                Spacer()
                Button("Save to Photos") { vm.saveToPhotos() }.font(.caption)
            }
            ForEach(vm.replacements) { r in
                HStack(spacing: 6) {
                    Text(r.from).strikethrough().foregroundStyle(.secondary)
                    Image(systemName: "arrow.right").font(.caption2).foregroundStyle(.secondary)
                    Text(r.to)
                }
                .font(.footnote)
                .lineLimit(1)
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14))
    }

    // MARK: status + actions

    private var statusBar: some View {
        HStack(spacing: 12) {
            if vm.isBusy { ProgressView().controlSize(.small) }
            Text(vm.status)
                .font(.footnote)
                .foregroundStyle(vm.isError ? .red : .secondary)
                .lineLimit(2)
            Spacer()
            Button { vm.analyze() } label: { Image(systemName: "text.viewfinder") }
                .disabled(!vm.hasImage || vm.isBusy)
            Button { vm.mask() } label: { Label("Mask", systemImage: "eye.slash").labelStyle(.titleAndIcon) }
                .buttonStyle(.borderedProminent)
                .tint(accent)
                .disabled(vm.lines.isEmpty || vm.isBusy)
        }
        .padding(.horizontal)
        .padding(.vertical, 10)
        .background(.bar)
    }

    private var rulesSheet: some View {
        NavigationStack {
            VStack(spacing: 0) {
                Toggle("Substitute lines no rule matches", isOn: $vm.substituteUnmatched)
                    .padding(.horizontal, 16).padding(.vertical, 10)
                Text("Off: only the from=to rules below are applied, like the desktop app. On: every other selected line is replaced with generated text of the same length and shape.")
                    .font(.footnote).foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 16).padding(.bottom, 6)
                Divider()
                TextEditor(text: $vm.rulesText)
                    .font(.system(.body, design: .monospaced))
                    .padding(8)
            }
                .navigationTitle("Replacement rules")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) { Button("Done") { showRules = false } }
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Reset") { vm.rulesText = Rules.format(Rules.defaults) }
                    }
                }
        }
        .presentationDetents([.medium, .large])
    }
}
