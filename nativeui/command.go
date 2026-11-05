package nativeui

// Command is a local renderer request. Connection and instance IDs are issued
// by Runtime; caller revisions order buffer checkpoints, not repository writes.
type Command struct {
	Version        int               `json:"version"`
	Kind           string            `json:"kind"`
	Connection     string            `json:"connection,omitempty"`
	Instance       string            `json:"instance,omitempty"`
	RequestID      string            `json:"requestId,omitempty"`
	Route          string            `json:"route,omitempty"`
	Action         string            `json:"action,omitempty"`
	Values         map[string]string `json:"values,omitempty"`
	Revision       uint64            `json:"revision"`
	Choice         string            `json:"choice,omitempty"`
	Hello          *Hello            `json:"hello,omitempty"`
	Receipt        *EffectReceipt    `json:"receipt,omitempty"`
	PresentationID string            `json:"presentationId,omitempty"`
	Selected       []string          `json:"selected,omitempty"`
	FileID         string            `json:"fileId,omitempty"`
	FileResult     *FileResult       `json:"fileResult,omitempty"`
	ServiceID      string            `json:"serviceId,omitempty"`
	ServiceResult  *ServiceResult    `json:"serviceResult,omitempty"`
	DataVersion    string            `json:"dataVersion,omitempty"`
}

// EditorState is the renderer-facing result of shared Go editor policy.
// Values are checkpointed values; a renderer must not replace a newer live buffer.
type EditorState struct {
	Baseline    map[string]string `json:"baseline"`
	Values      map[string]string `json:"values"`
	Revision    uint64            `json:"revision"`
	Dirty       bool              `json:"dirty"`
	Pending     bool              `json:"pending"`
	Unknown     bool              `json:"unknown"`
	Restored    bool              `json:"restored"`
	DraftStored bool              `json:"draftStored"`
	CanLeave    bool              `json:"canLeave"`
	CanNavigate bool              `json:"canNavigate"`
	Fields      map[string]string `json:"fields,omitempty"`
	Message     string            `json:"message,omitempty"`
}

// Reply is an observed command outcome. Expected failures stay structured even
// across gomobile, where a Go error alone cannot carry field validation details.
type Reply struct {
	Version        int               `json:"version"`
	Connection     string            `json:"connection,omitempty"`
	Instance       string            `json:"instance,omitempty"`
	RequestID      string            `json:"requestId,omitempty"`
	Revision       uint64            `json:"revision"`
	Contract       *Contract         `json:"contract,omitempty"`
	Screen         *Screen           `json:"screen,omitempty"`
	Response       *Response         `json:"response,omitempty"`
	Editor         *EditorState      `json:"editor,omitempty"`
	Error          *ActionError      `json:"error,omitempty"`
	Effects        []EffectReceipt   `json:"effects,omitempty"`
	EffectIDs      map[string]string `json:"effectIds,omitempty"`
	PresentationID string            `json:"presentationId,omitempty"`
	FileID         string            `json:"fileId,omitempty"`
	ServiceID      string            `json:"serviceId,omitempty"`
	DataVersion    string            `json:"dataVersion,omitempty"`
	Changed        bool              `json:"changed"`
}
