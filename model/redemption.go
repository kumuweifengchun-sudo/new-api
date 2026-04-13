package model

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"gorm.io/gorm"
)

const (
	RedemptionRewardTypeQuota        = "quota"
	RedemptionRewardTypeSubscription = "subscription"
)

type Redemption struct {
	Id                 int            `json:"id"`
	UserId             int            `json:"user_id"`
	Key                string         `json:"key" gorm:"type:char(32);uniqueIndex"`
	Status             int            `json:"status" gorm:"default:1"`
	Name               string         `json:"name" gorm:"index"`
	RewardType         string         `json:"reward_type" gorm:"type:varchar(32);not null;default:'quota'"`
	Quota              int            `json:"quota" gorm:"default:100"`
	SubscriptionPlanId int            `json:"subscription_plan_id" gorm:"type:int;default:0;index"`
	CreatedTime        int64          `json:"created_time" gorm:"bigint"`
	RedeemedTime       int64          `json:"redeemed_time" gorm:"bigint"`
	Count              int            `json:"count" gorm:"-:all"` // only for api request
	UsedUserId         int            `json:"used_user_id"`
	DeletedAt          gorm.DeletedAt `gorm:"index"`
	ExpiredTime        int64          `json:"expired_time" gorm:"bigint"` // 过期时间，0 表示不过期
}

type RedeemedSubscriptionInfo struct {
	PlanId         int    `json:"plan_id"`
	PlanTitle      string `json:"plan_title"`
	SubscriptionId int    `json:"subscription_id"`
	StartTime      int64  `json:"start_time"`
	EndTime        int64  `json:"end_time"`
	Source         string `json:"source"`
}

type RedeemResult struct {
	RewardType   string                    `json:"reward_type"`
	Quota        int                       `json:"quota,omitempty"`
	Subscription *RedeemedSubscriptionInfo `json:"subscription,omitempty"`
}

func NormalizeRedemptionRewardType(rewardType string) string {
	switch strings.TrimSpace(rewardType) {
	case "", RedemptionRewardTypeQuota:
		return RedemptionRewardTypeQuota
	case RedemptionRewardTypeSubscription:
		return RedemptionRewardTypeSubscription
	default:
		return ""
	}
}

func GetAllRedemptions(startIdx int, num int) (redemptions []*Redemption, total int64, err error) {
	// 开始事务
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 获取总数
	err = tx.Model(&Redemption{}).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 获取分页数据
	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 提交事务
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	for _, redemption := range redemptions {
		if redemption != nil {
			redemption.RewardType = NormalizeRedemptionRewardType(redemption.RewardType)
		}
	}

	return redemptions, total, nil
}

func SearchRedemptions(keyword string, startIdx int, num int) (redemptions []*Redemption, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Build query based on keyword type
	query := tx.Model(&Redemption{})

	// Only try to convert to ID if the string represents a valid integer
	if id, err := strconv.Atoi(keyword); err == nil {
		query = query.Where("id = ? OR name LIKE ?", id, keyword+"%")
	} else {
		query = query.Where("name LIKE ?", keyword+"%")
	}

	// Get total count
	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated data
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	for _, redemption := range redemptions {
		if redemption != nil {
			redemption.RewardType = NormalizeRedemptionRewardType(redemption.RewardType)
		}
	}

	return redemptions, total, nil
}

func GetRedemptionById(id int) (*Redemption, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	redemption := Redemption{Id: id}
	var err error = nil
	err = DB.First(&redemption, "id = ?", id).Error
	redemption.RewardType = NormalizeRedemptionRewardType(redemption.RewardType)
	return &redemption, err
}

