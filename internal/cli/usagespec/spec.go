// Package usagespec contains the CLI specification and its generated tables.
package usagespec

import _ "embed"

//go:generate usage generate go -f oats.usage.kdl -o tables.go -p usagespec

// Spec is the same source used to generate the parser and shipped with releases.
//
//go:embed oats.usage.kdl
var Spec string
