package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

var redemptionTestMigrateOnce sync.Once

func ensureRedemptionTestTables(t *testing.T) {
	t.Helper()
	redemptionTestMigrateOnce.Do(func() {
		require.NoError(t, DB.AutoMigrate(&Redemption{}, &SubscriptionPlan{}, &UserSubscription{}))
	})
}

func truncateRedemptionTables(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM redemptions")
		DB.Exec("DELETE FROM user_subscriptions")
		DB.Exec("DELETE FROM subscription_plans")
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
	})
}

func createTestUser(t *testing.T, username string) *User {
	t.Helper()
	user := &User{
		Username: username,
		Password: "password123",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func createTestPlan(t *testing.T, title string, maxPurchasePerUser int) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Title:              title,
		Currency:           "USD",
		DurationUnit:       SubscriptionDurationDay,
		DurationValue:      30,
		Enabled:            true,
		MaxPurchasePerUser: maxPurchasePerUser,
		TotalAmount:        1000,
	}
	require.NoError(t, DB.Create(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	return plan
}

func TestRedeemQuotaCode(t *testing.T) {
	ensureRedemptionTestTables(t)
	truncateRedemptionTables(t)

	user := createTestUser(t, "redeem_quota_user")
	code := &Redemption{
		UserId:      user.Id,
		Key:         "quota-redeem-code",
		Name:        "quota",
		RewardType:  RedemptionRewardTypeQuota,
		Quota:       12345,
		Status:      common.RedemptionCodeStatusEnabled,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(code).Error)

	result, err := Redeem(code.Key, user.Id)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, RedemptionRewardTypeQuota, result.RewardType)
	require.Equal(t, 12345, result.Quota)
	require.Nil(t, result.Subscription)

	var updatedUser User
	require.NoError(t, DB.First(&updatedUser, user.Id).Error)
	require.Equal(t, 12345, updatedUser.Quota)

	var updatedCode Redemption
	require.NoError(t, DB.First(&updatedCode, code.Id).Error)
	require.Equal(t, common.RedemptionCodeStatusUsed, updatedCode.Status)
	require.Equal(t, user.Id, updatedCode.UsedUserId)
}

func TestRedeemSubscriptionCode(t *testing.T) {
	ensureRedemptionTestTables(t)
	truncateRedemptionTables(t)

	user := createTestUser(t, "redeem_subscription_user")
	plan := createTestPlan(t, "Starter Plan", 0)
	code := &Redemption{
		UserId:             user.Id,
		Key:                "subscription-redeem-code",
		Name:               "subscription",
		RewardType:         RedemptionRewardTypeSubscription,
		SubscriptionPlanId: plan.Id,
		Status:             common.RedemptionCodeStatusEnabled,
		CreatedTime:        common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(code).Error)

	result, err := Redeem(code.Key, user.Id)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, RedemptionRewardTypeSubscription, result.RewardType)
	require.NotNil(t, result.Subscription)
	require.Equal(t, plan.Id, result.Subscription.PlanId)
	require.Equal(t, plan.Title, result.Subscription.PlanTitle)
	require.NotZero(t, result.Subscription.SubscriptionId)

	var subscriptions []UserSubscription
	require.NoError(t, DB.Where("user_id = ?", user.Id).Find(&subscriptions).Error)
	require.Len(t, subscriptions, 1)
	require.Equal(t, "redemption", subscriptions[0].Source)
	require.Equal(t, plan.Id, subscriptions[0].PlanId)

	var updatedCode Redemption
	require.NoError(t, DB.First(&updatedCode, code.Id).Error)
	require.Equal(t, common.RedemptionCodeStatusUsed, updatedCode.Status)
}

func TestRedeemSubscriptionCodeRespectsPlanLimit(t *testing.T) {
	ensureRedemptionTestTables(t)
	truncateRedemptionTables(t)

	user := createTestUser(t, "redeem_subscription_limited_user")
	plan := createTestPlan(t, "Limited Plan", 1)
	_, err := AdminBindSubscription(user.Id, plan.Id, "")
	require.NoError(t, err)

	code := &Redemption{
		UserId:             user.Id,
		Key:                "subscription-limit-code",
		Name:               "subscription-limit",
		RewardType:         RedemptionRewardTypeSubscription,
		SubscriptionPlanId: plan.Id,
		Status:             common.RedemptionCodeStatusEnabled,
		CreatedTime:        common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(code).Error)

	result, err := Redeem(code.Key, user.Id)
	require.Error(t, err)
	require.Nil(t, result)
	require.EqualError(t, err, "已达到该套餐购买上限")

	var updatedCode Redemption
	require.NoError(t, DB.First(&updatedCode, code.Id).Error)
	require.Equal(t, common.RedemptionCodeStatusEnabled, updatedCode.Status)
	require.Zero(t, updatedCode.UsedUserId)
}
