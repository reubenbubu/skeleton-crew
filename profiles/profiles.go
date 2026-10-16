// Package profiles holds the built-in profiles, embedded into the binary.
// Each directory is one profile: profile.toml, docs/ and scaffold/.
package profiles

import "embed"

//go:embed all:java-spring all:node-ts all:python
var FS embed.FS
