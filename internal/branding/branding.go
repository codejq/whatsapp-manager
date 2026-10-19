// Package branding holds the static sponsor ad appended to every email the
// app sends (the UI shows the same ad in the sidebar).
package branding

import (
	"fmt"
	"html"
	"strings"
)

// LogoURL is the sponsor logo, loaded by the mail client from the sponsor's
// server (not attached, so emails stay small and show no attachment icon).
const LogoURL = "https://api-academy.s-i-u.com/storage-cdn/082026/44172c982df844e4b96a48abc363fe9c.png"

type ad struct{ Label, Title, Text, CTA, URL string }

var ads = map[string]ad{
	"ar": {
		Label: "إعلان",
		Title: "منصة الدروس الخصوصية عربي ولغات الأولى في المنطقة",
		Text:  "احجز دروسك الخصوصية مباشرة من المنصة مع أمهر المدرسين من الروضة حتى الجامعة.",
		CTA:   "احجز درسك الآن",
		URL:   "https://www.s-i-u.com/ar/",
	},
	"en": {
		Label: "Sponsored",
		Title: "The leading private tutoring platform for Arabic and languages in the region",
		Text:  "Book your private lessons directly on the platform with the best teachers, from kindergarten to university.",
		CTA:   "Book a lesson",
		URL:   "https://www.s-i-u.com/en/",
	},
}

func get(lang string) ad {
	if a, ok := ads[lang]; ok {
		return a
	}
	return ads["ar"]
}

// AdText is the plain-text version of the ad.
func AdText(lang string) string {
	a := get(lang)
	return fmt.Sprintf("\n----------------------------------------\n%s: %s\n%s\n%s: %s\n", a.Label, a.Title, a.Text, a.CTA, a.URL)
}

// AdHTML is an email-safe (table + inline styles) HTML block.
func AdHTML(lang string) string {
	a := get(lang)
	// Spacing between logo and text sits on the side facing the text.
	dir, align, gap := "rtl", "right", "left"
	if lang == "en" {
		dir, align, gap = "ltr", "left", "right"
	}
	e := html.EscapeString
	return fmt.Sprintf(`<table role="presentation" dir="%[1]s" cellpadding="0" cellspacing="0" border="0" style="margin-top:24px;width:100%%;max-width:520px;border:1px solid #f3d27a;border-radius:12px;background:#fffbeb;font-family:Segoe UI,Tahoma,Arial,sans-serif">
<tr><td style="padding:16px;text-align:%[2]s">
<div style="font-size:11px;font-weight:bold;color:#b45309;letter-spacing:.5px;margin-bottom:8px">%[3]s</div>
<table role="presentation" cellpadding="0" cellspacing="0" border="0" dir="%[1]s"><tr>
<td style="vertical-align:top;padding-%[9]s:12px"><a href="%[7]s"><img src="%[8]s" width="72" height="72" alt="" style="display:block;border:0;border-radius:10px;background:#fff"></a></td>
<td style="vertical-align:top;text-align:%[2]s">
<div style="font-size:15px;font-weight:bold;color:#111827;line-height:1.4">%[4]s</div>
<div style="font-size:13px;color:#4b5563;line-height:1.6;margin-top:4px">%[5]s</div>
</td></tr></table>
<div style="margin-top:12px;text-align:center"><a href="%[7]s" style="display:inline-block;background:#f59e0b;color:#ffffff;font-weight:bold;font-size:13px;text-decoration:none;padding:8px 20px;border-radius:8px">%[6]s</a></div>
</td></tr></table>`, dir, align, e(a.Label), e(a.Title), e(a.Text), e(a.CTA), e(a.URL), e(LogoURL), gap)
}

// AppendToHTML inserts the ad at the end of an HTML document body.
func AppendToHTML(doc, lang string) string {
	block := AdHTML(lang)
	if i := strings.LastIndex(doc, "</body>"); i >= 0 {
		return doc[:i] + block + "\n" + doc[i:]
	}
	return doc + block
}
