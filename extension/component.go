package extension

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/DonaldMurillo/gofastr-native/behavior"
)

// Component is a keyed immutable presentation snapshot. Destinations maps opaque
// adapter event keys to Go routes; adapters must emit keys, never arbitrary URLs.
type Component struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Payload      string            `json:"payload"`
	Destinations map[string]string `json:"destinations,omitempty"`
}

// DecodeComponent strictly decodes a bounded, library-owned presentation payload.
func DecodeComponent(payload string, target any) error {
	return decode(payload, target, MaxComponentPayload)
}

// ValidateComponent checks per-app registration before invoking library policy.
// Route existence and navigation guards remain the screen runtime's concern.
func (registry *Registry) ValidateComponent(component Component) error {
	definition, exists := registry.definitions[component.Name]
	if !exists || definition.Manifest.Kind != ComponentKind || !behavior.SafeToken(component.ID) || len(component.Destinations) > 128 {
		return errors.New("extension: unregistered component or invalid identity")
	}
	if len(component.Payload) == 0 || len(component.Payload) > MaxComponentPayload || !json.Valid([]byte(component.Payload)) {
		return fmt.Errorf("extension: invalid component payload for %q", component.Name)
	}
	for key, route := range component.Destinations {
		if !behavior.SafeToken(key) || route == "" || len(route) > 4096 {
			return errors.New("extension: invalid component destination")
		}
	}
	return definition.ValidateComponent(component)
}
