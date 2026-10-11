import SwiftUI

/// Engine choice and the model cache. Downloads only start from here or
/// from the consent sheet; nothing is fetched silently.
struct ModelsView: View {
    @Bindable var store: ModelStore
    @Binding var engine: EngineID
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Form {
                Section("Text recognition engine") {
                    Picker("Engine", selection: $engine) {
                        ForEach(EngineID.allCases) { e in Text(e.title).tag(e) }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                    Text(engine.summary).font(.footnote).foregroundStyle(.secondary)
                }

                Section {
                    ForEach(ModelStore.catalog) { model in
                        row(model)
                    }
                    Toggle("Download models automatically", isOn: Binding(
                        get: { store.autoDownload }, set: { store.autoDownload = $0 }))
                } header: {
                    Text("Models")
                } footer: {
                    Text("Models are data files fetched over HTTPS from Hugging Face, verified against a pinned SHA-256 before use, and kept in this app's Application Support folder. With the toggle off, Screenpear asks before every download.")
                }

                if !store.missing(for: .ppocr).isEmpty {
                    Section {
                        Button("Download PP-OCRv6 models (\(ByteCountFormatter.string(fromByteCount: store.missing(for: .ppocr).reduce(0) { $0 + $1.size }, countStyle: .file)))") {
                            store.requestDownload(for: .ppocr)
                        }
                    }
                }
            }
            .navigationTitle("Models")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
        }
    }

    @ViewBuilder
    private func row(_ model: ModelStore.Model) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(model.name)
                Text("\(ByteCountFormatter.string(fromByteCount: model.size, countStyle: .file)) · sha256 \(model.sha256.prefix(12))…")
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
            }
            Spacer()
            switch store.state(of: model) {
            case .absent:
                Image(systemName: "icloud.and.arrow.down").foregroundStyle(.secondary)
            case .downloading(let p):
                ProgressView(value: p).frame(width: 70)
            case .verifying:
                ProgressView().controlSize(.small)
            case .ready:
                Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
            case .failed(let msg):
                Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.red).help(msg)
            }
        }
        .swipeActions {
            if store.state(of: model) == .ready {
                Button("Delete", role: .destructive) { store.delete(model) }
            }
        }
    }
}

/// The "ask before" sheet: lists exactly what will be fetched, from where,
/// how big, and offers a remembered "always allow".
struct ModelConsentSheet: View {
    @Bindable var store: ModelStore
    @State private var always = false

    var body: some View {
        NavigationStack {
            VStack(alignment: .leading, spacing: 16) {
                Label("Download OCR models?", systemImage: "icloud.and.arrow.down")
                    .font(.title3.weight(.semibold))
                Text("PP-OCRv6 needs these files. They are downloaded once over HTTPS and verified against pinned checksums.")
                    .foregroundStyle(.secondary)
                ForEach(store.pendingConsent) { m in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(m.name).font(.body.weight(.medium))
                        Text(m.url.host ?? "").font(.caption).foregroundStyle(.secondary)
                        Text(ByteCountFormatter.string(fromByteCount: m.size, countStyle: .file)).font(.caption).foregroundStyle(.secondary)
                    }
                }
                Toggle("Always allow model downloads", isOn: $always)
                Spacer()
                Button {
                    store.consentGiven(always: always)
                } label: {
                    Text("Download \(ByteCountFormatter.string(fromByteCount: store.totalMissingBytes, countStyle: .file))")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                Button("Not now") { store.consentDeclined() }
                    .frame(maxWidth: .infinity)
            }
            .padding()
        }
        .presentationDetents([.medium])
        .interactiveDismissDisabled()
    }
}
