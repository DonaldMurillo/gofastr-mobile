package behavior

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"maps"
	"strconv"
	"sync"
)

type Values map[string]string

type FieldErrors map[string]string

type Outcome string

const (
	SaveSucceeded Outcome = "success"
	SaveFailed    Outcome = "failure"
	SaveUnknown   Outcome = "unknown"
)

type Reason string

const (
	ReasonPending        Reason = "save_pending"
	ReasonUnknown        Reason = "save_unknown"
	ReasonDirty          Reason = "unsaved_changes"
	ReasonStale          Reason = "stale_completion"
	ReasonInvalidOutcome Reason = "invalid_outcome"
)

type Submission struct {
	InstanceID string `json:"instance_id"`
	DraftToken string `json:"draft_token"`
	Token      uint64 `json:"token"`
	Revision   uint64 `json:"revision"`
	Route      string `json:"route"`
	Values     Values `json:"values"`
}

type Result struct {
	InstanceID  string      `json:"instance_id"`
	Token       uint64      `json:"token"`
	Revision    uint64      `json:"revision"`
	Outcome     Outcome     `json:"outcome"`
	Values      Values      `json:"values"`
	FieldErrors FieldErrors `json:"field_errors,omitempty"`
	Message     string      `json:"message,omitempty"`
	SavedRoute  string      `json:"saved_route,omitempty"`
}

type Snapshot struct {
	InstanceID  string      `json:"instance_id"`
	DraftToken  string      `json:"draft_token"`
	Route       string      `json:"route"`
	Revision    uint64      `json:"revision"`
	Baseline    Values      `json:"baseline"`
	Current     Values      `json:"current"`
	Submitted   *Submission `json:"submitted,omitempty"`
	Pending     bool        `json:"pending"`
	Outcome     Outcome     `json:"outcome,omitempty"`
	FieldErrors FieldErrors `json:"field_errors,omitempty"`
	Message     string      `json:"message,omitempty"`
	Dirty       bool        `json:"dirty"`
}

type BeginDecision struct {
	Admitted   bool        `json:"admitted"`
	Reason     Reason      `json:"reason,omitempty"`
	Submission *Submission `json:"submission,omitempty"`
	Snapshot   Snapshot    `json:"snapshot"`
}

type CompletionDecision struct {
	Applied    bool           `json:"applied"`
	Reason     Reason         `json:"reason,omitempty"`
	MayLeave   bool           `json:"may_leave"`
	SavedRoute string         `json:"saved_route,omitempty"`
	ClearDraft *DraftRevision `json:"clear_draft,omitempty"`
	Snapshot   Snapshot       `json:"snapshot"`
}

type LeaveDisposition string

const (
	LeaveAllow   LeaveDisposition = "allow"
	LeaveConfirm LeaveDisposition = "confirm"
	LeaveBlock   LeaveDisposition = "block"
)

type LeaveDecision struct {
	Disposition LeaveDisposition `json:"disposition"`
	Reason      Reason           `json:"reason,omitempty"`
	Snapshot    Snapshot         `json:"snapshot"`
}

type Editor struct {
	mu           sync.Mutex
	instanceID   string
	draftSession string
	restored     bool
	route        string
	revision     uint64
	token        uint64
	baseline     Values
	current      Values
	submitted    *Submission
	pending      bool
	outcome      Outcome
	fieldErrors  FieldErrors
	message      string
}

func NewEditor(instanceID, route string, baseline Values) (*Editor, error) {
	if instanceID == "" {
		return nil, errors.New("behavior: editor instance ID is required")
	}
	var session [16]byte
	if _, err := rand.Read(session[:]); err != nil {
		return nil, err
	}
	return &Editor{
		instanceID:   instanceID,
		draftSession: hex.EncodeToString(session[:]),
		route:        route,
		baseline:     maps.Clone(baseline),
		current:      maps.Clone(baseline),
	}, nil
}

func (editor *Editor) Restore(snapshot Snapshot) error {
	if err := validateDraftSnapshot(snapshot); err != nil {
		return err
	}
	editor.mu.Lock()
	defer editor.mu.Unlock()
	if editor.restored || editor.revision != 0 || editor.submitted != nil || editor.outcome != "" {
		return errors.New("behavior: restore requires a fresh editor")
	}
	editor.observe(snapshot.Current)
	editor.fieldErrors = maps.Clone(snapshot.FieldErrors)
	editor.message = snapshot.Message
	editor.outcome = snapshot.Outcome
	if snapshot.Pending || snapshot.Outcome == SaveUnknown {
		editor.outcome = SaveUnknown
	}
	editor.restored = true
	return nil
}

func (editor *Editor) Snapshot() Snapshot {
	editor.mu.Lock()
	defer editor.mu.Unlock()
	return editor.snapshot()
}

func (editor *Editor) Checkpoint(current Values) Snapshot {
	editor.mu.Lock()
	defer editor.mu.Unlock()
	editor.observe(current)
	return editor.snapshot()
}

