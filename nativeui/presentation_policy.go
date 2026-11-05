package nativeui

import (
	"errors"

	"github.com/DonaldMurillo/gofastr-mobile/behavior"
)

func validatePresentation(presentation Presentation, continuations map[string]Respond) error {
	if presentation.ID == "" || len(presentation.ID) > 128 || continuations[presentation.ID] == nil || presentation.Title == "" || len(presentation.Title) > 4096 || len(presentation.Message) > 65536 {
		return errors.New("nativeui: a presentation requires a title and registered continuation")
	}
	switch presentation.Kind {
	case PresentationAlert, PresentationConfirmation, PresentationMenu:
		if presentation.Selection != nil {
			return errors.New("nativeui: selection requires a dialog or sheet")
		}
	case PresentationDialog, PresentationSheet:
	default:
		return errors.New("nativeui: unsupported presentation kind")
	}
	for _, choice := range presentation.Choices {
		if choice.Label == "" || len(choice.Label) > 512 || (choice.Role != "" && choice.Role != "cancel" && choice.Role != "destructive") {
			return errors.New("nativeui: invalid presentation choice")
		}
	}
	if presentation.Selection != nil {
		if presentation.Selection.Key != "" || presentation.Selection.Maximum != 0 {
			return errors.New("nativeui: collection selection keys do not apply to presentations")
		}
		for _, option := range presentation.Selection.Options {
			if option.Label == "" || len(option.Label) > 512 || len(option.Detail) > 4096 {
				return errors.New("nativeui: invalid selection label")
			}
		}
	}
	_, err := behavior.NewPresentation("validation", presentationSpec(presentation))
	return err
}

func presentationSpec(presentation Presentation) behavior.PresentationSpec {
	spec := behavior.PresentationSpec{Dismissible: presentation.Dismissible}
	for _, choice := range presentation.Choices {
		spec.Choices = append(spec.Choices, behavior.Choice{ID: choice.ID, Disabled: choice.Disabled, Cancel: choice.Role == "cancel"})
	}
	if presentation.Selection != nil {
		selection := presentation.Selection
		spec.Selection = &behavior.Selection{Selected: selection.Selected, Multiple: selection.Multiple, Min: selection.Min, Max: selection.Max}
		for _, option := range selection.Options {
			spec.Selection.Options = append(spec.Selection.Options, behavior.Option{ID: option.ID, Disabled: option.Disabled})
		}
	}
	return spec
}
