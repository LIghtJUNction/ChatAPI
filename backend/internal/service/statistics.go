package service

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/store"
)

type StatisticsSummary struct {
	TotalRequests            int            `json:"total_requests"`
	PendingRequests          int            `json:"pending_requests"`
	StreamingRequests        int            `json:"streaming_requests"`
	ClosedRequests           int            `json:"closed_requests"`
	AbortedRequests          int            `json:"aborted_requests"`
	AutomationHits           int            `json:"automation_hits"`
	OldestPendingWaitSeconds *int           `json:"oldest_pending_wait_seconds,omitempty"`
	ByStatus                 map[string]int `json:"by_status"`
	ByModel                  map[string]int `json:"by_model"`
	GeneratedAt              time.Time      `json:"generated_at"`
}

type RequestsOverview struct {
	TotalRequests            int            `json:"total_requests"`
	PendingRequests          int            `json:"pending_requests"`
	StreamingRequests        int            `json:"streaming_requests"`
	ClosedRequests           int            `json:"closed_requests"`
	AbortedRequests          int            `json:"aborted_requests"`
	AutomationHits           int            `json:"automation_hits"`
	OldestPendingWaitSeconds *int           `json:"oldest_pending_wait_seconds,omitempty"`
	ByStatus                 map[string]int `json:"by_status"`
	ByModel                  map[string]int `json:"by_model"`
	ByOwner                  map[string]int `json:"by_owner"`
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
	summary.AutomationHits, _ = s.store.CountAuditLogs(ctx, store.CountAuditLogsInput{
		EventType:   "automation.rule",
		ActorUserID: strings.TrimSpace(ownerID),
		Action:      "auto_complete",
		Outcome:     "success",
	})
	return summary, nil
}

func (s *ChatAPIService) RequestsOverview(ctx context.Context) (RequestsOverview, error) {
	items, err := s.ListRequests(ctx)
	if err != nil {
		return RequestsOverview{}, err
	}
	now := time.Now().UTC()
	overview := RequestsOverview{
		TotalRequests: len(items),
		ByStatus:      map[string]int{},
		ByModel:       map[string]int{},
		ByOwner:       map[string]int{},
		GeneratedAt:   now,
	}
	oldestPendingSeconds := math.MaxInt
	for _, item := range items {
		status := normalizedBucket(item.Status)
		model := normalizedBucket(item.Model)
		owner := normalizedBucket(item.OwnerID)
		overview.ByStatus[status]++
		overview.ByModel[model]++
		overview.ByOwner[owner]++
		switch status {
		case "waiting":
			overview.PendingRequests++
			oldestPendingSeconds = minPendingAge(oldestPendingSeconds, now, item.CreatedAt)
		case "streaming":
			overview.StreamingRequests++
			oldestPendingSeconds = minPendingAge(oldestPendingSeconds, now, item.CreatedAt)
		case "closed":
			overview.ClosedRequests++
		case "aborted":
			overview.AbortedRequests++
		}
	}
	if oldestPendingSeconds != math.MaxInt {
		overview.OldestPendingWaitSeconds = &oldestPendingSeconds
	}
	overview.AutomationHits, _ = s.store.CountAuditLogs(ctx, store.CountAuditLogsInput{
		EventType: "automation.rule",
		Action:    "auto_complete",
		Outcome:   "success",
	})
	return overview, nil
}

func normalizedBucket(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func minPendingAge(current int, now time.Time, createdAt time.Time) int {
	waitSeconds := int(now.Sub(createdAt).Seconds())
	if waitSeconds < 0 {
		waitSeconds = 0
	}
	if waitSeconds < current {
		return waitSeconds
	}
	return current
}