func Redeem(key string, userId int) (result *RedeemResult, err error) {
	if key == "" {
		return nil, errors.New("未提供兑换码")
	}
	if userId == 0 {
		return nil, errors.New("无效的 user id")
	}
	redemption := &Redemption{}
	redeemResult := &RedeemResult{}
	var redeemedPlan *SubscriptionPlan

	keyCol := "`key`"
	if common.UsingPostgreSQL {
		keyCol = `"key"`
	}
	common.RandomSleep()
	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(keyCol+" = ?", key).First(redemption).Error
		if err != nil {
			return errors.New("无效的兑换码")
		}
		if redemption.Status != common.RedemptionCodeStatusEnabled {
			return errors.New("该兑换码已被使用")
		}
		if redemption.ExpiredTime != 0 && redemption.ExpiredTime < common.GetTimestamp() {
			return errors.New("该兑换码已过期")
		}
		rewardType := NormalizeRedemptionRewardType(redemption.RewardType)
		if rewardType == "" {
			return errors.New("无效的兑换奖励类型")
		}
		redemption.RewardType = rewardType
		redeemResult.RewardType = rewardType
		switch rewardType {
		case RedemptionRewardTypeQuota:
			if redemption.Quota <= 0 {
				return errors.New("该兑换码未配置额度")
			}
			err = tx.Model(&User{}).Where("id = ?", userId).Update("quota", gorm.Expr("quota + ?", redemption.Quota)).Error
			if err != nil {
				return ErrRedeemFailed
			}
			redeemResult.Quota = redemption.Quota
		case RedemptionRewardTypeSubscription:
			if redemption.SubscriptionPlanId <= 0 {
				return errors.New("该兑换码未配置订阅套餐")
			}
			plan, err := getSubscriptionPlanByIdTx(tx, redemption.SubscriptionPlanId)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errors.New("关联的订阅套餐不存在")
				}
				return err
			}
			sub, err := CreateUserSubscriptionFromPlanTx(tx, userId, plan, "redemption")
			if err != nil {
				return err
			}
			redeemedPlan = plan
			redeemResult.Subscription = &RedeemedSubscriptionInfo{
				PlanId:         plan.Id,
				PlanTitle:      plan.Title,
				SubscriptionId: sub.Id,
				StartTime:      sub.StartTime,
				EndTime:        sub.EndTime,
				Source:         sub.Source,
			}
		}
		redemption.RedeemedTime = common.GetTimestamp()
		redemption.Status = common.RedemptionCodeStatusUsed
		redemption.UsedUserId = userId
		err = tx.Save(redemption).Error
		if err != nil {
			return ErrRedeemFailed
		}
		return err
	})
	if err != nil {
		common.SysError("redemption failed: " + err.Error())
		return nil, err
	}
	if redeemedPlan != nil && strings.TrimSpace(redeemedPlan.UpgradeGroup) != "" {
		_ = UpdateUserGroupCache(userId, redeemedPlan.UpgradeGroup)
	}
	switch redeemResult.RewardType {
	case RedemptionRewardTypeSubscription:
		if redeemResult.Subscription != nil {
			RecordLog(userId, LogTypeTopup, fmt.Sprintf("通过兑换码领取订阅套餐 %s，兑换码ID %d，订阅ID %d", redeemResult.Subscription.PlanTitle, redemption.Id, redeemResult.Subscription.SubscriptionId))
		}
	case RedemptionRewardTypeQuota:
		RecordLog(userId, LogTypeTopup, fmt.Sprintf("通过兑换码充值 %s，兑换码ID %d", logger.LogQuota(redemption.Quota), redemption.Id))
	}
	return redeemResult, nil
}

func (redemption *Redemption) Insert() error {
	var err error
	err = DB.Create(redemption).Error
	return err
}

func (redemption *Redemption) SelectUpdate() error {
	// This can update zero values
	return DB.Model(redemption).Select("redeemed_time", "status").Updates(redemption).Error
}

// Update Make sure your token's fields is completed, because this will update non-zero values
func (redemption *Redemption) Update() error {
	var err error
	err = DB.Model(redemption).Select("name", "status", "reward_type", "quota", "subscription_plan_id", "redeemed_time", "expired_time").Updates(redemption).Error
	return err
}

func (redemption *Redemption) Delete() error {
	var err error
	err = DB.Delete(redemption).Error
	return err
}

func DeleteRedemptionById(id int) (err error) {
	if id == 0 {
		return errors.New("id 为空！")
	}
	redemption := Redemption{Id: id}
	err = DB.Where(redemption).First(&redemption).Error
	if err != nil {
		return err
	}
	return redemption.Delete()
}

func DeleteInvalidRedemptions() (int64, error) {
	now := common.GetTimestamp()
	result := DB.Where("status IN ? OR (status = ? AND expired_time != 0 AND expired_time < ?)", []int{common.RedemptionCodeStatusUsed, common.RedemptionCodeStatusDisabled}, common.RedemptionCodeStatusEnabled, now).Delete(&Redemption{})
	return result.RowsAffected, result.Error
}
