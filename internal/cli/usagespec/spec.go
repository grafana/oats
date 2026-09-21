// Package usagespec contains the CLI specification and its generated tables.
package usagespec

import _ "embed"

//go:generate mise run generate-cli -- internal/cli/usagespec/tables.go

// Spec is the same source used to generate the parser and shipped with releases.
//
//go:embed oats.usage.kdl
var Spec string
