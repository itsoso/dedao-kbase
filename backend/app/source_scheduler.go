package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const defaultSourceSchedulerTickInterval = 30 * time.Second

type SourceSchedulerTickResult struct {
	Evaluated       int `json:"evaluated"`
	Queued          int `json:"queued"`
	Retried         int `json:"retried"`
	SkippedDisabled int `json:"skipped_disabled"`
	SkippedManual   int `json:"skipped_manual"`
	SkippedFuture   int `json:"skipped_future"`
	SkippedActive   int `json:"skipped_active"`
	SkippedBlocked  int `json:"skipped_blocked"`
	InvalidSchedule int `json:"invalid_schedule"`
}

type SourceScheduler struct {
	store *SourceSyncStore
	now   func() time.Time
}

func NewSourceScheduler(store *SourceSyncStore, now func() time.Time) (*SourceScheduler, error) {
	if store == nil || store.db == nil {
		return nil, fmt.Errorf("source sync store is required")
	}
	if now == nil {
		now = time.Now
	}
	return &SourceScheduler{store: store, now: now}, nil
}

func (s *SourceScheduler) Tick() (SourceSchedulerTickResult, error) {
	var result SourceSchedulerTickResult
	subscriptions, err := s.store.ListSubscriptions()
	if err != nil {
		return result, err
	}
	now := s.now().UTC()
	for _, subscription := range subscriptions {
		result.Evaluated++
		if !subscription.Enabled {
			result.SkippedDisabled++
			continue
		}
		interval, scheduled, err := sourceSubscriptionInterval(subscription.Schedule)
		if err != nil {
			result.InvalidSchedule++
			continue
		}
		if !scheduled {
			result.SkippedManual++
			continue
		}

		latest, found, err := s.latestRun(subscription.ID)
		if err != nil {
			return result, err
		}
		if found && isActiveSourceRunStatus(latest.Status) {
			result.SkippedActive++
			continue
		}
		anchor, err := sourceSubscriptionScheduleAnchor(subscription, latest, found)
		if err != nil {
			result.InvalidSchedule++
			continue
		}
		if now.Before(anchor.Add(interval)) {
			result.SkippedFuture++
			continue
		}
		if found && latest.Status == SourceRunFailed && sourceScheduleFailureBlocked(latest.Error) {
			result.SkippedBlocked++
			continue
		}

		if found && latest.Status == SourceRunFailed {
			_, err = s.store.RetryRun(latest.ID)
			if err == nil {
				result.Queued++
				result.Retried++
				continue
			}
		} else {
			_, err = s.store.CreateRun(subscription.ID, subscription.Operation)
			if err == nil {
				result.Queued++
				continue
			}
		}
		if errors.Is(err, ErrSourceRunActive) {
			result.SkippedActive++
			continue
		}
		return result, err
	}
	return result, nil
}

func (s *SourceScheduler) Run(
	ctx context.Context,
	interval time.Duration,
	onTick func(SourceSchedulerTickResult, error),
) {
	if interval <= 0 {
		interval = defaultSourceSchedulerTickInterval
	}
	runTick := func() {
		result, err := s.Tick()
		if onTick != nil {
			onTick(result, err)
		}
	}
	runTick()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runTick()
		}
	}
}

func (s *SourceScheduler) latestRun(subscriptionID string) (SourceSyncRun, bool, error) {
	run, err := scanSourceSyncRun(s.store.db.QueryRow(sourceSyncRunSelect+`
		WHERE subscription_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, strings.TrimSpace(subscriptionID)))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceSyncRun{}, false, nil
	}
	return run, err == nil, err
}

func sourceSubscriptionInterval(schedule string) (time.Duration, bool, error) {
	schedule = strings.TrimSpace(strings.ToLower(schedule))
	if schedule == "" || schedule == "manual" {
		return 0, false, nil
	}
	secondsText := schedule
	if strings.HasPrefix(secondsText, "interval:") {
		secondsText = strings.TrimSpace(strings.TrimPrefix(secondsText, "interval:"))
	}
	seconds, err := strconv.ParseInt(secondsText, 10, 64)
	if err != nil || seconds <= 0 {
		return 0, false, fmt.Errorf("schedule must be manual or a positive interval in seconds")
	}
	maximumSeconds := int64((365 * 24 * time.Hour) / time.Second)
	if seconds > maximumSeconds {
		return 0, false, fmt.Errorf("schedule interval exceeds one year")
	}
	return time.Duration(seconds) * time.Second, true, nil
}

func sourceSubscriptionScheduleAnchor(subscription SourceSubscription, latest SourceSyncRun, found bool) (time.Time, error) {
	value := subscription.CreatedAt
	if found {
		value = latest.FinishedAt
		if strings.TrimSpace(value) == "" {
			value = latest.UpdatedAt
		}
	}
	return time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
}

func isActiveSourceRunStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case SourceRunQueued, SourceRunLeased, SourceRunRunning:
		return true
	default:
		return false
	}
}

func sourceScheduleFailureBlocked(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	for _, marker := range []string{
		"not_max_version",
		"unactivated",
		"not activated",
		"unauthorized",
		"forbidden",
		"http 401",
		"http 403",
		"license",
		"licence",
		"throttl",
		"too many requests",
		"parameter expired",
		"parameter_expired",
		"request parameter expired",
		"req_data expired",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
