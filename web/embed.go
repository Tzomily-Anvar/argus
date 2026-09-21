// Package web carries the built dashboard so the binary is self-contained.
//
// The `all:` prefix is deliberate: it keeps the embed valid when dist
// holds only .gitkeep, which is the state of a fresh checkout before
// anyone has run `npm run build`. The server detects that case and says
// so, rather than serving a blank page.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
