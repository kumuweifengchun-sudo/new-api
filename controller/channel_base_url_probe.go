package controller

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

var channelBaseURLProbeTaskOnce sync.Once

func parseDailyClock(raw string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time format")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("invalid hour")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid minute")
	}
	return hour, minute, nil
}

func nextDailyRun(now time.Time, clock string) time.Time {
	hour, minute, err := parseDailyClock(clock)
	if err != nil {
		hour = 2
		minute = 0
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func StartChannelBaseURLProbeTask() {
	if !common.IsMasterNode {
		return
	}

	channelBaseURLProbeTaskOnce.Do(func() {
		go func() {
			for {
				monitorSetting := operation_setting.GetMonitorSetting()
				if !monitorSetting.ChannelTCPProbeEnabled {
					time.Sleep(time.Minute)
					continue
				}

				now := time.Now()
				nextRun := nextDailyRun(now, monitorSetting.ChannelTCPProbeScheduleTime)
				waitFor := time.Until(nextRun)
				common.SysLog(fmt.Sprintf("channel base_url probe task scheduled at %s", nextRun.Format(time.RFC3339)))
				time.Sleep(waitFor)

				monitorSetting = operation_setting.GetMonitorSetting()
				if !monitorSetting.ChannelTCPProbeEnabled {
					continue
				}

				common.SysLog("channel base_url probe task started")
				if err := service.ProbeAndPersistAllMultiBaseURLChannels(service.DefaultChannelBaseURLProbeTimeout); err != nil {
					common.SysLog(fmt.Sprintf("channel base_url probe task failed: %v", err))
				}
				common.SysLog("channel base_url probe task finished")
			}
		}()
	})
}
