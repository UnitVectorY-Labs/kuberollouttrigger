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
			name:   "basic valid event",
			input:  `{"image":"ghcr.io/test-org/myservice","tag":"dev"}`,
			prefix: "ghcr.io/test-org/",
		},
		{
			name:   "with digest",
			input:  `{"image":"ghcr.io/test-org/myservice","tag":"dev","digest":"sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"}`,
			prefix: "ghcr.io/test-org/",
		},
		{
			name:   "nested path",
			input:  `{"image":"ghcr.io/test-org/sub/path/myservice","tag":"latest"}`,
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
			if evt.Tag == "" {
				t.Error("expected tag to be set")
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
			input:  `{"tag":"dev"}`,
			prefix: "ghcr.io/test/",
		},
		{
			name:   "missing tag",
			input:  `{"image":"ghcr.io/test/myservice"}`,
			prefix: "ghcr.io/test/",
		},
		{
			name:   "wrong prefix",
			input:  `{"image":"docker.io/test/myservice","tag":"dev"}`,
			prefix: "ghcr.io/test/",
		},
		{
			name:   "unknown field",
			input:  `{"image":"ghcr.io/test/myservice","tag":"dev","unknown":"field"}`,
			prefix: "ghcr.io/test/",
		},
		{
			name:   "invalid digest format",
			input:  `{"image":"ghcr.io/test/myservice","tag":"dev","digest":"invalid"}`,
			prefix: "ghcr.io/test/",
		},
		{
			name:   "empty tag",
			input:  `{"image":"ghcr.io/test/myservice","tag":""}`,
			prefix: "ghcr.io/test/",
		},
		{
			name:   "invalid JSON",
			input:  `not json`,
			prefix: "ghcr.io/test/",
		},
		{
			name:   "image without slash",
			input:  `{"image":"ghcr.io","tag":"dev"}`,
			prefix: "ghcr.io",
		},
		{
			name:   "trailing content",
			input:  `{"image":"ghcr.io/test/myservice","tag":"dev"}extra`,
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

func TestEvent_ImageRef(t *testing.T) {
	evt := &Event{Image: "ghcr.io/test/myservice", Tag: "dev"}
	if got := evt.ImageRef(); got != "ghcr.io/test/myservice:dev" {
		t.Errorf("expected ghcr.io/test/myservice:dev, got %s", got)
	}
}

func TestEvent_ToJSON(t *testing.T) {
	evt := &Event{Image: "ghcr.io/test/myservice", Tag: "dev"}
	data, err := evt.ToJSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `{"image":"ghcr.io/test/myservice","tag":"dev"}`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}

func TestEvent_ToJSON_WithDigest(t *testing.T) {
	evt := &Event{
		Image:  "ghcr.io/test/myservice",
		Tag:    "dev",
		Digest: "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	}
	data, err := evt.ToJSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify round-trip
	evt2, err := ParseAndValidate(data, "ghcr.io/test/")
	if err != nil {
		t.Fatalf("round-trip failed: %v", err)
	}
	if evt2.Image != evt.Image || evt2.Tag != evt.Tag || evt2.Digest != evt.Digest {
		t.Error("round-trip mismatch")
	}
}

func TestValidateEvent_DigestFormats(t *testing.T) {
	validDigest := "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	tests := []struct {
		name    string
		digest  string
		wantErr bool
	}{
		{"valid sha256", validDigest, false},
		{"empty digest", "", false},
		{"short digest", "sha256:abc", true},
		{"wrong prefix", "md5:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", true},
		{"uppercase hex", "sha256:ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := &Event{Image: "ghcr.io/test/svc", Tag: "dev", Digest: tt.digest}
			err := ValidateEvent(evt, "ghcr.io/test/")
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEvent() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
