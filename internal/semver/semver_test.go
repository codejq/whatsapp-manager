package semver

import "testing"

func TestSemver(t *testing.T) {
	if !Newer("v9.7.0", "v9.6.0") || Newer("v9.6.0", "v9.6.0") || Newer("v9.5.9", "9.6.0") || !Newer("v10.0.0", "v9.9.9") || Newer("x", "v1.0.0") {
		t.Fatal("Newer wrong")
	}
	if !SameMajor("v9.7.0", "9.1.2") || SameMajor("v10.0.0", "v9.6.0") {
		t.Fatal("SameMajor wrong")
	}
	if Canonical("9.6.0") != "v9.6.0" || Canonical("v9.6.0-rc1") != "v9.6.0" || Canonical("bad") != "" {
		t.Fatal("Canonical wrong")
	}
}
