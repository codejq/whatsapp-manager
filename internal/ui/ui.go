// Package ui embeds the built web interface (web/ → internal/ui/dist).
package ui

import "embed"

// FS holds dist/; it contains only .gitkeep until `npm run build` runs.
//
//go:embed all:dist
var FS embed.FS
