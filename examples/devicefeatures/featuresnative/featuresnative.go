// Package featuresnative demonstrates one Go application using device adapters
// on both native platforms. Demo secrets are fixed test data, never credentials.
package featuresnative

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/DonaldMurillo/gofastr-native/device/securestore"
	"github.com/DonaldMurillo/gofastr-native/device/sharein"
	"github.com/DonaldMurillo/gofastr-native/device/widgets"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

var lifecycle sync.RWMutex
var runtime *nativeui.Runtime
var screens *nativeui.App

func Start(dataDir string) error {
	lifecycle.Lock()
	defer lifecycle.Unlock()
	if runtime != nil {
		return errors.New("featuresnative: already started")
	}
	app, err := newApp()
	if err != nil {
		return err
	}
	r, err := nativeui.NewRuntime(app, nativeui.RuntimeOptions{})
	if err != nil {
		return err
	}
	screens, runtime = app, r
	return nil
}

func Stop() error {
	lifecycle.Lock()
	defer lifecycle.Unlock()
	screens, runtime = nil, nil
	return nil
}
func Root() string { return "/features" }

func Command(payload string) (string, error) {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	if runtime == nil {
		return "", errors.New("featuresnative: not started")
	}
	return runtime.CommandJSON(context.Background(), payload)
}
func Render(route, values string) (string, error) {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	if screens == nil {
		return "", errors.New("featuresnative: not started")
	}
	return screens.RenderJSON(context.Background(), route, values)
}
func Dispatch(route, action, values string) (string, error) {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	if screens == nil {
		return "", errors.New("featuresnative: not started")
	}
	return screens.DispatchJSON(context.Background(), route, action, values)
}

