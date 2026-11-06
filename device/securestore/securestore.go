// Package securestore defines bounded, app-private secret storage operations.
package securestore

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxBytes = 16 << 10

type Operation string

const (
	Put          Operation = "put"
	Get          Operation = "get"
	Delete       Operation = "delete"
	Authenticate Operation = "authenticate"
	Status       Operation = "status"
)

type Protection string

const (
	Device    Protection = "device"
	Biometric Protection = "biometric"
)

// Request uses separate device and biometric compartments for each key. A
// biometric read requires fresh OS authentication bound to the secret's key.
// Authenticate checks presence only; it does not unlock or encrypt app data.
type Request struct {
	Operation  Operation  `json:"operation"`
	Key        string     `json:"key,omitempty"`
	Value      []byte     `json:"value,omitempty"`
	Protection Protection `json:"protection,omitempty"`
	Reason     string     `json:"reason,omitempty"`
}

// Result carries bytes only for a successful Get. Found distinguishes a
// missing secret from a successful read. Status reports current availability.
type Result struct {
	Value              []byte `json:"value,omitempty"`
	Found              bool   `json:"found"`
	Available          bool   `json:"available,omitempty"`
	BiometricAvailable bool   `json:"biometricAvailable,omitempty"`
}

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func (request Request) Validate() error {
	if len(request.Value) > MaxBytes || len(request.Reason) > 200 || !utf8.ValidString(request.Reason) || strings.ContainsFunc(request.Reason, unicode.IsControl) {
		return errors.New("securestore: invalid secret size or authentication reason")
	}
	switch request.Operation {
	case Status:
		if request.Key != "" || len(request.Value) != 0 || request.Protection != "" || request.Reason != "" {
			return errors.New("securestore: status has no arguments")
		}
		return nil
	case Authenticate:
		if request.Key != "" || len(request.Value) != 0 || request.Protection != Biometric || strings.TrimSpace(request.Reason) == "" {
			return errors.New("securestore: authentication requires biometric protection and a reason")
		}
		return nil
	case Put, Get, Delete:
		if !keyPattern.MatchString(request.Key) || (request.Protection != Device && request.Protection != Biometric) {
			return errors.New("securestore: invalid key or protection")
		}
	default:
		return errors.New("securestore: unknown operation")
	}
	if (request.Operation == Put) != (len(request.Value) > 0) {
		return errors.New("securestore: only put requires nonempty bytes")
	}
	prompt := request.Protection == Biometric && request.Operation != Delete
	if prompt && strings.TrimSpace(request.Reason) == "" || !prompt && request.Reason != "" {
		return errors.New("securestore: invalid authentication reason for operation")
	}
	return nil
}

// Accept rejects bytes and availability claims unrelated to the request.
func (request Request) Accept(result Result) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if len(result.Value) > MaxBytes {
		return errors.New("securestore: secret exceeds byte limit")
	}
	if request.Operation == Get {
		if result.Available || result.BiometricAvailable || result.Found != (len(result.Value) > 0) {
			return errors.New("securestore: invalid read result")
		}
		return nil
	}
	if len(result.Value) != 0 || result.Found {
		return errors.New("securestore: unexpected secret content")
	}
	if request.Operation != Status && (result.Available || result.BiometricAvailable) || result.BiometricAvailable && !result.Available {
		return errors.New("securestore: invalid availability result")
	}
	return nil
}
