package importjob

import (
	"context"
	"fmt"
	"strings"
)

func (q *Queue) Metrics(ctx context.Context) (string, error) {
	var output strings.Builder
	rows, err := q.db.QueryContext(ctx, `SELECT state,COUNT(*) FROM import_jobs WHERE import_type IN ('IMAGE_AI','TEXT_AI') GROUP BY state`)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var state string
		var count int
		if err = rows.Scan(&state, &count); err != nil {
			rows.Close()
			return "", err
		}
		fmt.Fprintf(&output, "shiftory_ai_jobs{state=%q} %d\n", state, count)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	var pending int
	if err = q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM import_outbox WHERE state='PENDING'`).Scan(&pending); err != nil {
		return "", err
	}
	fmt.Fprintf(&output, "shiftory_ai_outbox_pending %d\n", pending)
	rows, err = q.db.QueryContext(ctx, `SELECT provider_id,code,COUNT(*),COALESCE(SUM(TIMESTAMPDIFF(MICROSECOND,started_at,finished_at))/1000000,0) FROM import_provider_calls GROUP BY provider_id,code`)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var p, code string
		var count int
		var duration float64
		if err = rows.Scan(&p, &code, &count, &duration); err != nil {
			rows.Close()
			return "", err
		}
		fmt.Fprintf(&output, "shiftory_ai_provider_calls{provider=%q,code=%q} %d\nshiftory_ai_provider_duration_seconds{provider=%q,code=%q} %f\n", p, code, count, p, code, duration)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	var duration float64
	err = q.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(TIMESTAMPDIFF(MICROSECOND,started_at,finished_at))/1000000,0) FROM import_attempts`).Scan(&duration)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&output, "shiftory_ai_round_duration_seconds %f\n", duration)
	info, e := q.inspector.GetQueueInfo(q.cfg.Tasks.Queue)
	ready := 0
	if e == nil {
		ready = 1
		fmt.Fprintf(&output, "shiftory_ai_queue_pending %d\nshiftory_ai_queue_active %d\nshiftory_ai_queue_retry %d\nshiftory_ai_queue_archived %d\n", info.Pending, info.Active, info.Retry, info.Archived)
	}
	fmt.Fprintf(&output, "shiftory_ai_queue_ready %d\n", ready)
	n, _ := q.Redis.Get(ctx, "shiftory:ai:metrics:"+q.cfg.Tasks.Queue+":idempotent").Int64()
	fmt.Fprintf(&output, "shiftory_ai_idempotent_shortcuts %d\n", n)
	for _, provider := range q.cfg.AI.Providers {
		rows, err := q.db.QueryContext(ctx, `SELECT config_fingerprint FROM import_attempts WHERE config_fingerprint<>'' ORDER BY id DESC LIMIT 1`)
		if err != nil {
			break
		}
		var fingerprint string
		if rows.Next() {
			_ = rows.Scan(&fingerprint)
		}
		rows.Close()
		if fingerprint == "" {
			continue
		}
		prefix := "shiftory:ai:" + fingerprint + ":" + provider.ID
		exists, _ := q.Redis.Exists(ctx, prefix+":cool", prefix+":invalid").Result()
		fmt.Fprintf(&output, "shiftory_ai_provider_blocked{provider=%q} %d\n", provider.ID, exists)
	}
	return output.String(), nil
}
