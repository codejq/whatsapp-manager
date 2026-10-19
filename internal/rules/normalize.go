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

// Links are detected generically, for any site:
//   - anything starting with a scheme (http://, https://, ftp://) or "www.",
//     even when glued to other text ("الرابطhttps://…", "Join:https://…");
//   - bare domains with any alphabetic top-level domain ("zoom.us/j/1",
//     "mysite.academy", "t.me/x"). File names ("report.pdf") and email
//     addresses ("a@b.com") are not counted as links.
var (
	schemeLinkRe = regexp.MustCompile(`(?i)(?:(?:https?|ftp)://|www\.)\S+`)
	bareDomainRe = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+([a-z]{2,24})\b(?:[/?#][^\s]*)?`)
)

// fileExts are endings that look like top-level domains but are file names.
var fileExts = map[string]bool{
	"pdf": true, "doc": true, "docx": true, "xls": true, "xlsx": true, "ppt": true, "pptx": true,
	"txt": true, "csv": true, "rtf": true, "odt": true, "jpg": true, "jpeg": true, "png": true,
	"gif": true, "webp": true, "bmp": true, "svg": true, "heic": true, "mp3": true, "mp4": true,
	"m4a": true, "wav": true, "ogg": true, "opus": true, "mov": true, "avi": true, "mkv": true,
	"zip": true, "rar": true, "exe": true, "apk": true, "msi": true, "dll": true, "json": true,
	"xml": true, "html": true, "htm": true, "js": true, "css": true, "py": true, "go": true,
}

// FindLinks returns all links in s.
func FindLinks(s string) []string {
	var out []string
	taken := schemeLinkRe.FindAllStringIndex(s, -1)
	for _, m := range taken {
		out = append(out, s[m[0]:m[1]])
	}
	for _, m := range bareDomainRe.FindAllStringSubmatchIndex(s, -1) {
		start, end := m[0], m[1]
		inside := false
		for _, t := range taken { // already part of a scheme link
			if start < t[1] && end > t[0] {
				inside = true
				break
			}
		}
		if inside || (start > 0 && s[start-1] == '@') { // email address
			continue
		}
		if fileExts[strings.ToLower(s[m[2]:m[3]])] {
			continue
		}
		out = append(out, s[start:end])
	}
	return out
}

// ContainsLink reports whether the text contains a URL or a bare domain.
func ContainsLink(s string) bool { return len(FindLinks(s)) > 0 }
