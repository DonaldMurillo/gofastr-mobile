package webassets

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr-native/extension"
)

func TestCompiledRenderingDescriptorValidation(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node is required to execute bundled asset validation")
	}
	for _, check := range []struct {
		name, script string
		valid        bool
	}{
		{"component", `window.__gofastr_component_extension_definitions.push({name:'example.text/v1',capabilities:()=>({'example.text/v1':'available'}),mount:()=>({update(){},dispose(){}})});`, true},
		{"wrong kind", `window.__gofastr_file_extension_definitions = [{name:'example.text/v1',capabilities:()=>({'example.text/v1':'available'}),prepare(){}}];`, false},
		{"wrong name", `window.__gofastr_component_extension_definitions.push({name:'other.text/v1',capabilities:()=>({'example.text/v1':'available'}),mount(){}});`, false},
		{"missing mount", `window.__gofastr_component_extension_definitions.push({name:'example.text/v1',capabilities:()=>({'example.text/v1':'available'})});`, false},
		{"undeclared capability", `window.__gofastr_component_extension_definitions.push({name:'example.text/v1',capabilities:()=>({'example.text/v1':'available','camera/v1':'available'}),mount(){}});`, false},
	} {
		t.Run(check.name, func(t *testing.T) {
			manifest := extension.Manifest{Name: "example.text/v1", Kind: extension.ComponentKind, Capabilities: []string{"example.text/v1"}, IOS: extension.IOS{Factory: "ExampleText", Sources: []string{"Text.swift"}}, WebView: extension.WebView{Script: "text.js"}}
			content, _ := json.Marshal(manifest)
			definition := extension.Definition{Manifest: manifest, Assets: fstest.MapFS{extension.ManifestName: {Data: content}, "Text.swift": {Data: []byte("import UIKit")}, "text.js": {Data: []byte(check.script)}}, ValidateComponent: func(extension.Component) error { return nil }}
			module, wrapped, err := source(definition)
			if err != nil {
				t.Fatal(err)
			}
			program := "global.window={__gofastr:{}}; let accepted = false; try {\n" + wrapped + "\naccepted = !!window.__gofastr.loadedModules[" + strconv.Quote(module) + "]; } catch {} if (accepted !== " + strconv.FormatBool(check.valid) + ") process.exit(1);"
			if output, err := exec.Command("node", "-e", program).CombinedOutput(); err != nil {
				t.Fatalf("descriptor validation failed: %v\n%s", err, output)
			}
		})
	}
}
