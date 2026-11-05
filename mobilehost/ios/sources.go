package ios

import "embed"

// Sources embeds binding-independent Swift host adapters copied without templates.
//
//go:embed *.swift
var Sources embed.FS
