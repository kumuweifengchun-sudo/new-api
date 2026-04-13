package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

func intPtr(v int) *int {
	return &v
}

func TestResolveEffectiveModelRequestRateLimitUsesGlobalDefaults(t *testing.T) {
	originalCount := setting.ModelRequestRateLimitCount
	originalSuccessCount := setting.ModelRequestRateLimitSuccessCount
	originalGroup := setting.ModelRequestRateLimitGroup
	t.Cleanup(func() {
		setting.ModelRequestRateLimitCount = originalCount
		setting.ModelRequestRateLimitSuccessCount = originalSuccessCount
		setting.ModelRequestRateLimitGroup = originalGroup
	})

	setting.ModelRequestRateLimitCount = 180
	setting.ModelRequestRateLimitSuccessCount = 120
	setting.ModelRequestRateLimitGroup = map[string][2]int{}

	total, success := resolveEffectiveModelRequestRateLimit("default", nil)
	if total != 180 || success != 120 {
		t.Fatalf("expected global limits 180/120, got %d/%d", total, success)
	}
}

func TestResolveEffectiveModelRequestRateLimitUsesGroupBeforeGlobal(t *testing.T) {
	originalCount := setting.ModelRequestRateLimitCount
	originalSuccessCount := setting.ModelRequestRateLimitSuccessCount
	originalGroup := setting.ModelRequestRateLimitGroup
	t.Cleanup(func() {
		setting.ModelRequestRateLimitCount = originalCount
		setting.ModelRequestRateLimitSuccessCount = originalSuccessCount
		setting.ModelRequestRateLimitGroup = originalGroup
	})

	setting.ModelRequestRateLimitCount = 180
	setting.ModelRequestRateLimitSuccessCount = 120
	setting.ModelRequestRateLimitGroup = map[string][2]int{
		"vip": {20, 10},
	}

	total, success := resolveEffectiveModelRequestRateLimit("vip", nil)
	if total != 20 || success != 10 {
		t.Fatalf("expected group limits 20/10, got %d/%d", total, success)
	}
}

func TestResolveEffectiveModelRequestRateLimitUsesUserOverride(t *testing.T) {
	originalCount := setting.ModelRequestRateLimitCount
	originalSuccessCount := setting.ModelRequestRateLimitSuccessCount
	originalGroup := setting.ModelRequestRateLimitGroup
	t.Cleanup(func() {
		setting.ModelRequestRateLimitCount = originalCount
		setting.ModelRequestRateLimitSuccessCount = originalSuccessCount
		setting.ModelRequestRateLimitGroup = originalGroup
	})

	setting.ModelRequestRateLimitCount = 180
	setting.ModelRequestRateLimitSuccessCount = 120
	setting.ModelRequestRateLimitGroup = map[string][2]int{
		"vip": {20, 10},
	}
	user := &model.UserBase{
		Id:                                 1,
		ModelRequestRateLimitCountOverride: intPtr(8),
		ModelRequestRateLimitSuccessCountOverride: intPtr(4),
	}

	total, success := resolveEffectiveModelRequestRateLimit("vip", user)
	if total != 8 || success != 4 {
		t.Fatalf("expected user override limits 8/4, got %d/%d", total, success)
	}
}

func TestResolveEffectiveModelRequestRateLimitIgnoresIncompleteUserOverride(t *testing.T) {
	originalCount := setting.ModelRequestRateLimitCount
	originalSuccessCount := setting.ModelRequestRateLimitSuccessCount
	originalGroup := setting.ModelRequestRateLimitGroup
	t.Cleanup(func() {
		setting.ModelRequestRateLimitCount = originalCount
		setting.ModelRequestRateLimitSuccessCount = originalSuccessCount
		setting.ModelRequestRateLimitGroup = originalGroup
	})

	setting.ModelRequestRateLimitCount = 180
	setting.ModelRequestRateLimitSuccessCount = 120
	setting.ModelRequestRateLimitGroup = map[string][2]int{
		"vip": {20, 10},
	}
	user := &model.UserBase{
		Id:                                 1,
		ModelRequestRateLimitCountOverride: intPtr(8),
	}

	total, success := resolveEffectiveModelRequestRateLimit("vip", user)
	if total != 20 || success != 10 {
		t.Fatalf("expected fallback group limits 20/10, got %d/%d", total, success)
	}
}
