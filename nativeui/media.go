package nativeui

// Media is the validated, renderer-independent content of a private attachment
// preview. It contains no remote URLs or paths; component libraries may reuse it.
type Media struct {
	Images      []Image      `json:"images,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	Preview     *FilePreview `json:"preview,omitempty"`
}

func (media Media) Validate() error {
	if err := validateImages(media.Images); err != nil {
		return err
	}
	if err := validateAttachments(media.Attachments); err != nil {
		return err
	}
	if media.Preview != nil {
		return validateFilePreview(*media.Preview)
	}
	return nil
}
