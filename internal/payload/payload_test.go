package payload

import (
	"testing"
)

func TestParseAndValidate_Valid(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		prefix string
	}{
		{
			name:   "basic valid event with single tag",
			input:  `{"image":"ghcr.io/test-org/myservice","tags":["dev"]}`,
			prefix: "ghcr.io/test-org/",
		},
		{
			name:   "event with multiple tags",
			input:  `{"image":"ghcr.io/test-org/myservice","tags":["dev","v1.0.0","latest"]}`,
			prefix: "ghcr.io/test-org/",
		},
		{
			name:   "nested path",
			input:  `{"image":"ghcr.io/test-org/sub/path/myservice","tags":["latest"]}`,
			prefix: "ghcr.io/test-org/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt, err := ParseAndValidate([]byte(tt.input), tt.prefix)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if evt.Image == "" {
				t.Error("expected image to be set")
			}
			if len(evt.Tags) == 0 {
				t.Error("expected tags to be set")
			}
		})
	}
}

func TestParseAndValidate_Invalid(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		prefix string
	}{
		{
			name:   "empty JSON",
			input:  `{}`,
			prefix: "ghcr.io/test/",
		},
		{
			name:   "missing image",
			input:  `{"tags":["dev"]}`,
			prefix: "ghcr.io/test/",
		},
		{
			name:   "missing tags",
			input:  `{"image":"ghcr.io/test/myservice"}`,
			prefix: "ghcr.io/test/",
		},
		{
			name:   "empty tags array",
			input:  `{"image":"ghcr.io/test/myservice","tags":[]}`,
			prefix: "ghcr.io/test/",
		},
		{
			name:   "tags array with empty string",
			input:  `{"image":"ghcr.io/test/myservice","tags":["dev",""]}`,
			prefix: "ghcr.io/test/",
		},
		{
			name:   "wrong prefix",
			input:  `{"image":"docker.io/test/myservice","tags":["dev"]}`,
			prefix: "ghcr.io/test/",
		},
		{
			name:   "unknown field",
			input:  `{"image":"ghcr.io/test/myservice","tags":["dev"],"unknown":"field"}`,
			prefix: "ghcr.io/test/",
		},
		{
			name:   "invalid JSON",
			input:  `not json`,
			prefix: "ghcr.io/test/",
		},
		{
			name:   "image without slash",
			input:  `{"image":"ghcr.io","tags":["dev"]}`,
			prefix: "ghcr.io",
		},
		{
			name:   "trailing content",
			input:  `{"image":"ghcr.io/test/myservice","tags":["dev"]}extra`,
			prefix: "ghcr.io/test/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAndValidate([]byte(tt.input), tt.prefix)
			if err == nil {
				t.Fatal("expected error but got nil")
			}
		})
	}
}

func TestEvent_ImageRefs(t *testing.T) {
	tests := []struct {
		name     string
		evt      *Event
		expected []string
	}{
		{
			name: "single tag",
			evt:  &Event{Image: "ghcr.io/test/myservice", Tags: []string{"dev"}},
			expected: []string{
				"ghcr.io/test/myservice:dev",
			},
		},
		{
			name: "multiple tags",
			evt:  &Event{Image: "ghcr.io/test/myservice", Tags: []string{"dev", "v1.0.0", "latest"}},
			expected: []string{
				"ghcr.io/test/myservice:dev",
				"ghcr.io/test/myservice:v1.0.0",
				"ghcr.io/test/myservice:latest",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.evt.ImageRefs()
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d refs, got %d", len(tt.expected), len(got))
			}
			for i, exp := range tt.expected {
				if got[i] != exp {
					t.Errorf("expected ref[%d] = %s, got %s", i, exp, got[i])
				}
			}
		})
	}
}

func TestEvent_ToJSON(t *testing.T) {
	evt := &Event{Image: "ghcr.io/test/myservice", Tags: []string{"dev"}}
	data, err := evt.ToJSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `{"image":"ghcr.io/test/myservice","tags":["dev"]}`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}
