// Package webassets registers compiled extension assets before GoFastr seals its
// behavior manifest. Applications select definitions separately in nativeui.
package webassets

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

var modules sync.Map

func source(definition extension.Definition) (string, string, error) {
	if _, err := extension.New(definition); err != nil {
		return "", "", err
	}
	content, err := definition.Assets.ReadFile(definition.Manifest.WebView.Script)
	if err != nil {
		return "", "", err
	}
	manifest, _ := json.Marshal(definition.Manifest)
	digest := sha256.Sum256(append(manifest, content...))
	name := fmt.Sprintf("gfn-extension-%x", digest[:16])
	capabilities, _ := json.Marshal(definition.Manifest.Capabilities)
	wrapped := fmt.Sprintf(`(() => {
  const definitions = window.__gofastr_file_extension_definitions ||= [];
  const before = definitions.length;
  (() => {
%s
  })();
  const adapter = definitions[before];
  const declared = %s;
  if (definitions.length !== before + 1 || adapter?.name !== %s ||
      typeof adapter.prepare !== 'function' || typeof adapter.capabilities !== 'function' ||
      definitions.slice(0, before).some(item => item.name === adapter.name)) {
    throw new Error('Invalid compiled extension registration');
  }
  const capabilities = adapter.capabilities.bind(adapter);
  adapter.capabilities = () => {
    const facts = capabilities();
    if (!facts || Object.keys(facts).length !== declared.length ||
        declared.some(key => !['available','fallback','unavailable'].includes(facts[key]))) {
      throw new Error('Invalid extension capabilities');
    }
    return facts;
  };
  adapter.capabilities();
  (window.__gofastr.loadedModules ||= {})[%s] = true;
})();`, content, capabilities, strconv.Quote(definition.Manifest.Name), strconv.Quote(name))
	return name, wrapped, nil
}

// Register belongs in the extension library's package init, never app startup
// after another host has already built its immutable behavior manifest.
func Register(definition extension.Definition) {
	name, wrapped, err := source(definition)
	if err != nil {
		panic(err)
	}
	registry.RegisterBehavior(name, wrapped, registry.OnRequest())
	modules.Store(name, true)
}

// Module resolves an already registered, content-addressed bundled adapter.
func Module(definition extension.Definition) (string, error) {
	name, _, err := source(definition)
	if err != nil {
		return "", err
	}
	if _, exists := modules.Load(name); !exists {
		return "", errors.New("extension: register WebView assets at library package init")
	}
	return name, nil
}
