// Package shareextension bundles the separate iOS share extension executable.
package shareextension

import "embed"

//go:embed *.swift
var Sources embed.FS
