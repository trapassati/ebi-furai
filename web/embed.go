// Package web embeds the PWA files. Hidden files (such as .DS_Store copied
// from a Mac) are left out.
package web

import "embed"

//go:embed static
var FS embed.FS
