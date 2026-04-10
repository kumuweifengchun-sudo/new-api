package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupTokenLimitModelTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	DB = db
	LOG_DB = db

	if err := db.AutoMigrate(&Token{}); err != nil {
		t.Fatalf("failed to migrate token table: %v", err)
	}

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func TestRegisterUsedIPRespectsConfiguredLimit(t *testing.T) {
	db := setupTokenLimitModelTestDB(t)
	token := &Token{
		UserId:         1,
		Name:           "ip-limited-token",
		Key:            "test-token-key",
		Status:         common.TokenStatusEnabled,
		CreatedTime:    1,
		AccessedTime:   1,
		ExpiredTime:    -1,
		RemainQuota:    100,
		UnlimitedQuota: true,
		Group:          "default",
	}
	if err := db.Create(token).Error; err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	allowed, err := token.RegisterUsedIP("1.1.1.1", 2)
	if err != nil {
		t.Fatalf("expected first IP registration to succeed, got error: %v", err)
	}
	if !allowed {
		t.Fatalf("expected first IP registration to be allowed")
	}

	allowed, err = token.RegisterUsedIP("1.1.1.1", 2)
	if err != nil {
		t.Fatalf("expected repeated IP registration to succeed, got error: %v", err)
	}
	if !allowed {
		t.Fatalf("expected repeated IP registration to remain allowed")
	}

	allowed, err = token.RegisterUsedIP("2.2.2.2", 2)
	if err != nil {
		t.Fatalf("expected second distinct IP registration to succeed, got error: %v", err)
	}
	if !allowed {
		t.Fatalf("expected second distinct IP registration to be allowed")
	}

	allowed, err = token.RegisterUsedIP("3.3.3.3", 2)
	if err != nil {
		t.Fatalf("expected third distinct IP registration to return a limit result, got error: %v", err)
	}
	if allowed {
		t.Fatalf("expected third distinct IP registration to be rejected")
	}

	reloadedToken, err := GetTokenById(token.Id)
	if err != nil {
		t.Fatalf("failed to reload token: %v", err)
	}
	usedIPs := reloadedToken.GetUsedIps()
	if len(usedIPs) != 2 {
		t.Fatalf("expected exactly two recorded IPs, got %d (%v)", len(usedIPs), usedIPs)
	}
	if usedIPs[0] != "1.1.1.1" || usedIPs[1] != "2.2.2.2" {
		t.Fatalf("unexpected used IP list: %v", usedIPs)
	}
}
