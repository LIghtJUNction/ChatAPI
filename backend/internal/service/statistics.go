package service

import (
	"context"
	"math"
	"strings"
	"time"
)

type StatisticsSummary struct {
	TotalRequests            int            `json:"total_requests"`
	PendingRequests          int            `json:"pending_requests"`
	StreamingRequests        int            `json:"streaming_requests"`
	ClosedRequests           int            `json:"closed_requests"`
	AbortedRequests          int            `json:"aborted_requests"`
	OldestPendingWaitSeconds *int           `json:"oldest_pending_wait_seconds,omitempty"`
	ByStatus                 map[string]int `json:"by_status"`
	ByModel                  map[string]int `json:"by_model"`
	GeneratedAt              time.Time      `json:"generated_at"`
}

func (s *ChatAPIService) StatisticsSummaryForOwner(ctx context.Context, ownerID string) (StatisticsSummary, error) {
	items, err := s.ListRequestsForOwner(ctx, strings.TrimSpace(ownerID))
	if err != nil {
		return StatisticsSummary{}, err
	}
	now := time.Now().UTC()
	summary := StatisticsSummary{
		TotalRequests: len(items),
		ByStatus:      map[string]int{},
		ByModel:       map[string]int{},
		GeneratedAt:   now,
	}
	oldestPendingSeconds := math.MaxInt
	for _, item := range items {
		status := strings.TrimSpace(item.Status)
		if status == "" {
			status = "unknown"
		}
		summary.ByStatus[status]++
		model := strings.TrimSpace(item.Model)
		if model == "" {
			model = "unknown"
		}
		summary.ByModel[model]++
		switch status {
		case "waiting":
			summary.PendingRequests++
			waitSeconds := int(now.Sub(item.CreatedAt).Seconds())
			if waitSeconds < 0 {
				waitSeconds = 0
			}
			if waitSeconds < oldestPendingSeconds {
				oldestPendingSeconds = waitSeconds
			}
		case "streaming":
			summary.StreamingRequests++
			waitSeconds := int(now.Sub(item.CreatedAt).Seconds())
			if waitSeconds < 0 {
				waitSeconds = 0
			}
			if waitSeconds < oldestPendingSeconds {
				oldestPendingSeconds = waitSeconds
			}
		case "closed":
			summary.ClosedRequests++
		case "aborted":
			summary.AbortedRequests++
		}
	}
	if oldestPendingSeconds != math.MaxInt {
		summary.OldestPendingWaitSeconds = &oldestPendingSeconds
	}
	return summary, nil
}
