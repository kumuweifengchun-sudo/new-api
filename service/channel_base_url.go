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

func isEmptyNodeBaseURLProbeState(state dto.NodeBaseURLProbeState) bool {
	return strings.TrimSpace(state.PreferredBaseURL) == "" &&
		state.BaseURLProbeLastTime == 0 &&
		len(state.BaseURLProbeResults) == 0 &&
		strings.TrimSpace(state.LastSuccessBaseURL) == "" &&
		state.LastSuccessAt == 0
}

func saveNodeBaseURLProbeState(
	channelID int,
	nodeID string,
	mutate func(state dto.NodeBaseURLProbeState, validURLs []string) dto.NodeBaseURLProbeState,
) error {
	nodeID = strings.TrimSpace(nodeID)
	if channelID <= 0 || nodeID == "" {
		return nil
	}

	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return err
	}
	if channel == nil || !channel.HasMultipleBaseURLs() {
		return nil
	}

	validURLs := channel.GetBaseURLs()
	settings := channel.GetOtherSettings()
	settings.BaseURLProbeByNode = model.FilterBaseURLProbeStatesByNode(settings.BaseURLProbeByNode, validURLs)

	state := settings.BaseURLProbeByNode[nodeID]
	state = model.NormalizeBaseURLProbeState(state, validURLs)
	state = model.NormalizeBaseURLProbeState(mutate(state, validURLs), validURLs)

	if isEmptyNodeBaseURLProbeState(state) {
		delete(settings.BaseURLProbeByNode, nodeID)
	} else {
		if settings.BaseURLProbeByNode == nil {
			settings.BaseURLProbeByNode = make(map[string]dto.NodeBaseURLProbeState)
		}
		settings.BaseURLProbeByNode[nodeID] = state
	}

	channel.SetOtherSettings(settings)
	if err := channel.SaveOtherSettings(); err != nil {
		return err
	}
	model.CacheUpdateChannel(channel)
	return nil
}

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

func persistChannelBaseURLProbe(channelID int, nodeID string, preferred string, results []dto.BaseURLProbeResult, checkedAt int64) error {
	return saveNodeBaseURLProbeState(channelID, nodeID, func(state dto.NodeBaseURLProbeState, validURLs []string) dto.NodeBaseURLProbeState {
		state.BaseURLProbeResults = model.FilterBaseURLProbeResults(results, validURLs)
		state.BaseURLProbeLastTime = checkedAt
		if preferred != "" {
			for _, candidate := range validURLs {
				if candidate == preferred {
					state.PreferredBaseURL = preferred
					break
				}
			}
		}
		if state.PreferredBaseURL == "" && len(validURLs) > 0 {
			state.PreferredBaseURL = validURLs[0]
		}
		return state
	})
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
	return persistChannelBaseURLProbe(channelID, common.NodeID, preferred, results, common.GetTimestamp())
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

	return saveNodeBaseURLProbeState(channelID, common.NodeID, func(state dto.NodeBaseURLProbeState, validURLs []string) dto.NodeBaseURLProbeState {
		state.PreferredBaseURL = preferred
		state.LastSuccessBaseURL = preferred
		state.LastSuccessAt = common.GetTimestamp()
		return state
	})
}

func TriggerChannelBaseURLProbe(channelIDs ...int) {
	for _, channelID := range channelIDs {
		if channelID <= 0 {
			continue
		}
		go func(id int) {
			if err := ProbeAndPersistChannelBaseURLByID(id, DefaultChannelBaseURLProbeTimeout); err != nil {
				common.SysLog(fmt.Sprintf("channel base_url probe failed: node_id=%s channel_id=%d err=%v", common.NodeID, id, err))
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
		if err := persistChannelBaseURLProbe(channel.Id, common.NodeID, preferred, results, common.GetTimestamp()); err != nil {
			common.SysLog(fmt.Sprintf("persist channel base_url probe failed: node_id=%s channel_id=%d err=%v", common.NodeID, channel.Id, err))
		}
	}
	return nil
}
