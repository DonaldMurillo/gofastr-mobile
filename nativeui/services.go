package nativeui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-mobile/behavior"
	"github.com/DonaldMurillo/gofastr-mobile/device/audio"
	"github.com/DonaldMurillo/gofastr-mobile/device/files"
	"github.com/DonaldMurillo/gofastr-mobile/device/notifications"
	"github.com/DonaldMurillo/gofastr-mobile/device/sharing"
)

// ServiceRequest is one immutable, explicitly initiated device operation.
type ServiceRequest struct {
	ID           string                 `json:"id"`
	Notification *notifications.Request `json:"notification,omitempty"`
	Share        *sharing.Request       `json:"share,omitempty"`
	Clipboard    *sharing.Clipboard     `json:"clipboard,omitempty"`
	Audio        *audio.Request         `json:"audio,omitempty"`
	Commit       bool                   `json:"commit,omitempty"`
}

// ServiceResult is an observed OS outcome, not permission to repeat an operation.
type ServiceResult struct {
	Status         EffectStatus    `json:"status"`
	Message        string          `json:"message,omitempty"`
	Permission     string          `json:"permission,omitempty"`
	Identifier     string          `json:"identifier,omitempty"`
	File           *files.Document `json:"file,omitempty"`
	DurationMillis int             `json:"durationMillis,omitempty"`
}

// ServiceRespond receives every validated outcome, including cancellation.
// Only completed audio results contain content that can be persisted.
type ServiceRespond func(context.Context, Request, ServiceResult) (Result, error)

// Validate checks the operation without invoking application or platform code.
func (request ServiceRequest) Validate() error {
	operations := 0
	for _, present := range []bool{request.Notification != nil, request.Share != nil, request.Clipboard != nil, request.Audio != nil} {
		if present {
			operations++
		}
	}
	if !behavior.SafeToken(request.ID) || operations != 1 {
		return errors.New("nativeui: exactly one named device service is required")
	}
	switch {
	case request.Notification != nil:
		return request.Notification.Validate()
	case request.Share != nil:
		return request.Share.Validate()
	case request.Clipboard != nil:
		return request.Clipboard.Validate()
	default:
		return request.Audio.Validate()
	}
}

func (request ServiceRequest) capability() Capability {
	switch {
	case request.Notification != nil:
		return CapabilityNotifications
	case request.Share != nil:
		return CapabilitySharing
	case request.Clipboard != nil:
		return CapabilityClipboard
	default:
		return CapabilityAudio
	}
}

func (request ServiceRequest) accept(outcome ServiceResult) error {
	for _, text := range []string{outcome.Message, outcome.Permission, outcome.Identifier} {
		if len(text) > 512 || !utf8.ValidString(text) || strings.ContainsFunc(text, unicode.IsControl) {
			return errors.New("nativeui: invalid service outcome text")
		}
	}
	if outcome.Identifier != "" && (request.Notification == nil || outcome.Identifier != request.Notification.Identifier) {
		return errors.New("nativeui: service result has a different reminder identifier")
	}
	switch outcome.Status {
	case EffectCompleted:
		if request.Audio != nil {
			if outcome.File == nil {
				return errors.New("nativeui: successful recording requires audio content")
			}
			return request.Audio.Accept(*outcome.File, outcome.DurationMillis)
		}
	case EffectCancelled, EffectDenied, EffectUnsupported, EffectFailed:
	default:
		return errors.New("nativeui: unknown service outcome")
	}
	if outcome.File != nil || outcome.DurationMillis != 0 {
		return errors.New("nativeui: only successful audio may return bytes or duration")
	}
	return nil
}

func cloneService(request *ServiceRequest) *ServiceRequest {
	if request == nil {
		return nil
	}
	encoded, _ := json.Marshal(request)
	var clone ServiceRequest
	_ = json.Unmarshal(encoded, &clone)
	return &clone
}
