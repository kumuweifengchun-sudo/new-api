package operation_setting

import (
	"os"
	"strconv"

	"github.com/QuantumNous/new-api/setting/config"
)

type MonitorSetting struct {
	AutoTestChannelEnabled      bool    `json:"auto_test_channel_enabled"`
	AutoTestChannelMinutes      float64 `json:"auto_test_channel_minutes"`
	ChannelTCPProbeEnabled      bool    `json:"channel_tcp_probe_enabled"`
	ChannelTCPProbeScheduleTime string  `json:"channel_tcp_probe_schedule_time"`
}

// 默认配置
var monitorSetting = MonitorSetting{
	AutoTestChannelEnabled:      false,
	AutoTestChannelMinutes:      10,
	ChannelTCPProbeEnabled:      true,
	ChannelTCPProbeScheduleTime: "02:00",
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("monitor_setting", &monitorSetting)
}

func GetMonitorSetting() *MonitorSetting {
	if os.Getenv("CHANNEL_TEST_FREQUENCY") != "" {
		frequency, err := strconv.Atoi(os.Getenv("CHANNEL_TEST_FREQUENCY"))
		if err == nil && frequency > 0 {
			monitorSetting.AutoTestChannelEnabled = true
			monitorSetting.AutoTestChannelMinutes = float64(frequency)
		}
	}
	return &monitorSetting
}
