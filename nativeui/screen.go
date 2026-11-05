// Package nativeui defines Go-authored mobile screens shared by bundled native
// and WebView renderers. It is independent of GoFastr's web design system.
package nativeui

import "context"

// Screen describes one task-shaped native screen, not an HTML document.
type Screen struct {
	Route       string            `json:"route"`
	Title       string            `json:"title"`
	Prompt      string            `json:"prompt,omitempty"`
	LargeTitle  bool              `json:"largeTitle,omitempty"`
	Values      map[string]string `json:"values,omitempty"`
	Actions     []Action          `json:"actions,omitempty"`
	List        *List             `json:"list,omitempty"`
	Form        *Form             `json:"form,omitempty"`
	Text        string            `json:"text,omitempty"`
	FilePreview *FilePreview      `json:"filePreview,omitempty"`
	Images      []Image           `json:"images,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	Requires    []Requirement     `json:"requires,omitempty"`
	Tabs        []Tab             `json:"tabs,omitempty"`
	ActiveTab   string            `json:"activeTab,omitempty"`
	Live        *Live             `json:"live,omitempty"`
}

type Tab struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Route string `json:"route"`
	Icon  Icon   `json:"icon,omitempty"`
}

// Action names a registered Go handler exposed in a screen's navigation bar.
type Action struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Icon    Icon   `json:"icon,omitempty"`
	Leading bool   `json:"leading,omitempty"`
	Primary bool   `json:"primary,omitempty"`
	Commit  bool   `json:"commit,omitempty"`
}

// Icon is a semantic mobile icon, mapped to platform assets by the renderer.
type Icon string

const (
	IconCompose  Icon = "compose"
	IconInfo     Icon = "info"
	IconShare    Icon = "share"
	IconFile     Icon = "file"
	IconPhoto    Icon = "photo"
	IconAudio    Icon = "audio"
	IconNotes    Icon = "notes"
	IconSettings Icon = "settings"
)

// List uses platform list, search and refresh controls.
type List struct {
	Header            string     `json:"header,omitempty"`
	Footer            string     `json:"footer,omitempty"`
	Empty             string     `json:"empty,omitempty"`
	SearchKey         string     `json:"searchKey,omitempty"`
	SearchPlaceholder string     `json:"searchPlaceholder,omitempty"`
	Refreshable       bool       `json:"refreshable,omitempty"`
	Filters           []Field    `json:"filters,omitempty"`
	Selection         *Selection `json:"selection,omitempty"`
	Paging            *Paging    `json:"paging,omitempty"`
	Rows              []Row      `json:"rows"`
}

type Paging struct {
	Key   string `json:"key"`
	Next  string `json:"next"`
	Label string `json:"label"`
}

// Row is an accessible, full-width navigation destination.
type Row struct {
	ID              string      `json:"id"`
	Title           string      `json:"title"`
	Subtitle        string      `json:"subtitle,omitempty"`
	Metadata        string      `json:"metadata,omitempty"`
	Icon            Icon        `json:"icon,omitempty"`
	Destination     string      `json:"destination"`
	Disabled        bool        `json:"disabled,omitempty"`
	LeadingActions  []RowAction `json:"leadingActions,omitempty"`
	TrailingActions []RowAction `json:"trailingActions,omitempty"`
}

type RowAction struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Role      string `json:"role,omitempty"`
	FullSwipe bool   `json:"fullSwipe,omitempty"`
}

// Form binds mobile controls to named string values supplied to Go actions.
type Form struct {
	Fields       []Field `json:"fields"`
	Status       string  `json:"status,omitempty"`
	Draft        *Draft  `json:"draft,omitempty"`
	Style        string  `json:"style,omitempty"`
	DetailsTitle string  `json:"detailsTitle,omitempty"`
}

// Field describes a mobile control. Values and labels are always plain text.
type Field struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Placeholder string   `json:"placeholder,omitempty"`
	Value       string   `json:"value"`
	Kind        string   `json:"kind"`
	Style       string   `json:"style,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Disabled    bool     `json:"disabled,omitempty"`
	Options     []Option `json:"options,omitempty"`
}

type Option struct {
	Value    string `json:"value"`
	Label    string `json:"label"`
	Disabled bool   `json:"disabled,omitempty"`
}

// Draft opts a form into local recovery and a native unsaved-changes prompt.
// Drafts are not encrypted storage, repository saves, or conflict resolution.
type Draft struct {
	Key         string `json:"key"`
	Changed     string `json:"changed"`
	Restored    string `json:"restored"`
	BackLabel   string `json:"backLabel"`
	Title       string `json:"title"`
	Message     string `json:"message"`
	KeepEditing string `json:"keepEditing"`
	KeepDraft   string `json:"keepDraft"`
	Discard     string `json:"discard"`
}

// Request carries route parameters and submitted/search values. Authentication
// comes from the caller's context, never from these values.
type Request struct {
	Route  string
	Params map[string]string
	Values map[string]string
}

// Navigation requests one native stack operation. Push and sheet destinations
// are rendered in Go before the renderer receives the result.
type Navigation struct {
	Kind  string `json:"kind"`
	Route string `json:"route,omitempty"`
}

// Result describes an action's presentation effects after its Go work succeeds.
type Result struct {
	Service      *ServiceRequest `json:"service,omitempty"`
	Files        *FileRequest    `json:"files,omitempty"`
	Presentation *Presentation   `json:"presentation,omitempty"`
	Navigation   *Navigation     `json:"navigation,omitempty"`
	Share        string          `json:"share,omitempty"`
	Haptic       string          `json:"haptic,omitempty"`
	Announcement string          `json:"announcement,omitempty"`
	ClearDraft   bool            `json:"clearDraft,omitempty"`
	Saved        *SavedState     `json:"saved,omitempty"`
}

// SavedState identifies a committed entity and its canonical field baseline.
// Renderers adopt Route without rebuilding a live editor when newer edits exist.
type SavedState struct {
	Route    string            `json:"route"`
	Values   map[string]string `json:"values"`
	DraftKey string            `json:"draftKey,omitempty"`
}

// Render builds a screen from application state.
type Render func(context.Context, Request) (Screen, error)

// Handle performs a named application action using submitted field values.
type Handle func(context.Context, Request) (Result, error)

// Route registers a Go screen and its available actions.
type Route struct {
	Path          string
	Screen        Render
	Actions       map[string]Handle
	Presentations map[string]Respond
	Files         map[string]FileRespond
	Services      map[string]ServiceRespond
	Watch         Watch
}

// RouteInfo is a copy of a registered route's public presentation metadata.
// It contains no executable handlers and may be shared with a local renderer.
type RouteInfo struct {
	Path    string   `json:"path"`
	Actions []string `json:"actions"`
}

// ActionError describes an expected action failure without hiding its cause.
// Code "validation" identifies invalid fields; "not_found", "forbidden", and
// "conflict" identify other expected failures. Unrecognized codes stay internal.
// Message and Fields are safe, user-facing plain text, never HTML.
type ActionError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	Err     error             `json:"-"`
}

func (err *ActionError) Error() string { return err.Message }

// Unwrap preserves errors.Is/errors.As behavior for application service errors.
func (err *ActionError) Unwrap() error { return err.Err }

// Response is the versioned, local gomobile wire envelope. Applications author
// Go definitions; only the renderer and binding boundary use this JSON format.
type Response struct {
	Version int     `json:"version"`
	Screen  *Screen `json:"screen,omitempty"`
	Result
}
