package nativeui

import "context"

// Presentation is a transient, Go-authored task. ID names its registered
// continuation; Runtime supplies a separate, opaque presentation instance ID.
type Presentation struct {
	ID          string               `json:"id"`
	Kind        PresentationKind     `json:"kind"`
	Title       string               `json:"title"`
	Message     string               `json:"message,omitempty"`
	Choices     []PresentationChoice `json:"choices"`
	Selection   *Selection           `json:"selection,omitempty"`
	Dismissible bool                 `json:"dismissible"`
	Commit      bool                 `json:"commit"`
}

// PresentationKind describes intent, not a particular platform widget.
type PresentationKind string

const (
	PresentationAlert        PresentationKind = "alert"
	PresentationConfirmation PresentationKind = "confirmation"
	PresentationMenu         PresentationKind = "menu"
	PresentationDialog       PresentationKind = "dialog"
	PresentationSheet        PresentationKind = "sheet"
)

// PresentationChoice is a declared result, not a client-supplied action name.
// Role is empty, cancel, or destructive. Disabled choices cannot be submitted.
type PresentationChoice struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Role     string `json:"role,omitempty"`
	Disabled bool   `json:"disabled"`
}

// Selection describes single or multiple selection independently of controls.
// Min and Max constrain confirmed results; zero Max means all available options.
type Selection struct {
	Key      string            `json:"key,omitempty"`
	Maximum  int               `json:"maximum,omitempty"`
	Options  []SelectionOption `json:"options"`
	Selected []string          `json:"selected"`
	Multiple bool              `json:"multiple"`
	Min      int               `json:"min"`
	Max      int               `json:"max"`
}

// SelectionOption has stable identity and plain-text accessibility content.
type SelectionOption struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Detail   string `json:"detail,omitempty"`
	Disabled bool   `json:"disabled"`
}

// PresentationResult is validated by Go behavior before a continuation runs.
// Cancellation never implies approval and does not execute a continuation.
type PresentationResult struct {
	Choice    string   `json:"choice"`
	Selected  []string `json:"selected"`
	Cancelled bool     `json:"cancelled"`
}

// Respond handles a confirmed presentation result for a retained screen.
// Applications receive typed Go values, not renderer events or wire JSON.
type Respond func(context.Context, Request, PresentationResult) (Result, error)
