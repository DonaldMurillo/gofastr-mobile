// Package notifications describes explicit, application-scoped local reminders.
package notifications

import (
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	Schedule = "schedule"
	Cancel   = "cancel"
	Status   = "status"
)

// Request uses the device's local calendar; the host must reject past times and
// nonexistent local times without requesting permission for cancel or status.
type Request struct {
	Operation  string `json:"operation"`
	Identifier string `json:"identifier"`
	Title      string `json:"title,omitempty"`
	Body       string `json:"body,omitempty"`
	Route      string `json:"route,omitempty"`
	Date       string `json:"date,omitempty"`
	Time       string `json:"time,omitempty"`
}

// Validate checks shape only, never interprets a schedule in the Go timezone.
func (request Request) Validate() error {
	if request.Identifier == "" || len(request.Identifier) > 128 || strings.ContainsFunc(request.Identifier, func(character rune) bool {
		return !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("._:-", character))
	}) {
		return errors.New("notifications: a stable application-scoped identifier is required")
	}
	if !utf8.ValidString(request.Title) || !utf8.ValidString(request.Body) || len(request.Title) > 200 || len(request.Body) > 64<<10 || strings.ContainsFunc(request.Title, unicode.IsControl) || strings.ContainsRune(request.Body, '\x00') {
		return errors.New("notifications: invalid reminder text")
	}
	switch request.Operation {
	case Cancel, Status:
		if request.Date != "" || request.Time != "" || request.Route != "" || request.Title != "" || request.Body != "" {
			return errors.New("notifications: only scheduling accepts reminder content")
		}
		return nil
	case Schedule:
	default:
		return errors.New("notifications: unsupported operation")
	}
	date, dateErr := time.Parse("2006-01-02", request.Date)
	clock, timeErr := time.Parse("15:04", request.Time)
	parsed, routeErr := url.ParseRequestURI(request.Route)
	if request.Title == "" || dateErr != nil || date.Year() < 1 || date.Format("2006-01-02") != request.Date || timeErr != nil || clock.Format("15:04") != request.Time {
		return errors.New("notifications: a title, canonical YYYY-MM-DD date and HH:MM local time are required")
	}
	if routeErr != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(request.Route, "/") || strings.HasPrefix(request.Route, "//") || strings.ContainsAny(request.Route, "#\\") || len(request.Route) > 4096 {
		return errors.New("notifications: a registered local route is required")
	}
	return nil
}
