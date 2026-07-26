//go:build !shared_image_processor

package media

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/gen2brain/avif"
)

type localProcessor struct{}

func newProcessor(config ProcessorConfig) (Processor, error) {
	hasURL := strings.TrimSpace(config.URL) != ""
	hasToken := strings.TrimSpace(config.APIToken) != ""
	if hasURL != hasToken {
		return nil, fmt.Errorf("%w: shared processor URL and API token must be configured together", ErrProcessorConfig)
	}
	if hasURL {
		if err := validateRemoteConfig(config); err != nil {
			return nil, err
		}
		return newRemoteProcessor(config), nil
	}
	return localProcessor{}, nil
}

func (localProcessor) EncodeAVIF(_ context.Context, data ParsedImage, options AVIFOptions) (ProcessedImage, error) {
	if len(data.Bytes) == 0 {
		return ProcessedImage{}, ErrInvalidImageInput
	}
	image, detected, _, _, err := decodeImage(bytes.NewReader(data.Bytes))
	if err != nil {
		return ProcessedImage{}, err
	}
	if options.Quality <= 0 {
		options.Quality = avif.DefaultQuality
	}
	var output bytes.Buffer
	if err := avif.Encode(&output, image, avif.Options{
		Quality: options.Quality, QualityAlpha: options.Quality, Speed: avif.DefaultSpeed,
	}); err != nil {
		return ProcessedImage{}, fmt.Errorf("encode avif from %s: %w", detected, err)
	}
	return inspectProcessedAVIF(output.Bytes())
}
