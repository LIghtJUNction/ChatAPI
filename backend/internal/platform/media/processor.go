package media

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

var (
	ErrProcessorConfig      = errors.New("invalid image processor configuration")
	ErrProcessorTimeout     = errors.New("image processor job timed out")
	ErrProcessorFailed      = errors.New("image processor job failed")
	ErrProcessorUnavailable = errors.New("image processor unavailable")
)

type Processor interface {
	EncodeAVIF(context.Context, ParsedImage, AVIFOptions) (ProcessedImage, error)
}

type ProcessedImage struct {
	Bytes     []byte
	MediaType string
	Width     int
	Height    int
}

type ProcessorConfig struct {
	URL          string
	APIToken     string
	Tenant       string
	Priority     int
	JobTimeout   time.Duration
	PollInterval time.Duration
	MaxPollDelay time.Duration
}

func NewProcessor(config ProcessorConfig) (Processor, error) {
	if config.Tenant == "" {
		config.Tenant = "chatapi"
	}
	if config.Priority == 0 {
		config.Priority = 100
	}
	if config.JobTimeout <= 0 {
		config.JobTimeout = 5 * time.Minute
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 250 * time.Millisecond
	}
	if config.MaxPollDelay <= 0 {
		config.MaxPollDelay = 2 * time.Second
	}
	return newProcessor(config)
}

func validateRemoteConfig(config ProcessorConfig) error {
	target, err := url.Parse(strings.TrimSpace(config.URL))
	if err != nil || target.Host == "" || !allowedDownloadScheme(target) {
		return fmt.Errorf("%w: shared processor URL must be an absolute HTTP(S) URL", ErrProcessorConfig)
	}
	if strings.TrimSpace(config.APIToken) == "" {
		return fmt.Errorf("%w: shared processor API token is required", ErrProcessorConfig)
	}
	return nil
}

func inspectProcessedAVIF(data []byte) (ProcessedImage, error) {
	mediaType, width, height, err := InspectImageBytes(data)
	if err != nil {
		return ProcessedImage{}, fmt.Errorf("%w: inspect AVIF output: %v", ErrProcessorFailed, err)
	}
	if !strings.EqualFold(mediaType, "image/avif") || width <= 0 || height <= 0 {
		return ProcessedImage{}, fmt.Errorf("%w: processor output is not a valid AVIF image", ErrProcessorFailed)
	}
	return ProcessedImage{Bytes: data, MediaType: "image/avif", Width: width, Height: height}, nil
}
