// Package android bundles the binding-independent native Android renderer.
package android

import "embed"

//go:embed *.java
var Sources embed.FS
