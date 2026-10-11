import Foundation

// Mirrors services.Analysis / services.MaskRequest in Go. Line and Rule live
// in build/swift-shared/Rules.swift, shared with the iOS lane.

struct Analysis: Codable {
    let path: String
    let width: Int
    let height: Int
    let lines: [Line]
    let elapsedMs: Int
}

struct MaskRequest: Codable {
    let path: String
    let rules: [Rule]
    let lines: [Line]
    let lineIds: [Int]?
}

struct Replacement: Codable, Identifiable {
    let lineId: Int
    let from: String
    let to: String
    var id: Int { lineId }
}

struct MaskResult: Codable {
    let output: String
    let replaced: [Replacement]
    let elapsedMs: Int
}

struct APIError: Codable, Error, LocalizedError {
    let error: String
    var errorDescription: String? { error }
}
