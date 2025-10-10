package persona

// ContainsEmoji returns true if the input string contains (likely) emoji.
func ContainsEmoji(s string) bool {
	runes := []rune(s)
	for i := range runes {
		r := runes[i]

		if isRegionalIndicator(r) {
			if i+1 < len(runes) && isRegionalIndicator(runes[i+1]) {
				return true
			}
			// single regional indicator alone is not considered emoji
			continue
		}

		// keycap sequences: base (0-9, #, *) + (optional VS16) + U+20E3
		if isKeycapBase(r) {
			if i+1 < len(runes) && runes[i+1] == 0x20E3 { // COMBINING ENCLOSING KEYCAP
				return true
			}
			if i+2 < len(runes) && runes[i+1] == 0xFE0F && runes[i+2] == 0x20E3 {
				return true
			}
		}

		if isEmojiRune(r) {
			return true
		}

		// some plain characters become emoji when followed by VS16 (U+FE0F).
		// e.g. © (U+00A9), ® (U+00AE), certain punctuation like U+203C
		if i+1 < len(runes) && runes[i+1] == 0xFE0F && isEmojiPresentationBase(r) {
			return true
		}

		// if current rune is ZWJ, or next is ZWJ and neighbors are pictographs, treat as emoji
		if r == 0x200D { // ZERO WIDTH JOINER
			// to be conservative, check neighbors
			if (i-1 >= 0 && isEmojiRune(runes[i-1])) || (i+1 < len(runes) && isEmojiRune(runes[i+1])) {
				return true
			}
		}
	}

	return false
}

func isRegionalIndicator(r rune) bool {
	return r >= 0x1F1E6 && r <= 0x1F1FF
}

func isKeycapBase(r rune) bool {
	// digits 0-9, '#' (U+0023), '*' (U+002A)
	return (r >= '0' && r <= '9') || r == 0x23 || r == 0x2A
}

func isEmojiRune(r rune) bool {
	switch {
	// Emoticons
	case r >= 0x1F600 && r <= 0x1F64F:
		return true
	// Misc Symbols & Pictographs
	case r >= 0x1F300 && r <= 0x1F5FF:
		return true
	// Transport & Map
	case r >= 0x1F680 && r <= 0x1F6FF:
		return true
	// Supplemental Symbols and Pictographs
	case r >= 0x1F900 && r <= 0x1F9FF:
		return true
	// Symbols & Pictographs Extended-A
	case r >= 0x1FA70 && r <= 0x1FAFF:
		return true
	// Misc symbols (not all are emojis, but meh)
	case r >= 0x2600 && r <= 0x26FF:
		return true
	// Dingbats
	case r >= 0x2700 && r <= 0x27BF:
		return true
	// Enclosed Alphanumeric and Enclosed Ideographic
	case r >= 0x24C2 && r <= 0x1F251:
		return true
	// Miscellaneous Technical
	case r >= 0x2300 && r <= 0x23FF:
		return true
	// Geometric Shapes Extended
	case r >= 0x1F780 && r <= 0x1F7FF:
		return true
	}
	return false
}

func isEmojiPresentationBase(r rune) bool {
	// some codepoints (©, ®, numbers, punctuation) can be text or emoji presentation when followed by VS16
	switch r {
	case 0x00A9, // ©
		0x00AE, // ®
		0x203C, // ‼
		0x2049, // ⁉
		0x3030, // 〰
		0x303D, // 〽
		0x2753, // ❓
		0x2754, // ❔
		0x2755, // ❕
		0x2757, // ❗
		0x2117, // ℗
		0x2122, // ™
		0x2139, // ℹ️
		0x2194, // ↔️
		0x2195, // ↕️
		0x2196, // ↖️
		0x2197, // ↗️
		0x2198, // ↘️
		0x2199: // ↙️
		return true
	}
	return false
}
