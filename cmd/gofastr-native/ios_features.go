package main

import (
	"io/fs"
	"path/filepath"

	renderios "github.com/DonaldMurillo/gofastr-native/render/ios"
	"github.com/DonaldMurillo/gofastr-native/render/ios/shareextension"
)

// Feature executables contain only native adapters and shared wire DTOs. They
// never link the Go app or inherit its private Keychain access group.
func buildIOSFeatureExtensions(r *runner, a app, work, appDir, sdk, target string) error {
	type feature struct {
		enabled                     bool
		name, module, plist, shared string
		sources                     fs.ReadFileFS
		frameworks                  []string
	}
	widgetSources, err := fs.Sub(renderios.WidgetSources, "widget-extension")
	if err != nil {
		return err
	}
	features := []feature{
		{a.ReceiveShares, "Share", "GofastrShareExtension", "ShareExtension.plist.tmpl", "GofastrIncomingShares.swift", shareextension.Sources, []string{"UIKit", "Social", "UniformTypeIdentifiers"}},
		{a.Widgets, "Widget", "GofastrWidgetExtension", "WidgetExtension.plist.tmpl", "GofastrWidgets.swift", widgetSources.(fs.ReadFileFS), []string{"SwiftUI", "WidgetKit"}},
	}
	for _, f := range features {
		if !f.enabled {
			continue
		}
		ext := a
		ext.ID, ext.Exe = a.ID+"."+f.name, f.name
		ext.SecureStorage = false
		directory := filepath.Join(work, "ios-"+f.name)
		bundle := filepath.Join(appDir, "PlugIns", f.name+".appex")
		if err := r.do("prepare "+f.name+" extension bundle", func() error {
			if err := writeRendered("templates/ios/"+f.plist, filepath.Join(bundle, "Info.plist"), ext); err != nil {
				return err
			}
			if err := writeRendered("templates/ios/DeviceFeatures.entitlements.tmpl", filepath.Join(directory, "Extension.entitlements"), ext); err != nil {
				return err
			}
			content, err := renderios.Sources.ReadFile(f.shared)
			if err != nil {
				return err
			}
			return writeSource(filepath.Join(directory, f.shared), content)
		}); err != nil {
			return err
		}
		sources, err := copyIOSLibrary(r, f.sources, directory, f.name+" extension")
		if err != nil {
			return err
		}
		sources = append(sources, filepath.Join(directory, f.shared))
		args := []string{"--sdk", "iphonesimulator", "swiftc", "-parse-as-library", "-application-extension", "-O", "-module-name", f.module, "-target", target, "-sdk", sdk, "-framework", "Foundation"}
		for _, framework := range f.frameworks {
			args = append(args, "-framework", framework)
		}
		args = append(args, "-o", filepath.Join(bundle, ext.Exe))
		args = append(args, sources...)
		if err := r.run("compile "+f.name+" extension", "xcrun", args...); err != nil {
			return err
		}
		if err := r.run("sign "+f.name+" extension", "codesign", "--force", "--sign", "-", "--timestamp=none", "--entitlements", filepath.Join(directory, "Extension.entitlements"), bundle); err != nil {
			return err
		}
	}
	return nil
}
