package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type logAPIResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type recentIPPageResponse struct {
	Items []model.RecentUserIPItem `json:"items"`
	Total int                      `json:"total"`
	Page  int                      `json:"page"`
}

func setupLogControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	model.DB = db
	model.LOG_DB = db

	if err := db.AutoMigrate(&model.User{}, &model.Log{}); err != nil {
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

func newAdminContext(t *testing.T, target string, role int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	ctx.Set("role", role)
	return ctx, recorder
}

func decodeLogAPIResponse(t *testing.T, recorder *httptest.ResponseRecorder) logAPIResponse {
	t.Helper()

	var response logAPIResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode api response: %v", err)
	}
	return response
}

func TestGetUserRecentIPsReturnsAggregatedPage(t *testing.T) {
	db := setupLogControllerTestDB(t)

	user := &model.User{
		Id:          1,
		Username:    "log-target",
		Password:    "hashed-password",
		DisplayName: "Log Target",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	logs := []model.Log{
		{UserId: 1, Type: model.LogTypeConsume, TokenId: 7, TokenName: "first-token", Ip: "4.4.4.4", CreatedAt: 100},
		{UserId: 1, Type: model.LogTypeError, TokenId: 8, TokenName: "second-token", Ip: "4.4.4.4", CreatedAt: 120},
		{UserId: 1, Type: model.LogTypeConsume, TokenId: 9, TokenName: "third-token", Ip: "5.5.5.5", CreatedAt: 110},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("failed to create logs: %v", err)
	}

	ctx, recorder := newAdminContext(t, "/api/log/users/1/recent-ips?p=1&page_size=10&start_timestamp=90&end_timestamp=130", common.RoleRootUser)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(user.Id)}}

	GetUserRecentIPs(ctx)

	response := decodeLogAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var page recentIPPageResponse
	if err := common.Unmarshal(response.Data, &page); err != nil {
		t.Fatalf("failed to decode recent ip page: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("expected total 2, got %d", page.Total)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(page.Items))
	}
	if page.Items[0].IP != "4.4.4.4" {
		t.Fatalf("expected first aggregated ip to be 4.4.4.4, got %s", page.Items[0].IP)
	}
	if page.Items[0].RequestCount != 2 || page.Items[0].TokenCount != 2 {
		t.Fatalf("unexpected aggregated counts: %+v", page.Items[0])
	}
}
