package model

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
)

func ParseBaseURLs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	result := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		value := strings.TrimSpace(line)
		if value == "" {
			continue
		}
		value = strings.TrimRight(value, "/")
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func NormalizeBaseURLs(raw string) (string, []string, error) {
	urls := ParseBaseURLs(raw)
	for _, value := range urls {
		if err := validateBaseURLValue(value); err != nil {
			return "", nil, err
		}
	}
	return strings.Join(urls, "\n"), urls, nil
}

func ResolveBaseURLFromRaw(raw string, channelType int) string {
	urls := ParseBaseURLs(raw)
	if len(urls) > 0 {
		return urls[0]
	}
	return constant.ChannelBaseURLs[channelType]
}

func validateBaseURLValue(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("API地址格式错误: %s", value)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("API地址必须以 http:// 或 https:// 开头: %s", value)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("API地址缺少主机名: %s", value)
	}
	return nil
}

func (channel *Channel) GetBaseURLs() []string {
	if channel == nil {
		return nil
	}
	if channel.BaseURL != nil {
		urls := ParseBaseURLs(*channel.BaseURL)
		if len(urls) > 0 {
			return urls
		}
	}
	baseURL := constant.ChannelBaseURLs[channel.Type]
	if baseURL == "" {
		return nil
	}
	return []string{baseURL}
}

func (channel *Channel) HasMultipleBaseURLs() bool {
	return len(channel.GetBaseURLs()) > 1
}

func isValidBaseURLCandidate(candidate string, validURLs []string) bool {
	for _, value := range validURLs {
		if value == candidate {
			return true
		}
	}
	return false
}

func NormalizeBaseURLProbeState(state dto.NodeBaseURLProbeState, validURLs []string) dto.NodeBaseURLProbeState {
	if len(validURLs) == 0 {
		return dto.NodeBaseURLProbeState{}
	}

	state.BaseURLProbeResults = FilterBaseURLProbeResults(state.BaseURLProbeResults, validURLs)
	if !isValidBaseURLCandidate(strings.TrimSpace(state.PreferredBaseURL), validURLs) {
		state.PreferredBaseURL = ""
	}
	if !isValidBaseURLCandidate(strings.TrimSpace(state.LastSuccessBaseURL), validURLs) {
		state.LastSuccessBaseURL = ""
		state.LastSuccessAt = 0
	}
	return state
}

func FilterBaseURLProbeStatesByNode(states map[string]dto.NodeBaseURLProbeState, validURLs []string) map[string]dto.NodeBaseURLProbeState {
	if len(states) == 0 || len(validURLs) == 0 {
		return nil
	}

	filtered := make(map[string]dto.NodeBaseURLProbeState, len(states))
	for nodeID, state := range states {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID == "" {
			continue
		}
		normalized := NormalizeBaseURLProbeState(state, validURLs)
		if normalized.PreferredBaseURL == "" &&
			normalized.BaseURLProbeLastTime == 0 &&
			len(normalized.BaseURLProbeResults) == 0 &&
			normalized.LastSuccessBaseURL == "" &&
			normalized.LastSuccessAt == 0 {
			continue
		}
		filtered[nodeID] = normalized
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func (channel *Channel) GetLegacyBaseURLProbeState() dto.NodeBaseURLProbeState {
	if channel == nil {
		return dto.NodeBaseURLProbeState{}
	}
	settings := channel.GetOtherSettings()
	return NormalizeBaseURLProbeState(dto.NodeBaseURLProbeState{
		PreferredBaseURL:     settings.PreferredBaseURL,
		BaseURLProbeLastTime: settings.BaseURLProbeLastTime,
		BaseURLProbeResults:  settings.BaseURLProbeResults,
	}, channel.GetBaseURLs())
}

func (channel *Channel) GetBaseURLProbeStateByNode(nodeID string) dto.NodeBaseURLProbeState {
	if channel == nil {
		return dto.NodeBaseURLProbeState{}
	}
	settings := channel.GetOtherSettings()
	if len(settings.BaseURLProbeByNode) == 0 {
		return dto.NodeBaseURLProbeState{}
	}
	return NormalizeBaseURLProbeState(settings.BaseURLProbeByNode[strings.TrimSpace(nodeID)], channel.GetBaseURLs())
}

func (channel *Channel) GetCurrentNodeBaseURLProbeState() dto.NodeBaseURLProbeState {
	return channel.GetBaseURLProbeStateByNode(common.NodeID)
}

func (channel *Channel) GetPreferredBaseURL() string {
	urls := channel.GetBaseURLs()
	if len(urls) == 0 {
		return ""
	}
	if len(urls) == 1 {
		return urls[0]
	}

	state := channel.GetCurrentNodeBaseURLProbeState()
	preferred := strings.TrimSpace(state.PreferredBaseURL)
	if preferred != "" {
		if isValidBaseURLCandidate(preferred, urls) {
			return preferred
		}
	}

	bestURL := ""
	var bestLatency int64
	for _, result := range state.BaseURLProbeResults {
		if !result.Success {
			continue
		}
		if !isValidBaseURLCandidate(result.URL, urls) {
			continue
		}
		if bestURL == "" || result.LatencyMs < bestLatency {
			bestURL = result.URL
			bestLatency = result.LatencyMs
		}
	}
	if bestURL != "" {
		return bestURL
	}
	return urls[0]
}

func (channel *Channel) GetBaseURLCandidates() []string {
	urls := channel.GetBaseURLs()
	if len(urls) <= 1 {
		return urls
	}

	preferred := channel.GetPreferredBaseURL()
	if preferred == "" {
		return urls
	}

	result := make([]string, 0, len(urls))
	result = append(result, preferred)
	for _, candidate := range urls {
		if candidate == preferred {
			continue
		}
		result = append(result, candidate)
	}
	return result
}

func FilterBaseURLProbeResults(results []dto.BaseURLProbeResult, validURLs []string) []dto.BaseURLProbeResult {
	if len(results) == 0 || len(validURLs) == 0 {
		return nil
	}
	validSet := make(map[string]struct{}, len(validURLs))
	for _, value := range validURLs {
		validSet[value] = struct{}{}
	}
	filtered := make([]dto.BaseURLProbeResult, 0, len(results))
	for _, result := range results {
		if _, ok := validSet[result.URL]; !ok {
			continue
		}
		filtered = append(filtered, result)
	}
	return filtered
}
