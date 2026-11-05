package behavior

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
)

const MaximumSelection = 200

type CollectionState struct {
	Query    Values
	Page     string
	Selected []string
}

func (state CollectionState) WithQuery(query Values) CollectionState {
	if !maps.Equal(state.Query, query) {
		return CollectionState{Query: maps.Clone(query), Selected: []string{}}
	}
	return CollectionState{Query: maps.Clone(query), Page: state.Page, Selected: slices.Clone(state.Selected)}
}

func (state CollectionState) LoadMore(next string) (CollectionState, error) {
	if !SafeToken(next) || next == state.Page {
		return CollectionState{}, errors.New("Choose a new, valid page.")
	}
	state = state.WithQuery(state.Query)
	state.Page = next
	return state, nil
}

func SelectionMaximum(maximum int) (int, error) {
	if maximum == 0 {
		maximum = MaximumSelection
	}
	if maximum < 1 || maximum > MaximumSelection {
		return 0, errors.New("Selection maximum must be between 1 and 200.")
	}
	return maximum, nil
}

func ValidateSelection(selected []string, available map[string]bool, maximum int) error {
	maximum, err := SelectionMaximum(maximum)
	if err != nil {
		return err
	}
	if len(selected) > maximum {
		return errors.New("Too many records selected.")
	}
	seen := make(map[string]bool, len(selected))
	for _, identifier := range selected {
		if !SafeToken(identifier) || !available[identifier] || seen[identifier] {
			return errors.New("Choose distinct, available records from this list.")
		}
		seen[identifier] = true
	}
	return nil
}

func DecodeSelection(encoded string, available map[string]bool, maximum int) ([]string, error) {
	if len(encoded) > MaximumSelection*132+2 || !strings.HasPrefix(strings.TrimSpace(encoded), "[") {
		return nil, errors.New("Selection must be a JSON array of record IDs.")
	}
	var selected []string
	if err := json.Unmarshal([]byte(encoded), &selected); err != nil {
		return nil, errors.New("Selection must be a JSON array of record IDs.")
	}
	if err := ValidateSelection(selected, available, maximum); err != nil {
		return nil, err
	}
	return selected, nil
}

func SafeToken(token string) bool {
	if token == "" || len(token) > 128 || token == "." || token == ".." {
		return false
	}
	for _, character := range token {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' || character == ':' {
			continue
		}
		return false
	}
	return true
}
