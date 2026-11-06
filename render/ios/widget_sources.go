package ios

import "embed"

// WidgetSources contains the separate WidgetKit executable. Its @main entry
// point must never be compiled into the containing application.
//
//go:embed widget-extension/*.swift
var WidgetSources embed.FS
