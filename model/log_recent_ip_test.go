package model

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupLogModelTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.LogConsumeEnabled = true

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	DB = db
	LOG_DB = db

	if err := db.AutoMigrate(&User{}, &Token{}, &Log{}); err != nil {
		t.Fatalf("failed to migrate tables: %v", err)
	}

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func TestGetRecentUserIPsAggregatesLogs(t *testing.T) {
	db := setupLogModelTestDB(t)

	user := &User{
		Id:          1,
		Username:    "recent-ip-user",
		Password:    "hashed-password",
		DisplayName: "Recent IP User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	logs := []Log{
		{UserId: 1, Type: LogTypeConsume, TokenId: 11, TokenName: "alpha", Ip: "1.1.1.1", CreatedAt: 100},
		{UserId: 1, Type: LogTypeError, TokenId: 11, TokenName: "alpha", Ip: "1.1.1.1", CreatedAt: 110},
		{UserId: 1, Type: LogTypeConsume, TokenId: 12, TokenName: "beta", Ip: "1.1.1.1", CreatedAt: 120},
		{UserId: 1, Type: LogTypeConsume, TokenId: 13, TokenName: "gamma", Ip: "2.2.2.2", CreatedAt: 115},
		{UserId: 1, Type: LogTypeConsume, TokenId: 14, TokenName: "ignored-empty-ip", Ip: "", CreatedAt: 130},
		{UserId: 2, Type: LogTypeConsume, TokenId: 21, TokenName: "other-user", Ip: "9.9.9.9", CreatedAt: 140},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("failed to create logs: %v", err)
	}

	items, total, err := GetRecentUserIPs(1, 90, 125, 0, 0, 10)
	if err != nil {
		t.Fatalf("expected recent ip query to succeed, got error: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 distinct IPs, got %d", total)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	first := items[0]
	if first.IP != "1.1.1.1" {
		t.Fatalf("expected most recent ip to be 1.1.1.1, got %s", first.IP)
	}
	if first.LastUsedAt != 120 {
		t.Fatalf("expected last_used_at 120, got %d", first.LastUsedAt)
	}
	if first.RequestCount != 3 {
		t.Fatalf("expected request count 3, got %d", first.RequestCount)
	}
	if first.TokenCount != 2 {
		t.Fatalf("expected token count 2, got %d", first.TokenCount)
	}
	if len(first.Tokens) != 2 {
		t.Fatalf("expected 2 related tokens, got %d", len(first.Tokens))
	}
	if first.Tokens[0].Id != 12 || first.Tokens[1].Id != 11 {
		t.Fatalf("expected tokens ordered by latest usage, got %+v", first.Tokens)
	}

	second := items[1]
	if second.IP != "2.2.2.2" {
		t.Fatalf("expected second ip to be 2.2.2.2, got %s", second.IP)
	}
	if second.RequestCount != 1 || second.TokenCount != 1 {
		t.Fatalf("unexpected second item aggregation: %+v", second)
	}
}

func TestRecordConsumeAndErrorLogAlwaysPersistIP(t *testing.T) {
	db := setupLogModelTestDB(t)

	user := &User{
		Id:          3,
		Username:    "ip-log-user",
		Password:    "hashed-password",
		DisplayName: "IP Log User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Request.RemoteAddr = "3.3.3.3:12345"
	ctx.Set("username", user.Username)

	RecordConsumeLog(ctx, user.Id, RecordConsumeLogParams{
		ChannelId:        1,
		ModelName:        "gpt-test",
		TokenName:        "consume-token",
		TokenId:          31,
		Quota:            10,
		PromptTokens:     1,
		CompletionTokens: 2,
		Content:          "consume",
	})
	RecordErrorLog(ctx, user.Id, 1, "gpt-test", "error-token", "boom", 32, 1, false, "default", nil)

	var logs []Log
	if err := db.Order("id asc").Find(&logs).Error; err != nil {
		t.Fatalf("failed to load logs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}
	for _, entry := range logs {
		if entry.Ip != "3.3.3.3" {
			t.Fatalf("expected log ip 3.3.3.3, got %q for type %d", entry.Ip, entry.Type)
		}
	}
}
