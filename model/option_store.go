package model

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"

	"golang.org/x/sync/singleflight"
)

const (
	defaultOptionSyncChannel   = "new-api:config_sync"
	OptionSyncEventUpdated     = "option.updated"
	OptionSyncEventBulkRefresh = "option.bulk_refresh"
)

type OptionSyncEvent struct {
	Event     string   `json:"event"`
	Key       string   `json:"key,omitempty"`
	Keys      []string `json:"keys,omitempty"`
	NodeID    string   `json:"node_id,omitempty"`
	UpdatedAt int64    `json:"updated_at"`
}

type optionStore struct {
	snapshot atomic.Value

	refreshGroup singleflight.Group
	listenOnce   sync.Once
}

var globalOptionStore = &optionStore{}

func cloneOptionMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func (s *optionStore) load() map[string]string {
	raw := s.snapshot.Load()
	if raw == nil {
		return map[string]string{}
	}
	snapshot, _ := raw.(map[string]string)
	if snapshot == nil {
		return map[string]string{}
	}
	return snapshot
}

func (s *optionStore) store(snapshot map[string]string) {
	s.snapshot.Store(snapshot)
}

func captureCompatOptionMap() map[string]string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return cloneOptionMap(common.OptionMap)
}

func SyncOptionStoreFromCompat() {
	snapshot := captureCompatOptionMap()
	globalOptionStore.store(snapshot)
}

func GetOptions() map[string]string {
	return cloneOptionMap(globalOptionStore.load())
}

func GetOption(key string) (string, bool) {
	snapshot := globalOptionStore.load()
	value, ok := snapshot[key]
	return value, ok
}

func optionSyncChannel() string {
	return common.GetEnvOrDefaultString("OPTION_SYNC_CHANNEL", defaultOptionSyncChannel)
}

func optionRefreshJitter() time.Duration {
	minMs := common.GetEnvOrDefault("OPTION_SYNC_JITTER_MIN_MS", 150)
	maxMs := common.GetEnvOrDefault("OPTION_SYNC_JITTER_MAX_MS", 600)
	if minMs < 0 {
		minMs = 0
	}
	if maxMs < minMs {
		maxMs = minMs
	}
	if maxMs == 0 {
		return 0
	}
	delta := maxMs - minMs
	if delta == 0 {
		return time.Duration(minMs) * time.Millisecond
	}
	return time.Duration(minMs+rand.Intn(delta+1)) * time.Millisecond
}

func RefreshOptionsFromDB(ctx context.Context, cause string, useJitter bool) error {
	_, err, _ := globalOptionStore.refreshGroup.Do("options-refresh", func() (interface{}, error) {
		if useJitter {
			jitter := optionRefreshJitter()
			if jitter > 0 {
				timer := time.NewTimer(jitter)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-timer.C:
				}
			}
		}

		options, err := AllOption()
		if err != nil {
			return nil, err
		}
		for _, option := range options {
			if option == nil {
				continue
			}
			if updateErr := updateOptionMap(option.Key, option.Value); updateErr != nil {
				common.SysLog(fmt.Sprintf("failed to update option map for key %s: %v", option.Key, updateErr))
			}
		}
		SyncOptionStoreFromCompat()
		common.SysLog(fmt.Sprintf("options refreshed from database: cause=%s count=%d", cause, len(options)))
		return nil, nil
	})
	return err
}

func PublishOptionSyncEvent(event OptionSyncEvent) error {
	if !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	payload, err := common.Marshal(event)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return common.RDB.Publish(ctx, optionSyncChannel(), payload).Err()
}

func StartOptionSyncListener() {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	globalOptionStore.listenOnce.Do(func() {
		go func() {
			for {
				ctx := context.Background()
				pubsub := common.RDB.Subscribe(ctx, optionSyncChannel())
				_, err := pubsub.Receive(ctx)
				if err != nil {
					common.SysLog("option sync subscribe failed: " + err.Error())
					_ = pubsub.Close()
					time.Sleep(3 * time.Second)
					continue
				}
				ch := pubsub.Channel()
				common.SysLog("option sync listener started: channel=" + optionSyncChannel())

				for msg := range ch {
					var event OptionSyncEvent
					if err := common.UnmarshalJsonStr(msg.Payload, &event); err != nil {
						common.SysLog("invalid option sync payload: " + err.Error())
						continue
					}
					if event.NodeID != "" && event.NodeID == common.NodeID {
						continue
					}
					if err := RefreshOptionsFromDB(context.Background(), event.Event, true); err != nil && err != context.Canceled {
						common.SysLog("failed to refresh options from sync event: " + err.Error())
					}
				}

				_ = pubsub.Close()
				time.Sleep(500 * time.Millisecond)
			}
		}()
	})
}
