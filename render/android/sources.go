// Package android bundles the binding-independent native Android renderer.
package android

import "embed"

//go:embed *.java
var Sources embed.FS

// Resources contains RemoteViews-compatible native layouts, copied unchanged.
//
//go:embed resources/*/*.xml
var Resources embed.FS
