package service

import (
	"context"
	"fmt"
	"net"
	neturl "net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

const DefaultChannelBaseURLProbeTimeout = 3 * time.Second

func resolveProbeAddress(rawURL string) (string, error) {
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return "", err
	}
	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("missing hostname")
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(host, port), nil
}

func ProbeChannelBaseURLs(channel *model.Channel, timeout time.Duration) (string, []dto.BaseURLProbeResult) {
	if channel == nil {
		return "", nil
	}

	urls := channel.GetBaseURLs()
	if len(urls) == 0 {
		return "", nil
	}

	checkedAt := common.GetTimestamp()
	results := make([]dto.BaseURLProbeResult, 0, len(urls))
	var preferred string
	var bestLatency int64

	for _, rawURL := range urls {
		result := dto.BaseURLProbeResult{
			URL:       rawURL,
			CheckedAt: checkedAt,
		}

		address, err := resolveProbeAddress(rawURL)
		if err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}

		start := time.Now()
		dialer := &net.Dialer{Timeout: timeout}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		conn, err := dialer.DialContext(ctx, "tcp", address)
		cancel()
		if err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}

		_ = conn.Close()
		result.Success = true
		result.LatencyMs = time.Since(start).Milliseconds()
		results = append(results, result)

		if preferred == "" || result.LatencyMs < bestLatency {
			preferred = rawURL
			bestLatency = result.LatencyMs
		}
	}

	if preferred == "" && len(urls) > 0 {
		preferred = urls[0]
	}
	return preferred, results
}

func persistChannelBaseURLProbe(channel *model.Channel, preferred string, results []dto.BaseURLProbeResult, checkedAt int64) error {
	if channel == nil {
		return nil
	}

	validURLs := channel.GetBaseURLs()
	settings := channel.GetOtherSettings()
	settings.BaseURLProbeResults = model.FilterBaseURLProbeResults(results, validURLs)
	settings.BaseURLProbeLastTime = checkedAt
	if preferred != "" {
		for _, candidate := range validURLs {
			if candidate == preferred {
				settings.PreferredBaseURL = preferred
				break
			}
		}
	}
	if settings.PreferredBaseURL == "" && len(validURLs) > 0 {
		settings.PreferredBaseURL = validURLs[0]
	}

	channel.SetOtherSettings(settings)
	if err := channel.SaveOtherSettings(); err != nil {
		return err
	}
	model.CacheUpdateChannel(channel)
	return nil
}

func ProbeAndPersistChannelBaseURLByID(channelID int, timeout time.Duration) error {
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return err
	}
	if channel == nil || !channel.HasMultipleBaseURLs() {
		return nil
	}

	preferred, results := ProbeChannelBaseURLs(channel, timeout)
	return persistChannelBaseURLProbe(channel, preferred, results, common.GetTimestamp())
}

func SetPreferredChannelBaseURL(channelID int, preferred string) error {
	preferred = strings.TrimSpace(preferred)
	if preferred == "" {
		return nil
	}

	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return err
	}
	if channel == nil || !channel.HasMultipleBaseURLs() {
		return nil
	}

	valid := false
	for _, candidate := range channel.GetBaseURLs() {
		if candidate == preferred {
			valid = true
			break
		}
	}
	if !valid {
		return nil
	}

	settings := channel.GetOtherSettings()
	settings.PreferredBaseURL = preferred
	channel.SetOtherSettings(settings)
	if err := channel.SaveOtherSettings(); err != nil {
		return err
	}
	model.CacheUpdateChannel(channel)
	return nil
}

func TriggerChannelBaseURLProbe(channelIDs ...int) {
	for _, channelID := range channelIDs {
		if channelID <= 0 {
			continue
		}
		go func(id int) {
			if err := ProbeAndPersistChannelBaseURLByID(id, DefaultChannelBaseURLProbeTimeout); err != nil {
				common.SysLog(fmt.Sprintf("channel base_url probe failed: channel_id=%d err=%v", id, err))
			}
		}(channelID)
	}
}

func ProbeAndPersistAllMultiBaseURLChannels(timeout time.Duration) error {
	var channels []*model.Channel
	if err := model.DB.Find(&channels).Error; err != nil {
		return err
	}
	for _, channel := range channels {
		if channel == nil || !channel.HasMultipleBaseURLs() {
			continue
		}
		preferred, results := ProbeChannelBaseURLs(channel, timeout)
		if err := persistChannelBaseURLProbe(channel, preferred, results, common.GetTimestamp()); err != nil {
			common.SysLog(fmt.Sprintf("persist channel base_url probe failed: channel_id=%d err=%v", channel.Id, err))
		}
	}
	return nil
}
