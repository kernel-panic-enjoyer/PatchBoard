package updater

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func decodeCommandOutputBytes(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if utf8.Valid(data) {
		return repairUTF8Mojibake(string(data))
	}
	return repairUTF8Mojibake(decodeWindows1252(data))
}

func repairUTF8Mojibake(value string) string {
	if value == "" || !strings.ContainsAny(value, "ÃÂâ") {
		return value
	}
	var builder strings.Builder
	var segment strings.Builder
	changed := false
	flushSegment := func() {
		if segment.Len() == 0 {
			return
		}
		original := segment.String()
		repaired, ok := repairUTF8MojibakeSegment(original)
		if ok {
			builder.WriteString(repaired)
			changed = true
		} else {
			builder.WriteString(original)
		}
		segment.Reset()
	}
	for _, r := range value {
		if unicode.IsSpace(r) {
			flushSegment()
			builder.WriteRune(r)
			continue
		}
		segment.WriteRune(r)
	}
	flushSegment()
	if !changed {
		return value
	}
	return builder.String()
}

func repairUTF8MojibakeSegment(value string) (string, bool) {
	if value == "" || !strings.ContainsAny(value, "ÃÂâ") {
		return "", false
	}
	encoded := make([]byte, 0, len(value))
	for _, r := range value {
		b, ok := encodeWindows1252Rune(r)
		if !ok {
			return "", false
		}
		encoded = append(encoded, b)
	}
	if !utf8.Valid(encoded) {
		return "", false
	}
	repaired := string(encoded)
	if commandOutputTextScore(repaired) <= commandOutputTextScore(value)+2 {
		return "", false
	}
	return repaired, true
}

func decodeWindows1252(data []byte) string {
	var builder strings.Builder
	builder.Grow(len(data))
	for _, b := range data {
		builder.WriteRune(decodeWindows1252Byte(b))
	}
	return builder.String()
}

func decodeWindows1252Byte(b byte) rune {
	if b < 0x80 || b >= 0xa0 {
		return rune(b)
	}
	switch b {
	case 0x80:
		return '€'
	case 0x82:
		return '‚'
	case 0x83:
		return 'ƒ'
	case 0x84:
		return '„'
	case 0x85:
		return '…'
	case 0x86:
		return '†'
	case 0x87:
		return '‡'
	case 0x88:
		return 'ˆ'
	case 0x89:
		return '‰'
	case 0x8a:
		return 'Š'
	case 0x8b:
		return '‹'
	case 0x8c:
		return 'Œ'
	case 0x8e:
		return 'Ž'
	case 0x91:
		return '‘'
	case 0x92:
		return '’'
	case 0x93:
		return '“'
	case 0x94:
		return '”'
	case 0x95:
		return '•'
	case 0x96:
		return '–'
	case 0x97:
		return '—'
	case 0x98:
		return '˜'
	case 0x99:
		return '™'
	case 0x9a:
		return 'š'
	case 0x9b:
		return '›'
	case 0x9c:
		return 'œ'
	case 0x9e:
		return 'ž'
	case 0x9f:
		return 'Ÿ'
	default:
		return utf8.RuneError
	}
}

func encodeWindows1252Rune(r rune) (byte, bool) {
	if r < 0x80 || (r >= 0xa0 && r <= 0xff) {
		return byte(r), true
	}
	switch r {
	case '€':
		return 0x80, true
	case '‚':
		return 0x82, true
	case 'ƒ':
		return 0x83, true
	case '„':
		return 0x84, true
	case '…':
		return 0x85, true
	case '†':
		return 0x86, true
	case '‡':
		return 0x87, true
	case 'ˆ':
		return 0x88, true
	case '‰':
		return 0x89, true
	case 'Š':
		return 0x8a, true
	case '‹':
		return 0x8b, true
	case 'Œ':
		return 0x8c, true
	case 'Ž':
		return 0x8e, true
	case '‘':
		return 0x91, true
	case '’':
		return 0x92, true
	case '“':
		return 0x93, true
	case '”':
		return 0x94, true
	case '•':
		return 0x95, true
	case '–':
		return 0x96, true
	case '—':
		return 0x97, true
	case '˜':
		return 0x98, true
	case '™':
		return 0x99, true
	case 'š':
		return 0x9a, true
	case '›':
		return 0x9b, true
	case 'œ':
		return 0x9c, true
	case 'ž':
		return 0x9e, true
	case 'Ÿ':
		return 0x9f, true
	default:
		return 0, false
	}
}

func commandOutputTextScore(value string) int {
	score := 0
	for _, r := range value {
		switch {
		case r == utf8.RuneError:
			score -= 8
		case r == '\n' || r == '\r' || r == '\t':
			score += 1
		case unicode.IsControl(r):
			score -= 6
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			score += 3
		case unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r):
			score += 1
		default:
			score -= 1
		}
		switch r {
		case 'Ã', 'Â':
			score -= 8
		case '�':
			score -= 12
		case 'Ä', 'Ö', 'Ü', 'ä', 'ö', 'ü', 'ß':
			score += 3
		}
	}
	return score
}
