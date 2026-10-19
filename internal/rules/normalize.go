package rules

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var folder = cases.Fold()

// NormalizeText prepares text for keyword matching: Unicode NFKC, case
// folding, Arabic diacritics and tatweel removed, common Arabic letter
// variants unified, and Arabic-Indic digits mapped to ASCII.
func NormalizeText(s string) string {
	s = norm.NFKC.String(s)
	s = folder.String(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 0x064B && r <= 0x065F, r == 0x0670, r >= 0x06D6 && r <= 0x06ED: // tashkeel & Quranic marks
			continue
		case r == 0x0640: // tatweel
			continue
		case r == 0x200C || r == 0x200D || r == 0x200E || r == 0x200F || r == 0xFEFF: // zero-width & bidi marks
			continue
		case r == 'أ' || r == 'إ' || r == 'آ' || r == 'ٱ':
			b.WriteRune('ا')
		case r == 'ى':
			b.WriteRune('ي')
		case r == 'ة':
			b.WriteRune('ه')
		case r == 'ؤ':
			b.WriteRune('و')
		case r == 'ئ':
			b.WriteRune('ي')
		case r >= '٠' && r <= '٩':
			b.WriteRune('0' + (r - '٠'))
		case r >= '۰' && r <= '۹':
			b.WriteRune('0' + (r - '۰'))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// containsWord reports whether needle occurs in hay as a whole word
// (bounded by non-letter/digit characters or the string edges). Both must
// already be normalized.
func containsWord(hay, needle string) bool {
	if needle == "" {
		return false
	}
	start := 0
	for {
		i := strings.Index(hay[start:], needle)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(needle)
		if boundaryBefore(hay, i) && boundaryAfter(hay, end) {
			return true
		}
		start = i + 1
		if start >= len(hay) {
			return false
		}
	}
}

func boundaryBefore(s string, i int) bool {
	if i == 0 {
		return true
	}
	r := lastRune(s[:i])
	return !isWordRune(r)
}

func boundaryAfter(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	for _, r := range s[i:] {
		return !isWordRune(r)
	}
	return true
}

func lastRune(s string) rune {
	var last rune
	for _, r := range s {
		last = r
	}
	return last
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// Common TLDs used for bare-domain link detection (e.g. "example.com/page").
const tlds = `com|net|org|io|me|co|info|biz|app|dev|ly|gl|link|xyz|online|store|shop|site|edu|gov|tv|us|uk|sa|ae|eg|qa|kw|bh|om|jo|ma|tn|dz|iq|lb|ps|sy|ye|sd|tr|de|fr|in|pk|ru|cn|ai|gg|to`

var linkRe = regexp.MustCompile(`(?i)(?:\bhttps?://\S+|\bwww\.\S+|\b[a-z0-9][a-z0-9-]*(?:\.[a-z0-9-]+)*\.(?:` + tlds + `)\b(?:/\S*)?)`)

// ContainsLink reports whether the text contains a URL or a bare domain.
func ContainsLink(s string) bool { return linkRe.MatchString(s) }

// FindLinks returns all links in s.
func FindLinks(s string) []string { return linkRe.FindAllString(s, -1) }
