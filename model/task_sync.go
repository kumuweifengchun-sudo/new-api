package model

import (
	"context"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/go-redis/redis/v8"
)

const (
	defaultTaskSyncChannel          = "new-api:task_sync"
	defaultTaskPendingSetKey        = "new-api:tasks:pending"
	defaultMidjourneyPendingSetKey  = "new-api:midjourney:pending"
	taskSyncEventKindTask           = "task"
	taskSyncEventKindMidjourney     = "midjourney"
	taskSyncEventPendingUpdated     = "pending.updated"
	defaultTaskPollIntervalSeconds  = 15
	defaultTaskFallbackIntervalSecs = 60
	defaultTaskPendingBatchSize     = 200
)

type TaskSyncEvent struct {
	Kind      string  `json:"kind"`
	Event     string  `json:"event"`
	IDs       []int64 `json:"ids,omitempty"`
	NodeID    string  `json:"node_id,omitempty"`
	UpdatedAt int64   `json:"updated_at"`
}

func activeTaskStatuses() []TaskStatus {
	return []TaskStatus{
		TaskStatusNotStart,
		TaskStatusSubmitted,
		TaskStatusQueued,
		TaskStatusInProgress,
	}
}

func isActiveTaskStatus(status TaskStatus) bool {
	switch status {
	case TaskStatusFailure, TaskStatusSuccess:
		return false
	default:
		return true
	}
}

func IsActiveTaskStatus(status TaskStatus) bool {
	return isActiveTaskStatus(status)
}

func isActiveMidjourneyStatus(status string) bool {
	switch status {
	case "SUCCESS", "FAILURE":
		return false
	default:
		return true
	}
}

func IsActiveMidjourneyStatus(status string) bool {
	return isActiveMidjourneyStatus(status)
}

func taskSyncChannel() string {
	return common.GetEnvOrDefaultString("TASK_SYNC_CHANNEL", defaultTaskSyncChannel)
}

func taskPendingSetKey() string {
	return common.GetEnvOrDefaultString("TASK_PENDING_SET_KEY", defaultTaskPendingSetKey)
}

func midjourneyPendingSetKey() string {
	return common.GetEnvOrDefaultString("MIDJOURNEY_PENDING_SET_KEY", defaultMidjourneyPendingSetKey)
}

func TaskPollInterval() time.Duration {
	return time.Duration(common.GetEnvOrDefault("TASK_POLL_INTERVAL_SECONDS", defaultTaskPollIntervalSeconds)) * time.Second
}

func TaskFallbackInterval() time.Duration {
	return time.Duration(common.GetEnvOrDefault("TASK_SYNC_FALLBACK_INTERVAL_SECONDS", defaultTaskFallbackIntervalSecs)) * time.Second
}

func TaskPendingBatchSize() int {
	return common.GetEnvOrDefault("TASK_PENDING_BATCH_SIZE", defaultTaskPendingBatchSize)
}

func publishTaskSyncEvent(event TaskSyncEvent) error {
	if !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	payload, err := common.Marshal(event)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return common.RDB.Publish(ctx, taskSyncChannel(), payload).Err()
}

func addPendingIDs(key string, ids []int64, delay time.Duration, publish bool, kind string) error {
	if len(ids) == 0 || !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	score := float64(time.Now().Add(delay).Unix())
	members := make([]*redis.Z, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		members = append(members, &redis.Z{Score: score, Member: strconv.FormatInt(id, 10)})
	}
	if len(members) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := common.RDB.ZAdd(ctx, key, members...).Err(); err != nil {
		return err
	}
	if publish {
		return publishTaskSyncEvent(TaskSyncEvent{
			Kind:      kind,
			Event:     taskSyncEventPendingUpdated,
			IDs:       ids,
			NodeID:    common.NodeID,
			UpdatedAt: time.Now().Unix(),
		})
	}
	return nil
}

func removePendingIDs(key string, ids []int64) error {
	if len(ids) == 0 || !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	members := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		members = append(members, strconv.FormatInt(id, 10))
	}
	if len(members) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return common.RDB.ZRem(ctx, key, members...).Err()
}

func fetchDuePendingIDs(key string, limit int) ([]int64, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = TaskPendingBatchSize()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	entries, err := common.RDB.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min:    "-inf",
		Max:    strconv.FormatInt(time.Now().Unix(), 10),
		Offset: 0,
		Count:  int64(limit),
	}).Result()
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(entries))
	for _, entry := range entries {
		id, convErr := strconv.ParseInt(entry, 10, 64)
		if convErr == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func nextPendingDelay(key string, fallback time.Duration) time.Duration {
	if !common.RedisEnabled || common.RDB == nil {
		return fallback
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	result, err := common.RDB.ZRangeWithScores(ctx, key, 0, 0).Result()
	if err != nil || len(result) == 0 {
		return fallback
	}
	nextAt := time.Unix(int64(result[0].Score), 0)
	delay := time.Until(nextAt)
	if delay < 0 {
		return 0
	}
	if delay > fallback {
		return fallback
	}
	return delay
}

func SubscribeTaskSync() *redis.PubSub {
	if !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	return common.RDB.Subscribe(context.Background(), taskSyncChannel())
}

func RegisterPendingTask(id int64) error {
	return addPendingIDs(taskPendingSetKey(), []int64{id}, 0, true, taskSyncEventKindTask)
}

func SchedulePendingTasks(ids []int64, delay time.Duration) error {
	return addPendingIDs(taskPendingSetKey(), ids, delay, false, taskSyncEventKindTask)
}

func RemovePendingTasks(ids []int64) error {
	return removePendingIDs(taskPendingSetKey(), ids)
}

func FetchDuePendingTaskIDs(limit int) ([]int64, error) {
	return fetchDuePendingIDs(taskPendingSetKey(), limit)
}

func NextPendingTaskDelay(fallback time.Duration) time.Duration {
	return nextPendingDelay(taskPendingSetKey(), fallback)
}

func RegisterPendingMidjourney(id int64) error {
	return addPendingIDs(midjourneyPendingSetKey(), []int64{id}, 0, true, taskSyncEventKindMidjourney)
}

func SchedulePendingMidjourney(ids []int64, delay time.Duration) error {
	return addPendingIDs(midjourneyPendingSetKey(), ids, delay, false, taskSyncEventKindMidjourney)
}

func RemovePendingMidjourney(ids []int64) error {
	return removePendingIDs(midjourneyPendingSetKey(), ids)
}

func FetchDuePendingMidjourneyIDs(limit int) ([]int64, error) {
	return fetchDuePendingIDs(midjourneyPendingSetKey(), limit)
}

func NextPendingMidjourneyDelay(fallback time.Duration) time.Duration {
	return nextPendingDelay(midjourneyPendingSetKey(), fallback)
}
