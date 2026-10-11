import Foundation

/// Loopback client for the Go process (services/api.go).
actor APIClient {
    let baseURL: URL

    init(port: Int) {
        baseURL = URL(string: "http://127.0.0.1:\(port)")!
    }

    /// --port wins, then SCREENPEAR_API_PORT, then the port file the Go side
    /// writes under Application Support and the temp dir.
    static func resolvePort() -> Int? {
        let args = CommandLine.arguments
        if let i = args.firstIndex(of: "--port"), i + 1 < args.count, let p = Int(args[i + 1]), p > 0 {
            return p
        }
        if let env = ProcessInfo.processInfo.environment["SCREENPEAR_API_PORT"], let p = Int(env) {
            return p
        }
        let id = "com.automationd.screenpear"
        var candidates: [String] = []
        if let support = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first {
            candidates.append(support.appendingPathComponent("\(id)/api-port").path)
        }
        candidates.append(NSTemporaryDirectory() + "\(id)/api-port")
        for path in candidates {
            if let s = try? String(contentsOfFile: path, encoding: .utf8),
               let p = Int(s.trimmingCharacters(in: .whitespacesAndNewlines)) {
                return p
            }
        }
        return nil
    }

    func imageURL(for path: String) -> URL {
        var c = URLComponents(url: baseURL.appendingPathComponent("api/image"), resolvingAgainstBaseURL: false)!
        c.queryItems = [URLQueryItem(name: "path", value: path)]
        return c.url!
    }

    func health() async -> Bool {
        (try? await get("api/health")) != nil
    }

    func rules() async throws -> [Rule] {
        try JSONDecoder().decode([Rule].self, from: try await get("api/rules"))
    }

    func analyze(path: String) async throws -> Analysis {
        try await post("api/analyze", body: ["path": path])
    }

    func mask(_ req: MaskRequest) async throws -> MaskResult {
        try await post("api/mask", body: req)
    }

    private func get(_ path: String) async throws -> Data {
        let (data, response) = try await URLSession.shared.data(from: baseURL.appendingPathComponent(path))
        try Self.check(data, response)
        return data
    }

    private func post<B: Encodable, R: Decodable>(_ path: String, body: B) async throws -> R {
        var req = URLRequest(url: baseURL.appendingPathComponent(path))
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONEncoder().encode(body)
        req.timeoutInterval = 600 // first run downloads models
        let (data, response) = try await URLSession.shared.data(for: req)
        try Self.check(data, response)
        return try JSONDecoder().decode(R.self, from: data)
    }

    private static func check(_ data: Data, _ response: URLResponse) throws {
        guard let http = response as? HTTPURLResponse, (200..<300).contains(http.statusCode) else {
            if let apiError = try? JSONDecoder().decode(APIError.self, from: data) { throw apiError }
            throw APIError(error: "HTTP \((response as? HTTPURLResponse)?.statusCode ?? 0)")
        }
    }
}
