package app

import "testing"

func TestNormalizePhone(t *testing.T) {
	cases := map[string]struct {
		want string
		ok   bool
	}{
		"+966 50 123 4567": {"966501234567", true},
		"00966501234567":   {"966501234567", true},
		"٩٦٦٥٠١٢٣٤٥٦٧":     {"966501234567", true},
		"12345":            {"12345", false},
	}
	for in, c := range cases {
		got, ok := NormalizePhone(in)
		if got != c.want || ok != c.ok {
			t.Errorf("NormalizePhone(%q) = %q,%v want %q,%v", in, got, ok, c.want, c.ok)
		}
	}
}

func TestNewerVersion(t *testing.T) {
	if !NewerVersion("v1.2.0", "1.1.9") || NewerVersion("v1.0.0", "1.0.0") || NewerVersion("1.0.0", "dev") || !NewerVersion("v2.0.0", "1.9.9-beta") {
		t.Fatal("version comparison wrong")
	}
}

func TestGeneralNormalize(t *testing.T) {
	g := General{Language: "fr", CatchupHours: 0, WARatePerMin: 500}
	g.normalize()
	if g.Language != "ar" || g.CatchupHours != 1 || g.WARatePerMin != 60 || g.Theme != "system" {
		t.Fatalf("normalize: %+v", g)
	}
}
