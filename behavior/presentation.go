package behavior

import (
	"errors"
	"slices"
	"sync"
)

// Choice declares a semantic result. Cancellation bypasses selection limits.
type Choice struct {
	ID       string
	Disabled bool
	Cancel   bool
}

// Option identifies a selectable value independently of platform controls.
type Option struct {
	ID       string
	Disabled bool
}

// Selection constrains a confirmed selection. Zero Max means the available
// option count for multiple selection, or one for single selection.
type Selection struct {
	Options  []Option
	Selected []string
	Multiple bool
	Min      int
	Max      int
}

// PresentationSpec contains behavior only, without content or widget types.
type PresentationSpec struct {
	Choices     []Choice
	Selection   *Selection
	Dismissible bool
}

// PresentationOutcome is an immutable, validated result in option order.
type PresentationOutcome struct {
	Choice    string
	Selected  []string
	Cancelled bool
}

// Presentation admits one declared result for one retained instance.
// Renderers keep tentative selection and focus locally until confirmation.
type Presentation struct {
	mu      sync.Mutex
	id      string
	spec    PresentationSpec
	settled bool
}

// NewPresentation validates and copies a small interaction specification.
func NewPresentation(instanceID string, spec PresentationSpec) (*Presentation, error) {
	if instanceID == "" || len(instanceID) > 128 || len(spec.Choices) == 0 || len(spec.Choices) > 16 {
		return nil, errors.New("behavior: invalid presentation identity or choices")
	}
	spec.Choices = slices.Clone(spec.Choices)
	seen := make(map[string]bool)
	for _, choice := range spec.Choices {
		if choice.ID == "" || len(choice.ID) > 128 || seen[choice.ID] {
			return nil, errors.New("behavior: duplicate or invalid presentation choice")
		}
		seen[choice.ID] = true
	}
	if spec.Selection != nil {
		selection := *spec.Selection
		selection.Options = slices.Clone(selection.Options)
		selection.Selected = slices.Clone(selection.Selected)
		if len(selection.Options) == 0 || len(selection.Options) > 256 || selection.Min < 0 || selection.Max < 0 {
			return nil, errors.New("behavior: invalid selection bounds")
		}
		seen = make(map[string]bool)
		for _, option := range selection.Options {
			if option.ID == "" || len(option.ID) > 128 || seen[option.ID] {
				return nil, errors.New("behavior: duplicate or invalid selection option")
			}
			seen[option.ID] = true
		}
		if selection.Max == 0 {
			selection.Max = len(selection.Options)
			if !selection.Multiple {
				selection.Max = 1
			}
		}
		if selection.Min > selection.Max || selection.Max > len(selection.Options) || (!selection.Multiple && selection.Max > 1) {
			return nil, errors.New("behavior: inconsistent selection bounds")
		}
		if _, err := selection.normalize(selection.Selected, false); err != nil {
			return nil, err
		}
		spec.Selection = &selection
	}
	return &Presentation{id: instanceID, spec: spec}, nil
}

// Resolve validates a result before consuming the presentation. Invalid input
// leaves it open; a consumed or foreign instance cannot execute again.
func (presentation *Presentation) Resolve(instanceID, choiceID string, selected []string) (PresentationOutcome, error) {
	presentation.mu.Lock()
	defer presentation.mu.Unlock()
	if instanceID != presentation.id || presentation.settled {
		return PresentationOutcome{}, errors.New("behavior: presentation is no longer active")
	}
	outcome := PresentationOutcome{Choice: choiceID, Selected: []string{}}
	if choiceID == "" {
		if !presentation.spec.Dismissible {
			return PresentationOutcome{}, errors.New("behavior: choose an explicit presentation action")
		}
		outcome.Cancelled = true
	} else {
		found := false
		for _, choice := range presentation.spec.Choices {
			if choice.ID == choiceID && !choice.Disabled {
				found, outcome.Cancelled = true, choice.Cancel
				break
			}
		}
		if !found {
			return PresentationOutcome{}, errors.New("behavior: choose an available presentation action")
		}
	}
	if !outcome.Cancelled {
		if presentation.spec.Selection == nil {
			if len(selected) != 0 {
				return PresentationOutcome{}, errors.New("behavior: this presentation has no selection")
			}
		} else {
			var err error
			outcome.Selected, err = presentation.spec.Selection.normalize(selected, true)
			if err != nil {
				return PresentationOutcome{}, err
			}
		}
	}
	presentation.settled = true
	return outcome, nil
}

func (selection Selection) normalize(selected []string, requireMinimum bool) ([]string, error) {
	if len(selected) > selection.Max || (requireMinimum && len(selected) < selection.Min) {
		return nil, errors.New("behavior: choose the required number of options")
	}
	seen := make(map[string]bool)
	for _, identifier := range selected {
		if seen[identifier] {
			return nil, errors.New("behavior: an option cannot be selected twice")
		}
		seen[identifier] = true
	}
	ordered := make([]string, 0, len(selected))
	for _, option := range selection.Options {
		if seen[option.ID] {
			if option.Disabled {
				return nil, errors.New("behavior: this option is unavailable")
			}
			ordered = append(ordered, option.ID)
		}
	}
	if len(ordered) != len(selected) {
		return nil, errors.New("behavior: choose an available option")
	}
	return ordered, nil
}