func newApp() (*nativeui.App, error) {
	var inboxMu sync.Mutex
	var next *sharein.Item
	service := func(request nativeui.ServiceRequest) nativeui.Handle {
		request.ID = "device"
		return func(context.Context, nativeui.Request) (nativeui.Result, error) {
			return nativeui.Result{Service: &request}, nil
		}
	}
	secure := func(op securestore.Operation, policy securestore.Protection) nativeui.Handle {
		r := securestore.Request{Operation: op, Protection: policy}
		if op == securestore.Put || op == securestore.Get || op == securestore.Delete {
			r.Key = "demo"
		}
		if op == securestore.Put {
			r.Value = []byte("gofastr-demo-secret")
		}
		if policy == securestore.Biometric && op != securestore.Delete {
			r.Reason = "Read or save the demo secret"
		}
		return service(nativeui.ServiceRequest{SecureStorage: &r})
	}
	respond := func(_ context.Context, _ nativeui.Request, result nativeui.ServiceResult) (nativeui.Result, error) {
		message := "Device result: " + string(result.Status)
		if result.Status == nativeui.EffectCompleted {
			switch {
			case result.SecureStorage != nil:
				if result.SecureStorage.Found {
					message = fmt.Sprintf("Read %d secret bytes. The value stays out of the UI.", len(result.SecureStorage.Value))
				} else if result.SecureStorage.Available {
					message = fmt.Sprintf("Secure storage available. Biometrics available: %t.", result.SecureStorage.BiometricAvailable)
				} else {
					message = "Secure operation completed. No secret bytes returned."
				}
			case result.Widget != nil:
				message = fmt.Sprintf("Widget snapshot published: %t.", result.Widget.Published)
			default:
				item := result.IncomingShare
				inboxMu.Lock()
				next = item
				inboxMu.Unlock()
				if item != nil {
					message = fmt.Sprintf("Share %s: %s %s (%d files). Read again keeps the same item.", item.Identifier, item.Title, preview(item.Text), len(item.Files))
				} else {
					message = "The share inbox is empty or the share was discarded."
				}
			}
		}
		return nativeui.Result{Announcement: message}, nil
	}
	deviceRoute := func(path, title, text string, actions []nativeui.Action, handlers map[string]nativeui.Handle) nativeui.Route {
		return nativeui.Route{Path: path, Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Title: title, Text: text, Actions: actions}, nil
		}, Actions: handlers, Services: map[string]nativeui.ServiceRespond{"device": respond}}
	}
	return nativeui.New(Root(), nativeui.Route{Path: Root(), Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Device features", Prompt: "The same Go screens run on Android and iOS.", List: &nativeui.List{Rows: []nativeui.Row{
			{ID: "inbox", Title: "Shared inbox", Destination: "/features/inbox"},
			{ID: "secure", Title: "Secure storage", Destination: "/features/secure"},
			{ID: "widgets", Title: "Home-screen widget", Destination: "/features/widgets"},
		}}}, nil
	}},
		deviceRoute("/features/inbox", "Shared inbox", "Share text or files from another app, then choose Read next. Read does not consume it. Discard removes only the reviewed share.", []nativeui.Action{
			{ID: "read", Label: "Read next", Primary: true}, {ID: "discard", Label: "Discard reviewed share"},
		}, map[string]nativeui.Handle{
			"read": service(nativeui.ServiceRequest{IncomingShare: &sharein.Request{Operation: sharein.Next}}),
			"discard": func(context.Context, nativeui.Request) (nativeui.Result, error) {
				inboxMu.Lock()
				defer inboxMu.Unlock()
				if next == nil {
					return nativeui.Result{Announcement: "Read a share before discarding it."}, nil
				}
				return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "device", IncomingShare: &sharein.Request{Operation: sharein.Acknowledge, Identifier: next.Identifier}}}, nil
			},
		}),
		deviceRoute("/features/secure", "Secure storage", "These actions use a fixed demo secret. Authentication confirms your presence; it does not encrypt this app's database.", []nativeui.Action{
			{ID: "status", Label: "Check availability", Primary: true}, {ID: "put", Label: "Store demo secret"}, {ID: "get", Label: "Read demo secret"}, {ID: "delete", Label: "Delete demo secret"},
			{ID: "biometric-put", Label: "Store with biometrics"}, {ID: "biometric-get", Label: "Read with biometrics"}, {ID: "biometric-delete", Label: "Delete biometric secret"}, {ID: "authenticate", Label: "Authenticate"},
		}, map[string]nativeui.Handle{
			"status": secure(securestore.Status, ""), "put": secure(securestore.Put, securestore.Device), "get": secure(securestore.Get, securestore.Device), "delete": secure(securestore.Delete, securestore.Device),
			"biometric-put": secure(securestore.Put, securestore.Biometric), "biometric-get": secure(securestore.Get, securestore.Biometric), "biometric-delete": secure(securestore.Delete, securestore.Biometric), "authenticate": secure(securestore.Authenticate, securestore.Biometric),
		}),
		deviceRoute("/features/widgets", "Home-screen widget", "Publish a public summary, then add this app's widget from your home screen. Publishing does not pin it or confirm OS display.", []nativeui.Action{
			{ID: "publish", Label: "Publish summary", Primary: true}, {ID: "status", Label: "Check snapshot"}, {ID: "clear", Label: "Clear summary"},
		}, map[string]nativeui.Handle{
			"publish": service(nativeui.ServiceRequest{Widget: &widgets.Request{Operation: widgets.Publish, Snapshot: &widgets.Snapshot{Title: "GoFastr device features", Text: "Published from shared Go code", Route: "/features/widgets", Items: []widgets.Item{{ID: "shared", Title: "Android and iOS", Subtitle: "One Go snapshot"}}}}}),
			"status":  service(nativeui.ServiceRequest{Widget: &widgets.Request{Operation: widgets.Status}}),
			"clear":   service(nativeui.ServiceRequest{Widget: &widgets.Request{Operation: widgets.Clear}}),
		}),
	)
}

func preview(value string) string {
	runes := []rune(value)
	if len(runes) > 160 {
		return string(runes[:160]) + "..."
	}
	return value
}
