// Package refstdio embeds the reference llmctl-stdio/1 backend script.
package refstdio

import _ "embed"

//go:embed ref_stdio_backend.py
var Script string
