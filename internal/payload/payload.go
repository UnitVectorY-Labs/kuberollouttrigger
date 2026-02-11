package payload

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Event represents the webhook event payload.
type Event struct {
	Image string   `json:"image"`
	Tags  []string `json:"tags"`
}

// ParseAndValidate parses JSON bytes into an Event and validates all fields.
// allowedPrefix is the required prefix for the image field.
func ParseAndValidate(data []byte, allowedPrefix string) (*Event, error) {
	// Reject unexpected fields by using a strict decoder
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()

	var evt Event
	if err := dec.Decode(&evt); err != nil {
		return nil, fmt.Errorf("invalid JSON payload: %w", err)
	}

	// Ensure no trailing content
	if dec.More() {
		return nil, fmt.Errorf("invalid JSON payload: unexpected trailing content")
	}

	if err := ValidateEvent(&evt, allowedPrefix); err != nil {
		return nil, err
	}

	return &evt, nil
}

// ValidateEvent validates an already-parsed Event.
func ValidateEvent(evt *Event, allowedPrefix string) error {
	if evt.Image == "" {
		return fmt.Errorf("missing required field: image")
	}
	if len(evt.Tags) == 0 {
		return fmt.Errorf("missing required field: tags (must be a non-empty array)")
	}

	// Validate each tag is non-empty
	for i, tag := range evt.Tags {
		if tag == "" {
			return fmt.Errorf("tags[%d] is empty", i)
		}
	}

	// Validate image starts with allowed prefix
	if !strings.HasPrefix(evt.Image, allowedPrefix) {
		return fmt.Errorf("image %q does not start with allowed prefix %q", evt.Image, allowedPrefix)
	}

	// Validate image looks like a container image reference (registry/path format)
	if !strings.Contains(evt.Image, "/") {
		return fmt.Errorf("image %q is not a valid container image reference", evt.Image)
	}

	return nil
}

// ImageRefs returns all full image references (image:tag) for each tag in the event.
func (e *Event) ImageRefs() []string {
	refs := make([]string, len(e.Tags))
	for i, tag := range e.Tags {
		refs[i] = e.Image + ":" + tag
	}
	return refs
}

// ToJSON serializes the event to minimized JSON.
func (e *Event) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}
