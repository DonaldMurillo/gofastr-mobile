package featuresnative

import (
	"context"
	"testing"
)

func TestEveryDemoActionUsesValidSharedServiceContracts(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/features", "/features/inbox", "/features/secure", "/features/widgets"} {
		screen, err := app.Render(context.Background(), route, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range screen.Actions {
			response, err := app.Dispatch(context.Background(), route, action.ID, nil)
			if err != nil {
				t.Fatalf("%s/%s: %v", route, action.ID, err)
			}
			if response.Service != nil {
				if err := response.Service.Validate(); err != nil {
					t.Fatalf("%s/%s: %v", route, action.ID, err)
				}
			}
		}
	}
}
