package nativeui

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/securestore"
	"github.com/DonaldMurillo/gofastr-native/device/sharein"
	"github.com/DonaldMurillo/gofastr-native/device/widgets"
)

func TestDeviceFeatureOutcomesSettleOnce(t *testing.T) {
	tests := []struct {
		name    string
		request ServiceRequest
		outcome ServiceResult
	}{
		{"inbox", ServiceRequest{ID: "device", IncomingShare: &sharein.Request{Operation: sharein.Next}}, ServiceResult{Status: EffectCompleted, IncomingShare: &sharein.Item{Identifier: "0123456789abcdef0123456789abcdef", Text: "Shared text"}}},
		{"secure", ServiceRequest{ID: "device", SecureStorage: &securestore.Request{Operation: securestore.Get, Key: "demo", Protection: securestore.Device}}, ServiceResult{Status: EffectCompleted, SecureStorage: &securestore.Result{Found: true, Value: []byte("secret")}}},
		{"widget", ServiceRequest{ID: "device", Widget: &widgets.Request{Operation: widgets.Status}}, ServiceResult{Status: EffectCompleted, Widget: &widgets.State{Published: true}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			runtime, command := serviceFixture(t, &test.request, func(_ context.Context, _ Request, result ServiceResult) (Result, error) {
				calls.Add(1)
				if err := test.request.accept(result); err != nil {
					t.Error(err)
				}
				return Result{Announcement: "Observed"}, nil
			}, nil)
			shown := admitService(t, runtime, command)
			command.Kind, command.RequestID, command.ServiceID, command.ServiceResult = "service-result", "observed", shown.ServiceID, &test.outcome
			for i := 0; i < 2; i++ {
				if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error != nil {
					t.Fatal(reply.Error)
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("continuation ran %d times", calls.Load())
			}
			command.ServiceResult = &ServiceResult{Status: EffectCancelled}
			if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil || reply.Error.Code != "conflict" {
				t.Fatal("changed replay accepted")
			}
		})
	}
}

func TestDeviceFeatureResultsCannotCrossServicesOrFailure(t *testing.T) {
	secure := &securestore.Result{Found: true, Value: []byte("secret")}
	request := ServiceRequest{ID: "device", Widget: &widgets.Request{Operation: widgets.Status}}
	for _, outcome := range []ServiceResult{
		{Status: EffectCompleted, Widget: &widgets.State{}, SecureStorage: secure},
		{Status: EffectCancelled, Widget: &widgets.State{}},
		{Status: EffectCompleted},
	} {
		if err := request.accept(outcome); err == nil {
			t.Fatalf("accepted unrelated or missing result: %+v", outcome)
		}
	}
	request = ServiceRequest{ID: "device", SecureStorage: &securestore.Request{Operation: securestore.Delete, Key: "demo", Protection: securestore.Device}}
	if err := request.accept(ServiceResult{Status: EffectCompleted, SecureStorage: secure}); err == nil {
		t.Fatal("delete returned secret bytes")
	}
	request.IncomingShare = &sharein.Request{Operation: sharein.Next}
	if request.Validate() == nil {
		t.Fatal("multiple operations accepted")
	}
	request.IncomingShare = nil
	clone := cloneService(&request)
	clone.SecureStorage.Key = "other"
	if request.SecureStorage.Key != "demo" {
		t.Fatal("cloned request aliases original")
	}
	encoded, _ := json.Marshal(ServiceResult{Status: EffectCompleted, SecureStorage: &securestore.Result{}})
	if string(encoded) != `{"status":"completed","secureStorage":{"found":false}}` {
		t.Fatalf("required missing-secret flag lost: %s", encoded)
	}
}
