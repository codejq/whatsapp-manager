// Package semver compares "vMAJOR.MINOR.PATCH" version strings.
package semver

import (
	"strconv"
	"strings"
)

// Parse returns the three numeric parts of v ("v1.2.3", "1.2.3-beta").
func Parse(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Newer reports whether a is a newer version than b. Unparsable input is never newer.
func Newer(a, b string) bool {
	pa, oka := Parse(a)
	pb, okb := Parse(b)
	if !oka || !okb {
		return false
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

// SameMajor reports whether a and b share the major version.
func SameMajor(a, b string) bool {
	pa, oka := Parse(a)
	pb, okb := Parse(b)
	return oka && okb && pa[0] == pb[0]
}

// Canonical returns "vX.Y.Z" or "" if v cannot be parsed.
func Canonical(v string) string {
	p, ok := Parse(v)
	if !ok {
		return ""
	}
	return "v" + strconv.Itoa(p[0]) + "." + strconv.Itoa(p[1]) + "." + strconv.Itoa(p[2])
}
