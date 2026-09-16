package event

import (
	"accelerator/internal/core/error_type"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type HTTPPublisher struct {
	client     *http.Client
	targetURL  string
}

func NewHTTPPublisher(targetURL string) *HTTPPublisher {
	return &HTTPPublisher{
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		targetURL: targetURL,
	}
}

func (p *HTTPPublisher) Publish(ctx context.Context, event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return error_type.NewInternal(fmt.Errorf("failed to marshal event: %w", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.targetURL, bytes.NewReader(data))
	if err != nil {
		return error_type.NewInternal(fmt.Errorf("failed to create request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return error_type.NewServiceUnavailable("notification service is unavailable")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return error_type.NewServiceUnavailable(fmt.Sprintf("notification service returned status %d", resp.StatusCode))
	}

	return nil
}