func (editor *Editor) Begin(current Values) BeginDecision {
	editor.mu.Lock()
	defer editor.mu.Unlock()
	editor.observe(current)
	if editor.pending {
		return BeginDecision{Reason: ReasonPending, Snapshot: editor.snapshot()}
	}
	if editor.outcome == SaveUnknown {
		return BeginDecision{Reason: ReasonUnknown, Snapshot: editor.snapshot()}
	}
	editor.token++
	editor.submitted = &Submission{
		InstanceID: editor.instanceID,
		DraftToken: editor.draftToken(),
		Token:      editor.token,
		Revision:   editor.revision,
		Route:      editor.route,
		Values:     maps.Clone(editor.current),
	}
	editor.pending = true
	editor.outcome = ""
	editor.message = ""
	return BeginDecision{Admitted: true, Submission: cloneSubmission(editor.submitted), Snapshot: editor.snapshot()}
}

func (editor *Editor) Complete(result Result, current Values) CompletionDecision {
	editor.mu.Lock()
	defer editor.mu.Unlock()
	if editor.submitted == nil || result.InstanceID != editor.instanceID ||
		result.Token != editor.submitted.Token || result.Revision != editor.submitted.Revision ||
		(!editor.pending && (editor.outcome != SaveUnknown || result.Outcome == SaveUnknown)) {
		return CompletionDecision{Reason: ReasonStale, Snapshot: editor.snapshot()}
	}
	if result.Outcome != SaveSucceeded && result.Outcome != SaveFailed && result.Outcome != SaveUnknown {
		return CompletionDecision{Reason: ReasonInvalidOutcome, Snapshot: editor.snapshot()}
	}
	editor.observe(current)
	editor.pending = false
	editor.outcome = result.Outcome
	editor.message = result.Message
	decision := CompletionDecision{Applied: true}
	switch result.Outcome {
	case SaveSucceeded:
		newerEdits := editor.revision != editor.submitted.Revision
		confirmed := result.Values
		if confirmed == nil {
			confirmed = editor.submitted.Values
		}
		editor.baseline = maps.Clone(confirmed)
		if editor.revision == editor.submitted.Revision {
			editor.observe(confirmed)
		}
		editor.fieldErrors = nil
		if result.SavedRoute != "" {
			editor.route = result.SavedRoute
		}
		decision.MayLeave = !editor.dirty()
		decision.SavedRoute = editor.route
		decision.ClearDraft = &DraftRevision{InstanceID: editor.instanceID, Revision: editor.submitted.Revision, Token: editor.submitted.DraftToken}
		if !newerEdits {
			decision.ClearDraft.Revision = editor.revision
		}
	case SaveFailed:
		editor.fieldErrors = nil
		for field, message := range result.FieldErrors {
			if sameField(editor.current, editor.submitted.Values, field) {
				if editor.fieldErrors == nil {
					editor.fieldErrors = make(FieldErrors)
				}
				editor.fieldErrors[field] = message
			}
		}
	}
	decision.Snapshot = editor.snapshot()
	return decision
}

func (editor *Editor) Leave(current Values) LeaveDecision {
	editor.mu.Lock()
	defer editor.mu.Unlock()
	editor.observe(current)
	decision := LeaveDecision{Disposition: LeaveAllow}
	switch {
	case editor.pending:
		decision.Disposition, decision.Reason = LeaveBlock, ReasonPending
	case editor.outcome == SaveUnknown:
		decision.Disposition, decision.Reason = LeaveBlock, ReasonUnknown
	case editor.dirty():
		decision.Disposition, decision.Reason = LeaveConfirm, ReasonDirty
	}
	decision.Snapshot = editor.snapshot()
	return decision
}

func (editor *Editor) observe(current Values) {
	if maps.Equal(editor.current, current) {
		return
	}
	for field := range editor.fieldErrors {
		if !sameField(editor.current, current, field) {
			delete(editor.fieldErrors, field)
		}
	}
	editor.current = maps.Clone(current)
	editor.revision++
}

func (editor *Editor) dirty() bool {
	return !maps.Equal(editor.baseline, editor.current)
}

func (editor *Editor) draftToken() string {
	return editor.draftSession + "-" + strconv.FormatUint(editor.token, 10)
}

func (editor *Editor) snapshot() Snapshot {
	return Snapshot{
		InstanceID: editor.instanceID, DraftToken: editor.draftToken(), Route: editor.route, Revision: editor.revision,
		Baseline: maps.Clone(editor.baseline), Current: maps.Clone(editor.current),
		Submitted: cloneSubmission(editor.submitted), Pending: editor.pending,
		Outcome: editor.outcome, FieldErrors: maps.Clone(editor.fieldErrors),
		Message: editor.message, Dirty: editor.dirty(),
	}
}

func cloneSubmission(submitted *Submission) *Submission {
	if submitted == nil {
		return nil
	}
	copy := *submitted
	copy.Values = maps.Clone(submitted.Values)
	return &copy
}

func sameField(first, second Values, field string) bool {
	firstValue, firstExists := first[field]
	secondValue, secondExists := second[field]
	return firstValue == secondValue && firstExists == secondExists
}
