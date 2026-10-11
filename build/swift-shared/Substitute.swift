import Foundation

/// Generated replacement text for lines no rule matches.
///
/// Keeps the shape of the original so the repaint fits the same box: same
/// character count, letters become pronounceable letters with the same
/// case, digits become other digits, everything else (punctuation, spaces,
/// symbols) stays. Deterministic per (text, seed) so re-masking the same
/// screenshot gives the same result. "nutcorp-prod 10.2.50.31" becomes
/// something like "lomirta-kedu 47.9.13.85".
enum Substitute {
    private static let consonants = Array("bcdfghklmnprstvz")
    private static let vowels = Array("aeiou")

    static func text(for original: String, seed: Int) -> String {
        // FNV-1a rather than hashValue: Swift seeds hashValue per process, and
        // the result must be stable across launches.
        var h: UInt64 = 0xcbf2_9ce4_8422_2325
        for b in original.utf8 { h = (h ^ UInt64(b)) &* 0x0100_0000_01b3 }
        var rng = XorShift(seed: h ^ (UInt64(truncatingIfNeeded: seed) &* 0x9E37_79B9_7F4A_7C15))
        var out = ""
        var wantVowel = Bool.random(using: &rng)
        for ch in original {
            if ch.isLetter {
                let pool = wantVowel ? vowels : consonants
                let c = pool[Int(rng.next() % UInt64(pool.count))]
                out.append(ch.isUppercase ? Character(c.uppercased()) : c)
                wantVowel.toggle()
            } else if ch.isNumber {
                out.append(Character(String(rng.next() % 10)))
            } else {
                out.append(ch)
                wantVowel = Bool.random(using: &rng)
            }
        }
        return out
    }

    struct XorShift: RandomNumberGenerator {
        private var state: UInt64
        init(seed: UInt64) { state = seed == 0 ? 0x2545_F491_4F6C_DD1D : seed }
        mutating func next() -> UInt64 {
            state ^= state << 13
            state ^= state >> 7
            state ^= state << 17
            return state
        }
    }
}
