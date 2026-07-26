//go:build shared_image_processor

package media

import (
	"errors"
	"testing"
)

func TestRemoteOnlyProcessorRequiresConfiguration(t *testing.T) {
	if _, err := NewProcessor(ProcessorConfig{}); !errors.Is(err, ErrProcessorConfig) {
		t.Fatalf("expected fail-fast processor configuration error, got %v", err)
	}
}
