package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func TestGetPreferredBaseURLUsesCurrentNodeState(t *testing.T) {
	originalNodeID := common.NodeID
	common.NodeID = "node-a"
	defer func() {
		common.NodeID = originalNodeID
	}()

	settingsJSON, err := common.Marshal(dto.ChannelOtherSettings{
		BaseURLProbeByNode: map[string]dto.NodeBaseURLProbeState{
			"node-a": {
				BaseURLProbeResults: []dto.BaseURLProbeResult{
					{URL: "https://slow.example.com", Success: true, LatencyMs: 25},
					{URL: "https://fast.example.com", Success: true, LatencyMs: 8},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	channel := &Channel{
		BaseURL:       common.GetPointer("https://slow.example.com\nhttps://fast.example.com"),
		OtherSettings: string(settingsJSON),
	}

	if got := channel.GetPreferredBaseURL(); got != "https://fast.example.com" {
		t.Fatalf("expected current node to prefer fast url, got %q", got)
	}
}

func TestGetPreferredBaseURLFallsBackToConfiguredOrderWithoutNodeState(t *testing.T) {
	originalNodeID := common.NodeID
	common.NodeID = "node-a"
	defer func() {
		common.NodeID = originalNodeID
	}()

	settingsJSON, err := common.Marshal(dto.ChannelOtherSettings{
		PreferredBaseURL:     "https://fast.example.com",
		BaseURLProbeLastTime: 123,
		BaseURLProbeResults: []dto.BaseURLProbeResult{
			{URL: "https://fast.example.com", Success: true, LatencyMs: 5},
		},
	})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	channel := &Channel{
		BaseURL:       common.GetPointer("https://first.example.com\nhttps://fast.example.com"),
		OtherSettings: string(settingsJSON),
	}

	if got := channel.GetPreferredBaseURL(); got != "https://first.example.com" {
		t.Fatalf("expected configured order fallback for node without state, got %q", got)
	}
}

func TestFilterBaseURLProbeStatesByNodeKeepsOnlyValidState(t *testing.T) {
	states := map[string]dto.NodeBaseURLProbeState{
		"node-a": {
			PreferredBaseURL: "https://invalid.example.com",
			BaseURLProbeResults: []dto.BaseURLProbeResult{
				{URL: "https://invalid.example.com", Success: true, LatencyMs: 3},
			},
			LastSuccessBaseURL: "https://invalid.example.com",
			LastSuccessAt:      100,
		},
		"node-b": {
			PreferredBaseURL: "https://valid.example.com",
			BaseURLProbeResults: []dto.BaseURLProbeResult{
				{URL: "https://valid.example.com", Success: true, LatencyMs: 7},
				{URL: "https://invalid.example.com", Success: false},
			},
			LastSuccessBaseURL: "https://valid.example.com",
			LastSuccessAt:      200,
		},
	}

	filtered := FilterBaseURLProbeStatesByNode(states, []string{
		"https://valid.example.com",
	})

	if len(filtered) != 1 {
		t.Fatalf("expected exactly one valid node state, got %d", len(filtered))
	}
	if _, ok := filtered["node-a"]; ok {
		t.Fatalf("expected invalid node state to be removed")
	}
	state, ok := filtered["node-b"]
	if !ok {
		t.Fatalf("expected node-b to be preserved")
	}
	if state.PreferredBaseURL != "https://valid.example.com" {
		t.Fatalf("expected preferred base url to remain valid, got %q", state.PreferredBaseURL)
	}
	if len(state.BaseURLProbeResults) != 1 || state.BaseURLProbeResults[0].URL != "https://valid.example.com" {
		t.Fatalf("expected probe results to keep only valid urls, got %+v", state.BaseURLProbeResults)
	}
}
