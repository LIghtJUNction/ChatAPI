package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type remoteProcessor struct {
	config ProcessorConfig
	client *http.Client
}

type remoteJob struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Error     string `json:"error"`
	OutputURL string `json:"output_url"`
}

func newRemoteProcessor(config ProcessorConfig) Processor {
	return &remoteProcessor{config: config, client: &http.Client{
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many image processor redirects")
			}
			if !allowedDownloadScheme(request.URL) {
				return fmt.Errorf("unsupported image processor redirect scheme %q", request.URL.Scheme)
			}
			if len(via) > 0 && !sameOrigin(via[0].URL, request.URL) {
				request.Header.Del("Authorization")
				request.Header.Del("X-Image-Processor-Tenant")
			}
			return nil
		},
	}}
}

func (p *remoteProcessor) EncodeAVIF(ctx context.Context, data ParsedImage, options AVIFOptions) (ProcessedImage, error) {
	if len(data.Bytes) == 0 {
		return ProcessedImage{}, ErrInvalidImageInput
	}
	jobCtx, cancel := context.WithTimeout(ctx, p.config.JobTimeout)
	defer cancel()
	job, err := p.create(jobCtx, data.Bytes, options)
	if err != nil {
		return ProcessedImage{}, p.normalizeContextError(jobCtx, err)
	}
	delay := p.config.PollInterval
	for job.Status == "queued" || job.Status == "processing" {
		if err := waitForPoll(jobCtx, delay); err != nil {
			return ProcessedImage{}, p.normalizeContextError(jobCtx, err)
		}
		job, err = p.get(jobCtx, job.ID)
		if err != nil {
			return ProcessedImage{}, p.normalizeContextError(jobCtx, err)
		}
		delay = min(delay*2, p.config.MaxPollDelay)
	}
	if job.Status != "completed" || job.OutputURL == "" {
		return ProcessedImage{}, fmt.Errorf("%w: status=%s: %s", ErrProcessorFailed, job.Status, job.Error)
	}
	output, err := p.download(jobCtx, job.OutputURL)
	if err != nil {
		return ProcessedImage{}, p.normalizeContextError(jobCtx, err)
	}
	result, err := inspectProcessedAVIF(output)
	if err != nil {
		return ProcessedImage{}, err
	}
	return result, nil
}

func (p *remoteProcessor) create(ctx context.Context, data []byte, options AVIFOptions) (remoteJob, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	value, _ := json.Marshal(map[string]any{
		"mode": "static_avif", "priority": p.config.Priority,
		"preferred_quality": options.Quality, "minimum_quality": 20, "effort": 4,
		"max_input_bytes": len(data), "max_pixels": 50_000_000,
	})
	field, _ := writer.CreateFormField("options")
	_, _ = field.Write(value)
	file, _ := writer.CreateFormFile("file", "source")
	_, _ = file.Write(data)
	_ = writer.Close()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.URL+"/v1/jobs?wait=120", &body)
	if err != nil {
		return remoteJob{}, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	p.authorize(request, avifIdempotencyKey(data, options))
	return p.doJob(request)
}

func (p *remoteProcessor) get(ctx context.Context, id string) (remoteJob, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.config.URL+"/v1/jobs/"+url.PathEscape(id), nil)
	if err != nil {
		return remoteJob{}, err
	}
	p.authorize(request, "")
	return p.doJob(request)
}

func (p *remoteProcessor) doJob(request *http.Request) (remoteJob, error) {
	response, err := p.client.Do(request)
	if err != nil {
		return remoteJob{}, fmt.Errorf("%w: %v", ErrProcessorUnavailable, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return remoteJob{}, fmt.Errorf("%w: read response: %v", ErrProcessorUnavailable, err)
	}
	var job remoteJob
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = json.Unmarshal(body, &job)
		return remoteJob{}, fmt.Errorf("%w: returned %d: %s", ErrProcessorUnavailable, response.StatusCode, job.Error)
	}
	if err := json.Unmarshal(body, &job); err != nil {
		return remoteJob{}, fmt.Errorf("%w: decode response: %v", ErrProcessorUnavailable, err)
	}
	return job, nil
}

func (p *remoteProcessor) download(ctx context.Context, outputURL string) ([]byte, error) {
	target, err := url.Parse(outputURL)
	if err != nil {
		return nil, err
	}
	if !target.IsAbs() {
		base, parseErr := url.Parse(p.config.URL)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid processor base URL", ErrProcessorConfig)
		}
		target = base.ResolveReference(target)
	}
	if !allowedDownloadScheme(target) {
		return nil, fmt.Errorf("%w: unsupported output URL scheme %q", ErrProcessorFailed, target.Scheme)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(p.config.URL)
	if sameOrigin(base, target) {
		p.authorize(request, "")
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: download output: %v", ErrProcessorUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: download output returned %d", ErrProcessorUnavailable, response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, 100<<20))
}

func avifIdempotencyKey(data []byte, options AVIFOptions) string {
	inputHash := sha256.Sum256(data)
	identity, _ := json.Marshal(struct {
		Mode           string `json:"mode"`
		InputSHA256    string `json:"input_sha256"`
		Quality        int    `json:"preferred_quality"`
		MinimumQuality int    `json:"minimum_quality"`
		Effort         int    `json:"effort"`
	}{
		Mode: "static_avif", InputSHA256: fmt.Sprintf("%x", inputHash),
		Quality: options.Quality, MinimumQuality: 20, Effort: 4,
	})
	key := sha256.Sum256(identity)
	return "avif:" + fmt.Sprintf("%x", key)
}

func waitForPoll(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *remoteProcessor) normalizeContextError(ctx context.Context, err error) error {
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%w: exceeded %s", ErrProcessorTimeout, p.config.JobTimeout)
	}
	if ctx.Err() == context.Canceled {
		return context.Canceled
	}
	return err
}

func allowedDownloadScheme(target *url.URL) bool {
	return target != nil && (strings.EqualFold(target.Scheme, "http") || strings.EqualFold(target.Scheme, "https"))
}

func sameOrigin(left *url.URL, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func (p *remoteProcessor) authorize(request *http.Request, idempotencyKey string) {
	request.Header.Set("Authorization", "Bearer "+p.config.APIToken)
	request.Header.Set("X-Image-Processor-Tenant", p.config.Tenant)
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
}
