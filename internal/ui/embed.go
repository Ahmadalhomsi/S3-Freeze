// Package ui embeds the built web frontend (web/ is built into ./dist).
package ui

import "embed"

//go:embed all:dist
var Dist embed.FS
