package securestore

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestPolicy(t *testing.T) {
	valid := []Request{{Operation: Status}, {Operation: Authenticate, Protection: Biometric, Reason: "Unlock private notes"}, {Operation: Put, Protection: Device, Key: "token", Value: []byte("secret")}, {Operation: Get, Protection: Biometric, Key: "token", Reason: "Read your token"}, {Operation: Delete, Protection: Biometric, Key: "token"}}
	for _, r := range valid {
		if err := r.Validate(); err != nil {
			t.Fatalf("%s: %v", r.Operation, err)
		}
	}
	invalid := []Request{{Operation: Put, Protection: Device, Key: "token"}, {Operation: Get, Protection: Device, Key: "token", Value: []byte("secret")}, {Operation: Get, Protection: Biometric, Key: "token"}, {Operation: Put, Protection: Device, Key: "../token", Value: []byte("secret")}, {Operation: Put, Protection: Device, Key: "token", Value: make([]byte, MaxBytes+1)}, {Operation: Authenticate, Protection: Device, Reason: "Unlock"}, {Operation: Status, Key: "token"}, {Operation: Get, Protection: Device, Key: "token", Reason: "Unexpected prompt"}, {Operation: Get, Protection: Biometric, Key: "token", Reason: "\nPrompt"}}
	for _, r := range invalid {
		if r.Validate() == nil {
			t.Fatalf("accepted invalid request %#v", r)
		}
	}
}

func TestResultCannotSmuggleSecretOrChangeMeaning(t *testing.T) {
	get := Request{Operation: Get, Key: "token", Protection: Device}
	for _, result := range []Result{{}, {Found: true, Value: []byte("secret")}} {
		if err := get.Accept(result); err != nil {
			t.Fatal(err)
		}
	}
	for _, result := range []Result{{Found: true}, {Value: []byte("secret")}, {Found: true, Value: make([]byte, MaxBytes+1)}, {Available: true}} {
		if get.Accept(result) == nil {
			t.Fatalf("accepted read %#v", result)
		}
	}
	for _, operation := range []Operation{Put, Delete, Authenticate, Status} {
		r := Request{Operation: operation, Key: "token", Protection: Device}
		if operation == Put {
			r.Value = []byte("secret")
		}
		if operation == Authenticate {
			r.Key = ""
			r.Protection = Biometric
			r.Reason = "Unlock"
		}
		if operation == Status {
			r.Key = ""
			r.Protection = ""
		}
		if r.Accept(Result{Value: []byte("secret"), Found: true}) == nil {
			t.Fatalf("%s returned secret", operation)
		}
	}
}

func TestWireUsesBase64AndOmittedSecrets(t *testing.T) {
	content, err := json.Marshal(Request{Operation: Put, Key: "token", Protection: Device, Value: []byte{0, 1, 255}})
	if err != nil || !strings.Contains(string(content), `"value":"AAH/"`) {
		t.Fatalf("%s %v", content, err)
	}
	content, _ = json.Marshal(Result{})
	if strings.Contains(string(content), "value") || string(content) != `{"found":false}` {
		t.Fatal(string(content))
	}
}
