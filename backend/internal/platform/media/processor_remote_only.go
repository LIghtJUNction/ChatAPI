//go:build shared_image_processor

package media

import (
	"fmt"
	"strings"
)

func newProcessor(config ProcessorConfig) (Processor, error) {
	if strings.TrimSpace(config.URL) == "" || strings.TrimSpace(config.APIToken) == "" {
		return nil, fmt.Errorf("%w: shared processor URL and API token are required by this build", ErrProcessorConfig)
	}
	if err := validateRemoteConfig(config); err != nil {
		return nil, err
	}
	return newRemoteProcessor(config), nil
}
