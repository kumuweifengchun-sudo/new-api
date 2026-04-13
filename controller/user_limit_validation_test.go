package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

func testIntPtr(v int) *int {
	return &v
}

func TestValidateUserTokenLimitsAllowsNilOverrides(t *testing.T) {
	user := &model.User{}
	if err := validateUserTokenLimits(user); err != nil {
		t.Fatalf("expected nil overrides to pass validation, got %v", err)
	}
}

func TestValidateUserTokenLimitsRejectsIncompleteRateLimitOverride(t *testing.T) {
	user := &model.User{
		ModelRequestRateLimitCountOverride: testIntPtr(10),
	}
	err := validateUserTokenLimits(user)
	if err == nil {
		t.Fatal("expected incomplete rate limit override to fail validation")
	}
}

func TestValidateUserTokenLimitsRejectsNegativeTotalRateLimitOverride(t *testing.T) {
	user := &model.User{
		ModelRequestRateLimitCountOverride:        testIntPtr(-1),
		ModelRequestRateLimitSuccessCountOverride: testIntPtr(5),
	}
	err := validateUserTokenLimits(user)
	if err == nil {
		t.Fatal("expected negative total rate limit override to fail validation")
	}
}

func TestValidateUserTokenLimitsRejectsNonPositiveSuccessRateLimitOverride(t *testing.T) {
	user := &model.User{
		ModelRequestRateLimitCountOverride:        testIntPtr(0),
		ModelRequestRateLimitSuccessCountOverride: testIntPtr(0),
	}
	err := validateUserTokenLimits(user)
	if err == nil {
		t.Fatal("expected non-positive success rate limit override to fail validation")
	}
}

func TestValidateUserTokenLimitsAllowsValidRateLimitOverride(t *testing.T) {
	user := &model.User{
		ModelRequestRateLimitCountOverride:        testIntPtr(0),
		ModelRequestRateLimitSuccessCountOverride: testIntPtr(5),
	}
	if err := validateUserTokenLimits(user); err != nil {
		t.Fatalf("expected valid rate limit override to pass validation, got %v", err)
	}
}
