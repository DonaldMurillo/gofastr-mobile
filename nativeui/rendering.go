package nativeui

import (
	"errors"
	"fmt"

	"github.com/DonaldMurillo/gofastr-native/extension"
)

func (app *App) validateRenderingComponents(screen *Screen) error {
	if len(screen.Components) > 16 {
		return errors.New("nativeui: at most 16 rendering components per screen")
	}
	screen.Requires = append([]Requirement(nil), screen.Requires...)
	seen, size := map[string]bool{}, 0
	for _, component := range screen.Components {
		if seen[component.ID] {
			return errors.New("nativeui: duplicate rendering component identity")
		}
		seen[component.ID] = true
		size += len(component.Payload)
		for key, destination := range component.Destinations {
			size += len(key) + len(destination)
		}
		if size > extension.MaxComponentPayload {
			return errors.New("nativeui: combined rendering component payload exceeds 2 MiB")
		}
		if err := app.extensions.ValidateComponent(component); err != nil {
			return err
		}
		for _, destination := range component.Destinations {
			if _, err := app.Resolve(destination); err != nil {
				return fmt.Errorf("nativeui: invalid component destination: %w", err)
			}
		}
		capability := Capability(component.Name)
		found := false
		for index := range screen.Requires {
			if screen.Requires[index].Capability == capability {
				screen.Requires[index].AllowFallback = false
				found = true
			}
		}
		if !found {
			screen.Requires = append(screen.Requires, Requirement{Capability: capability})
		}
	}
	return nil
}
