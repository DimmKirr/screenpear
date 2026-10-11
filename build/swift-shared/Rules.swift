import Foundation

// Shared by the macOS and iOS lanes. Mirrors internal/mask in Go: a Line is
// one recognized text line with its box in image pixels, a Rule is one
// literal case-sensitive substitution.

struct Line: Codable, Identifiable, Hashable {
    let id: Int
    let text: String
    let score: Float
    let x0: Int
    let y0: Int
    let x1: Int
    let y1: Int
    /// Baseline skew in radians, image coordinates (positive = clockwise on
    /// screen). 0 for engines that only report axis-aligned boxes.
    var angle: Double = 0

    init(id: Int, text: String, score: Float, x0: Int, y0: Int, x1: Int, y1: Int, angle: Double = 0) {
        self.id = id; self.text = text; self.score = score
        self.x0 = x0; self.y0 = y0; self.x1 = x1; self.y1 = y1; self.angle = angle
    }

    /// Mirror of mask.Line.Classify: a glyph-sized icon read as one
    /// character, or a line without any letter or digit, is a symbol and is
    /// not rewritten unless selected.
    var isSymbol: Bool {
        let chars = text.filter { !$0.isWhitespace }
        let alnum = chars.filter { $0.isLetter || $0.isNumber }.count
        return alnum == 0 || chars.count <= 1 || (chars.count <= 2 && score < 0.8)
    }

    private enum CodingKeys: String, CodingKey { case id, text, score, x0, y0, x1, y1, angle }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(Int.self, forKey: .id)
        text = try c.decode(String.self, forKey: .text)
        score = try c.decode(Float.self, forKey: .score)
        x0 = try c.decode(Int.self, forKey: .x0); y0 = try c.decode(Int.self, forKey: .y0)
        x1 = try c.decode(Int.self, forKey: .x1); y1 = try c.decode(Int.self, forKey: .y1)
        angle = try c.decodeIfPresent(Double.self, forKey: .angle) ?? 0
    }

    var rect: CGRect {
        CGRect(x: x0, y: y0, width: x1 - x0, height: y1 - y0)
    }
}

struct Rule: Codable, Hashable {
    var from: String
    var to: String
}

enum Rules {
    /// The dictionary the original tool shipped with (mask.DefaultRules).
    static let defaults: [Rule] = [
        Rule(from: "NutCorp", to: "SquirrelCorp"),
        Rule(from: "nutcorp", to: "squirrelcorp"),
        Rule(from: "DevOps", to: "SysAdmin"),
        Rule(from: "devops", to: "sysadmin"),
        Rule(from: "github", to: "screenpear"),
        Rule(from: "tesseract", to: "EasyOCR"),
        Rule(from: "postgres", to: "sqlite"),
        Rule(from: "Upgrade", to: "update"),
        Rule(from: "Orange", to: "Raspberry"),
        Rule(from: "metameetings", to: "instabark"),
        Rule(from: "mapdigital", to: "barkdigital"),
        Rule(from: "mm-core", to: "ib-core"),
        Rule(from: "mm-", to: "ib-"),
    ]

    /// One "from=to" per line; blank lines and # comments are skipped and a
    /// line without "=" is ignored so half-typed rules never block a run.
    static func parse(_ text: String) -> [Rule] {
        text.split(separator: "\n").compactMap { raw in
            let line = raw.trimmingCharacters(in: .whitespaces)
            guard !line.isEmpty, !line.hasPrefix("#"), let eq = line.firstIndex(of: "=") else { return nil }
            let from = line[..<eq].trimmingCharacters(in: .whitespaces)
            guard !from.isEmpty else { return nil }
            return Rule(from: from, to: line[line.index(after: eq)...].trimmingCharacters(in: .whitespaces))
        }
    }

    static func format(_ rules: [Rule]) -> String {
        rules.map { "\($0.from)=\($0.to)" }.joined(separator: "\n")
    }

    /// mask.Rewrite: longest `from` first, literal, case-sensitive.
    static func rewrite(_ text: String, _ rules: [Rule]) -> (String, Bool) {
        var out = text
        for r in rules.sorted(by: { $0.from.count > $1.from.count }) where !r.from.isEmpty {
            out = out.replacingOccurrences(of: r.from, with: r.to)
        }
        return (out, out != text)
    }

    static func wouldChange(_ text: String, _ rules: [Rule]) -> Bool {
        rewrite(text, rules).1
    }
}
