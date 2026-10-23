package controller

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupManageUserTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	dialect := os.Getenv("TEST_MANAGE_USER_DIALECT")
	if dialect == "" {
		dialect = "sqlite"
	}
	databaseTypes := map[string]common.DatabaseType{
		"sqlite": common.DatabaseTypeSQLite, "mysql": common.DatabaseTypeMySQL, "postgres": common.DatabaseTypePostgreSQL,
	}
	require.Contains(t, databaseTypes, dialect)
	dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
	db, _ := newAuditTestDatabase(t, dialect, dsn)
	logDB := db
	if os.Getenv("TEST_MANAGE_USER_SEPARATE_LOG_DB") == "1" {
		logDB, _ = newAuditTestDatabase(t, dialect, dsn)
	}
	model.DB, model.LOG_DB = db, logDB
	common.RedisEnabled = false
	common.SetDatabaseTypes(databaseTypes[dialect], databaseTypes[dialect])

	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		if logDB != db {
			sqlLogDB, err := logDB.DB()
			if err == nil {
				_ = sqlLogDB.Close()
			}
		}
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserGroupAssignment{}, &model.UserSession{}, &model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, model.InitializeUserGroupAssignments())
	require.NoError(t, logDB.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	versionQuery := "SELECT version()"
	if dialect == "sqlite" {
		versionQuery = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("database: %s %s, separate log database: %v", dialect, version, logDB != db)
	return db
}

func TestUserCreatorColumnMigrationPreservesRowsAndIsIdempotent(t *testing.T) {
	db := setupManageUserTestDB(t)
	existing := model.User{Username: "legacy-user", DisplayName: "Legacy User", Role: common.RoleCommonUser, Group: "existing", Quota: 120, AffCode: "legacy-user"}
	require.NoError(t, db.Create(&existing).Error)
	require.True(t, db.Migrator().HasColumn(&model.User{}, "CreatorID"))
	require.NoError(t, db.Migrator().DropTable(&model.UserGroupAssignment{}))
	legacyWithoutAssignments := model.User{
		Username: "legacy-user-without-assignments",
		Role:     common.RoleCommonUser,
		Group:    "existing",
		AffCode:  "legacy-user-without-assignments",
	}
	require.NoError(t, db.Create(&legacyWithoutAssignments).Error)
	require.NoError(t, db.Migrator().DropColumn(&model.User{}, "CreatorID"))
	require.False(t, db.Migrator().HasColumn(&model.User{}, "CreatorID"))

	for range 2 {
		require.NoError(t, db.AutoMigrate(&model.User{}))
	}
	require.True(t, db.Migrator().HasColumn(&model.User{}, "CreatorID"))
	require.True(t, db.Migrator().HasIndex(&model.User{}, "idx_users_creator_id"))
	var migrated model.User
	require.NoError(t, db.First(&migrated, existing.Id).Error)
	assert.Equal(t, existing.Username, migrated.Username)
	assert.Equal(t, existing.DisplayName, migrated.DisplayName)
	assert.Equal(t, existing.Group, migrated.Group)
	assert.Equal(t, existing.Quota, migrated.Quota)
	assert.Zero(t, migrated.CreatorID)
	require.NoError(t, db.AutoMigrate(&model.UserGroupAssignment{}))
	require.NoError(t, db.AutoMigrate(&model.UserGroupAssignment{}))
	require.NoError(t, model.InitializeUserGroupAssignments())
	require.NoError(t, model.InitializeUserGroupAssignments())
	groups, err := model.GetUserGroupAssignments(existing.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"existing"}, groups, "legacy users retain their single billing group as an initial channel-group assignment")
	var assignmentCount int64
	require.NoError(t, db.Model(&model.UserGroupAssignment{}).Where("user_id = ?", existing.Id).Count(&assignmentCount).Error)
	assert.EqualValues(t, 1, assignmentCount, "repeated startup backfill must stay idempotent")
	legacyGroups, err := model.GetUserGroupAssignments(legacyWithoutAssignments.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"existing"}, legacyGroups, "legacy users without an assignment row inherit their billing group during backfill")

	duplicate := model.User{Username: existing.Username, AffCode: "duplicate-legacy-user"}
	assert.Error(t, db.Create(&duplicate).Error, "existing username uniqueness must survive migration")
}

func performManageUserRequest(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 9999)
	c.Set("role", common.RoleRootUser)
	c.Set("username", "root-operator")
	c.Set(common.RequestIdKey, "quota-test-request")
	ManageUser(c)
	return recorder
}

func TestManageUserDisableAdvancesAuthVersionOnceAndRevokesSession(t *testing.T) {
	db := setupManageUserTestDB(t)
	now := time.Now().Unix()
	user := model.User{
		Username: "managed-disable-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.UserSession{
		SID: "managed-disable-session", UserID: user.Id, Version: 1, UserAuthVersion: 1,
		Status: model.UserSessionStatusActive, RefreshHash: "refresh-hash", LoginMethod: "password",
		LastActiveAt: now, ExpiresAt: now + 3600,
	}).Error)

	recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"disable"}`, user.Id))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)

	var updated model.User
	require.NoError(t, db.First(&updated, user.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, updated.Status)
	assert.EqualValues(t, 2, updated.AuthVersion)
	var session model.UserSession
	require.NoError(t, db.First(&session, "sid = ?", "managed-disable-session").Error)
	assert.Equal(t, model.UserSessionStatusRevoked, session.Status)
}

func TestManageUserDemoteAdvancesAuthVersionAndRevokesSessionsOnce(t *testing.T) {
	db := setupManageUserTestDB(t)
	previousMaster := common.IsMasterNode
	common.IsMasterNode = false
	t.Cleanup(func() { common.IsMasterNode = previousMaster })
	require.NoError(t, authz.Init(db))

	now := time.Now().Unix()
	user := model.User{
		Username: "managed-demote-user", Password: "password", Role: common.RoleAdminUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)
	for _, sid := range []string{"managed-demote-session-one", "managed-demote-session-two"} {
		require.NoError(t, db.Create(&model.UserSession{
			SID: sid, UserID: user.Id, Version: 1, UserAuthVersion: 1,
			Status: model.UserSessionStatusActive, RefreshHash: "refresh-" + sid, LoginMethod: "password",
			LastActiveAt: now, ExpiresAt: now + 3600,
		}).Error)
	}

	sessionUpdateCount := 0
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:count_demote_session_updates", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "user_sessions" {
			sessionUpdateCount++
		}
	}))

	recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"demote"}`, user.Id))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)

	var updated model.User
	require.NoError(t, db.First(&updated, user.Id).Error)
	assert.Equal(t, common.RoleCommonUser, updated.Role)
	assert.EqualValues(t, 2, updated.AuthVersion)
	var sessions []model.UserSession
	require.NoError(t, db.Where("user_id = ?", user.Id).Order("sid asc").Find(&sessions).Error)
	require.Len(t, sessions, 2)
	for _, session := range sessions {
		assert.Equal(t, model.UserSessionStatusRevoked, session.Status)
		assert.Equal(t, "admin_demote", session.RevokedReason)
	}
	assert.Equal(t, 1, sessionUpdateCount)
}

func TestManageUserDeleteReturnsImmediatelyAndUnknownActionFails(t *testing.T) {
	db := setupManageUserTestDB(t)
	deleted := model.User{
		Username: "managed-delete-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: "delete-aff",
	}
	require.NoError(t, db.Create(&deleted).Error)

	recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"delete"}`, deleted.Id))
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	var deletedCount int64
	require.NoError(t, db.Unscoped().Model(&model.User{}).Where("id = ? AND deleted_at IS NOT NULL", deleted.Id).Count(&deletedCount).Error)
	assert.EqualValues(t, 1, deletedCount)

	unchanged := model.User{
		Username: "managed-unknown-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: "unknown-aff",
	}
	require.NoError(t, db.Create(&unchanged).Error)
	recorder = performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"unknown"}`, unchanged.Id))
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	require.NoError(t, db.First(&unchanged, unchanged.Id).Error)
	assert.EqualValues(t, 1, unchanged.AuthVersion)
	assert.Equal(t, common.UserStatusEnabled, unchanged.Status)
}

func createQuotaTestOperator(t *testing.T, db *gorm.DB, role int) model.User {
	t.Helper()
	if role == 0 {
		role = common.RoleRootUser
	}
	operator := model.User{Id: 9999, Username: "root-operator", Role: role, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "root-operator-aff"}
	require.NoError(t, db.Create(&operator).Error)
	return operator
}

func TestManageUserQuotaRecordsTopupAndAudit(t *testing.T) {
	for _, tc := range []struct {
		name, mode, action, content string
		value, wantQuota            int
	}{
		{"add", "add", "user.quota_add", "Increased user quota by 500", 500, 1500},
		{"subtract", "subtract", "user.quota_subtract", "Decreased user quota by 500", 500, 500},
		{"override_up", "override", "user.quota_override", "Overrode user quota from 1000 to 1500", 1500, 1500},
		{"override_down", "override", "user.quota_override", "Overrode user quota from 1000 to 500", 500, 500},
		{"override_unchanged", "override", "user.quota_override", "Overrode user quota from 1000 to 1000", 1000, 1000},
		{"override_zero", "override", "user.quota_override", "Overrode user quota from 1000 to 0", 0, 0},
		{"override_negative", "override", "user.quota_override", "Overrode user quota from 1000 to -1", -1, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			user := model.User{Username: "quota-owner", Role: common.RoleCommonUser, Quota: 1000, AffCode: "quota-owner-aff"}
			require.NoError(t, db.Create(&user).Error)
			createQuotaTestOperator(t, db, common.RoleRootUser)

			recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":%q,"value":%d}`, user.Id, tc.mode, tc.value))
			assert.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Body.String(), `"success":true`)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, tc.wantQuota, user.Quota)

			logs, total, err := model.GetAllLogs(model.LogTypeTopup, 0, 0, "", "", "", 0, 20, 0, "", "", "")
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
			require.Len(t, logs, 1)
			assert.Equal(t, user.Id, logs[0].UserId)
			assert.Equal(t, user.Username, logs[0].Username)
			assert.Equal(t, tc.content, logs[0].Content)
			model.FormatAdminLogs(logs)
			var other model.AuditOther
			require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
			assert.Equal(t, &model.AuditAdminInfo{AdminID: 9999, AdminUsername: "root-operator", AdminRole: common.RoleRootUser, AuthMethod: "session"}, other.AdminInfo)

			logs, total, err = model.GetUserLogs(user.Id, model.LogTypeTopup, 0, 0, "", "", 0, 20, "", "", "")
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
			require.Len(t, logs, 1)
			other = model.AuditOther{}
			require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
			assert.Nil(t, other.AdminInfo)
			require.NotNil(t, other.Op)
			assert.Equal(t, tc.action, other.Op.Action)
			params, err := common.Marshal(other.Op.Params)
			require.NoError(t, err)
			expectedParams := model.AuditFields{"target_user_id": user.Id, "target_username": user.Username, "mode": tc.mode, "requested_quota": tc.value, "from": 1000, "to": tc.wantQuota}
			if tc.mode != "override" {
				expectedParams["quota"] = tc.value
			}
			expected, err := common.Marshal(expectedParams)
			require.NoError(t, err)
			assert.JSONEq(t, string(expected), string(params))
			assert.Equal(t, "quota-test-request", logs[0].RequestId)
			assert.Empty(t, logs[0].Ip, "recipient logs must not disclose the administrator IP")

			logs, total, err = model.GetUserLogs(9999, model.LogTypeTopup, 0, 0, "", "", 0, 20, "", "", "")
			require.NoError(t, err)
			assert.Zero(t, total)
			assert.Empty(t, logs)
			var audits []model.AuditLog
			require.NoError(t, model.LOG_DB.Find(&audits).Error)
			require.Len(t, audits, 1)
			assert.Equal(t, 9999, audits[0].UserId)
			assert.Equal(t, "root-operator", audits[0].Username)
			assert.Equal(t, tc.action, audits[0].Action)
			assert.True(t, audits[0].Success)
			params, err = common.Marshal(audits[0].Other.Op.Params)
			require.NoError(t, err)
			assert.JSONEq(t, string(expected), string(params))
			assert.Equal(t, "quota-test-request", audits[0].RequestId)
		})
	}
}

func TestManageUserQuotaFailuresDoNotRecordTopup(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		value      int
		failUpdate bool
	}{
		{"zero_add", "add", 0, false},
		{"negative_subtract", "subtract", -1, false},
		{"invalid_mode", "invalid", 500, false},
		{"add_update_error", "add", 500, true},
		{"subtract_update_error", "subtract", 500, true},
		{"override_update_error", "override", 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			createQuotaTestOperator(t, db, common.RoleRootUser)
			user := model.User{Username: "quota-owner", Role: common.RoleCommonUser, Quota: 1000}
			require.NoError(t, db.Create(&user).Error)
			if tc.failUpdate {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:fail_quota_update", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" {
						tx.AddError(errors.New("quota update unavailable"))
					}
				}))
			}
			recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":%q,"value":%d}`, user.Id, tc.mode, tc.value))
			assert.Contains(t, recorder.Body.String(), `"success":false`)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 1000, user.Quota)
			var logCount, auditCount int64
			require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&logCount).Error)
			require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Count(&auditCount).Error)
			assert.Zero(t, logCount)
			assert.EqualValues(t, 1, auditCount)
			var audit model.AuditLog
			require.NoError(t, model.LOG_DB.First(&audit).Error)
			assert.False(t, audit.Success)
			params, err := common.Marshal(audit.Other.Op.Params)
			require.NoError(t, err)
			assert.NotContains(t, string(params), `"from"`)
			assert.NotContains(t, string(params), `"to"`)
			assert.NotContains(t, string(params), `"target_username"`)
			assert.NotContains(t, string(params), "quota update unavailable")
			reason := "invalid_parameters"
			if tc.failUpdate {
				reason = "database_error"
			}
			assert.Contains(t, string(params), `"failure_reason":"`+reason+`"`)
			assert.Contains(t, string(params), fmt.Sprintf(`"requested_quota":%d`, tc.value))
		})
	}
}

func TestManageUserQuotaLogFailureKeepsSuccessfulAdjustment(t *testing.T) {
	for _, failedTable := range []string{"logs", "audit_logs"} {
		t.Run(failedTable, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			createQuotaTestOperator(t, db, common.RoleRootUser)
			user := model.User{Username: "quota-owner", Role: common.RoleCommonUser, Quota: 1000}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, model.LOG_DB.Callback().Create().Before("gorm:create").Register("test:fail_quota_log", func(tx *gorm.DB) {
				if tx.Statement.Table == failedTable {
					tx.AddError(errors.New("quota log unavailable"))
				}
			}))
			recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":500}`, user.Id))
			assert.Contains(t, recorder.Body.String(), `"success":true`)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 1500, user.Quota)
			var logCount, auditCount int64
			require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&logCount).Error)
			require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Count(&auditCount).Error)
			if failedTable == "logs" {
				assert.Zero(t, logCount)
				assert.EqualValues(t, 1, auditCount)
			} else {
				assert.EqualValues(t, 1, logCount)
				assert.Zero(t, auditCount)
			}
		})
	}
}

func TestManageUserQuotaTargetsAndWalletBounds(t *testing.T) {
	for _, tc := range []struct {
		name, mode, reason                                string
		before, value, targetID, targetRole, operatorRole int
		deleted, failRead                                 bool
	}{
		{name: "zero_id", mode: "add", value: 1, reason: "invalid_parameters"},
		{name: "negative_id", mode: "subtract", value: 1, targetID: -1, reason: "invalid_parameters"},
		{name: "missing", mode: "override", value: 1, targetID: 12345, reason: "target_not_found"},
		{name: "deleted", mode: "add", value: 1, targetID: 1, deleted: true, reason: "target_not_found"},
		{name: "peer_admin", mode: "add", value: 1, targetID: 1, targetRole: common.RoleAdminUser, operatorRole: common.RoleAdminUser, reason: "permission_denied"},
		{name: "higher_role", mode: "override", value: 1, targetID: 1, targetRole: common.RoleRootUser, operatorRole: common.RoleAdminUser, reason: "permission_denied"},
		{name: "read_error", mode: "add", value: 1, targetID: 1, failRead: true, reason: "database_error"},
		{name: "add_overflow", mode: "add", before: common.MaxWalletQuota, value: 1, targetID: 1, reason: "quota_limit_exceeded"},
		{name: "subtract_underflow", mode: "subtract", before: -common.MaxWalletQuota, value: 1, targetID: 1, reason: "quota_limit_exceeded"},
		{name: "oversized_add", mode: "add", value: common.MaxWalletQuota + 1, targetID: 1, reason: "quota_limit_exceeded"},
		{name: "oversized_subtract", mode: "subtract", value: common.MaxWalletQuota + 1, targetID: 1, reason: "quota_limit_exceeded"},
		{name: "oversized_override", mode: "override", value: -common.MaxWalletQuota - 1, targetID: 1, reason: "quota_limit_exceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			createQuotaTestOperator(t, db, tc.operatorRole)
			user := model.User{Id: 1, Username: "private-target-name", Role: tc.targetRole, Quota: tc.before}
			require.NoError(t, db.Create(&user).Error)
			if tc.deleted {
				require.NoError(t, db.Delete(&user).Error)
			}
			if tc.failRead {
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:quota_read_error", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" && len(tx.Statement.Selects) == 0 {
						tx.AddError(errors.New("private database error"))
					}
				}))
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage", strings.NewReader(fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":%q,"value":%d}`, tc.targetID, tc.mode, tc.value)))
			role := tc.operatorRole
			if role == 0 {
				role = common.RoleRootUser
			}
			c.Set("id", 9999)
			c.Set("role", role)
			ManageUser(c)
			require.Contains(t, recorder.Body.String(), `"success":false`)
			if tc.failRead {
				require.NoError(t, db.Callback().Query().Remove("test:quota_read_error"))
			}
			require.NoError(t, db.Unscoped().First(&user, user.Id).Error)
			assert.Equal(t, tc.before, user.Quota)
			var audits []model.AuditLog
			require.NoError(t, model.LOG_DB.Find(&audits).Error)
			require.Len(t, audits, 1)
			assert.False(t, audits[0].Success)
			params, err := common.Marshal(audits[0].Other.Op.Params)
			require.NoError(t, err)
			expected, err := common.Marshal(model.AuditFields{"target_user_id": tc.targetID, "mode": tc.mode, "requested_quota": tc.value, "failure_reason": tc.reason})
			require.NoError(t, err)
			assert.JSONEq(t, string(expected), string(params))
			assert.NotContains(t, audits[0].Content, user.Username)
			var count int64
			require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestManageUserQuotaMiddlewareKeepsOneOperationPerRequest(t *testing.T) {
	db := setupManageUserTestDB(t)
	pat := "quota-middleware-test-token"
	operator := model.User{Id: 9999, Username: "root-operator", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &pat, Quota: 1000}
	require.NoError(t, db.Create(&operator).Error)
	router := gin.New()
	router.Use(middleware.RequestId(), middleware.AccessTokenAudit())
	router.POST("/api/user/manage", middleware.AdminAuth(), ManageUser)
	for _, tc := range []struct {
		body, action string
		success      bool
	}{
		{`{"id":9999,"action":"add_quota","mode":"add","value":100}`, "user.quota_add", true},
		{`{"id":9999,"action":"add_quota","mode":"subtract","value":0}`, "user.quota_subtract", false},
		{`{"id":9999,"action":"add_quota","mode":"invalid","value":1}`, "generic", false},
		{`{"id":`, "generic", false},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/user/manage", strings.NewReader(tc.body))
		request.Header.Set("Authorization", "Bearer "+pat)
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Contains(t, recorder.Body.String(), fmt.Sprintf(`"success":%t`, tc.success))
		requestID := recorder.Header().Get(common.RequestIdKey)
		require.NotEmpty(t, requestID)
		var audits []model.AuditLog
		require.NoError(t, model.LOG_DB.Where("request_id = ?", requestID).Find(&audits).Error)
		require.Len(t, audits, 2, "one operation audit and one PAT request audit")
		for _, audit := range audits {
			assert.Equal(t, tc.success, audit.Success)
			if audit.Category != model.AuditCategoryOperation {
				continue
			}
			assert.Equal(t, tc.action, audit.Action)
			assert.Equal(t, operator.Id, audit.UserId)
			assert.Equal(t, "/api/user/manage", audit.Route)
			if tc.success {
				params, err := common.Marshal(audit.Other.Op.Params)
				require.NoError(t, err)
				assert.Contains(t, string(params), `"target_user_id":9999`)
				assert.Contains(t, string(params), `"target_username":"root-operator"`)
			}
		}
		var count int64
		require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("request_id = ?", requestID).Count(&count).Error)
		if tc.success {
			assert.EqualValues(t, 1, count)
		} else {
			assert.Zero(t, count)
		}
	}
	require.NoError(t, db.First(&operator, operator.Id).Error)
	assert.Equal(t, 1100, operator.Quota)
}

func TestManageUserQuotaConcurrentSnapshots(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{Username: "concurrent-quota", Quota: 1000}
	require.NoError(t, db.Create(&user).Error)
	var ready sync.WaitGroup
	ready.Add(2)
	release := make(chan struct{})
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:concurrent_quota_start", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			ready.Done()
			<-release
		}
	}))
	type result struct {
		adjustment *model.UserQuotaAdjustment
		err        error
		value      int
	}
	results := make(chan result, 2)
	for _, value := range []int{10, 20} {
		go func(value int) {
			adjustment, err := model.AdjustUserQuota(user.Id, common.RoleRootUser, "add", value)
			results <- result{adjustment, err, value}
		}(value)
	}
	ready.Wait()
	close(release)
	var committed []model.UserQuotaAdjustment
	for range 2 {
		result := <-results
		if result.err != nil {
			require.True(t, common.UsingMainDatabase(common.DatabaseTypeSQLite), "row-locking databases must serialize both adjustments: %v", result.err)
			assert.Contains(t, strings.ToLower(result.err.Error()), "locked")
			assert.Nil(t, result.adjustment)
			continue
		}
		require.NotNil(t, result.adjustment)
		assert.Equal(t, result.value, result.adjustment.After-result.adjustment.Before)
		committed = append(committed, *result.adjustment)
	}
	require.NoError(t, db.Callback().Query().Remove("test:concurrent_quota_start"))
	require.NotEmpty(t, committed)
	sort.Slice(committed, func(i, j int) bool { return committed[i].Before < committed[j].Before })
	balance := 1000
	for _, adjustment := range committed {
		assert.Equal(t, balance, adjustment.Before)
		balance = adjustment.After
	}
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, balance, user.Quota)
}

func TestManageUserQuotaCacheUsesCommittedIntegerDifference(t *testing.T) {
	for _, tc := range []struct {
		name, mode                               string
		before, cached, value, after, wantCached int
		failUpdate, failCache, missingCache      bool
	}{
		{name: "add_preserves_reservations", mode: "add", before: 1000, cached: 900, value: 500, after: 1500, wantCached: 1400},
		{name: "subtract_preserves_reservations", mode: "subtract", before: 1000, cached: 900, value: 500, after: 500, wantCached: 400},
		{name: "override_preserves_reservations", mode: "override", before: 1000, cached: 900, value: 2000, after: 2000, wantCached: 1900},
		{name: "large_odd_difference", mode: "override", before: common.MaxWalletQuota - 1, cached: common.MaxWalletQuota - 1, value: -common.MaxWalletQuota, after: -common.MaxWalletQuota, wantCached: -common.MaxWalletQuota},
		{name: "rollback_does_not_change_cache", mode: "subtract", before: 1000, cached: 900, value: 500, after: 1000, wantCached: 900, failUpdate: true},
		{name: "cache_error_keeps_committed_change", mode: "add", before: 1000, value: 500, after: 1500, failCache: true},
		{name: "missing_cache_is_not_partially_created", mode: "add", before: 1000, value: 500, after: 1500, missingCache: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			operator := createQuotaTestOperator(t, db, common.RoleRootUser)
			server := miniredis.RunT(t)
			oldRDB := common.RDB
			common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
			common.RedisEnabled = true
			t.Cleanup(func() { _ = common.RDB.Close(); common.RDB = oldRDB })
			user := model.User{Username: "cached-quota", Quota: tc.before, AuthVersion: 1}
			require.NoError(t, db.Create(&user).Error)
			cache, err := model.GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, tc.before, cache.Quota)
			_, err = model.GetUserCache(operator.Id)
			require.NoError(t, err)
			keys := server.Keys()
			var quotaKey string
			for _, key := range keys {
				if server.HGet(key, "Id") == strconv.Itoa(user.Id) && server.HGet(key, "Quota") != "" {
					quotaKey = key
					break
				}
			}
			require.NotEmpty(t, quotaKey)
			server.HSet(quotaKey, "Quota", strconv.Itoa(tc.cached))
			if tc.missingCache {
				server.Del(quotaKey)
			}
			if tc.failCache {
				server.SetError("ERR quota cache unavailable")
			}
			if tc.failUpdate {
				require.NoError(t, db.Callback().Update().After("gorm:update").Register("test:cache_quota_rollback", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" {
						tx.AddError(errors.New("quota rollback"))
					}
				}))
			}
			recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":%q,"value":%d}`, user.Id, tc.mode, tc.value))
			assert.Contains(t, recorder.Body.String(), fmt.Sprintf(`"success":%t`, !tc.failUpdate))
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, tc.after, user.Quota)
			if tc.missingCache {
				// Log username lookup may hydrate the whole user after commit.
				if server.Exists(quotaKey) {
					assert.Equal(t, strconv.Itoa(user.Id), server.HGet(quotaKey, "Id"))
					assert.NotEmpty(t, server.HGet(quotaKey, "CacheSchema"))
					assert.Equal(t, strconv.Itoa(tc.after), server.HGet(quotaKey, "Quota"))
				}
			} else if !tc.failCache {
				assert.Equal(t, strconv.Itoa(tc.wantCached), server.HGet(quotaKey, "Quota"))
			}
		})
	}
}

// managerRequest exercises the real dashboard role middleware as well as the
// object-level checks. Each test supplies its own user/database state.
func managerRequest(t *testing.T, operator model.User, method, path, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	return managerRequestWithRoute(t, operator, method, path, path, body, handler)
}

func managerRequestWithRoute(t *testing.T, operator model.User, method, route, path, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	ensureManageUserGroups(t, model.DB, operator.Id, []string{operator.Group})
	router := gin.New()
	router.Handle(method, route, middleware.UserManagementAuth(), handler)
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+operator.GetAccessToken())
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func ensureManageUserGroups(t *testing.T, db *gorm.DB, userID int, groups []string) {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&model.UserGroupAssignment{}).Where("user_id = ?", userID).Count(&count).Error)
	if count > 0 {
		return
	}
	if len(groups) == 0 || groups[0] == "" {
		groups = []string{"default"}
	}
	require.NoError(t, model.ReplaceUserGroupAssignmentsTx(db, userID, groups))
}

func setManageUserGroups(t *testing.T, db *gorm.DB, userID int, groups []string) {
	t.Helper()
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return model.ReplaceUserGroupAssignmentsTx(tx, userID, groups)
	}))
}

func TestUserManagerListsAndSearchesOnlyOwnedOrdinaryUsers(t *testing.T) {
	db := setupManageUserTestDB(t)
	pat := "manager-list-token"
	manager := model.User{Username: "manager-list", Role: common.RoleUserManager, Group: "default", Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &pat}
	require.NoError(t, db.Create(&manager).Error)
	for _, user := range []model.User{
		{Username: "child-owned", Role: common.RoleCommonUser, CreatorID: manager.Id, AffCode: "owned"},
		{Username: "child-other", Role: common.RoleCommonUser, CreatorID: manager.Id + 100, AffCode: "other"},
		{Username: "child-legacy", Role: common.RoleCommonUser, AffCode: "legacy"},
		{Username: "child-promoted", Role: common.RoleAdminUser, CreatorID: manager.Id, AffCode: "promoted"},
	} {
		require.NoError(t, db.Create(&user).Error)
	}
	for _, query := range []string{"", "?keyword=child", "?keyword=child-other", "?role=10"} {
		path := "/api/user/"
		handler := GetAllUsers
		if query != "" {
			path, handler = "/api/user/search", SearchUsers
		}
		response := httptest.NewRecorder()
		router := gin.New()
		router.GET(path, middleware.UserManagementAuth(), handler)
		request := httptest.NewRequest("GET", path+query, nil)
		request.Header.Set("Authorization", "Bearer "+pat)
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				Total int          `json:"total"`
				Items []model.User `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		require.True(t, result.Success)
		if query == "?keyword=child-other" || query == "?role=10" {
			assert.Zero(t, result.Data.Total)
			assert.Empty(t, result.Data.Items)
			continue
		}
		assert.Equal(t, 1, result.Data.Total)
		require.Len(t, result.Data.Items, 1)
		assert.Equal(t, "child-owned", result.Data.Items[0].Username)
		assert.Equal(t, manager.Id, result.Data.Items[0].CreatorID)
	}
}

func TestUserManagerCreatesZeroQuotaAndRejectsPrivilegeForging(t *testing.T) {
	db := setupManageUserTestDB(t)
	gift := common.QuotaForNewUser
	common.QuotaForNewUser = 5000
	t.Cleanup(func() { common.QuotaForNewUser = gift })
	pat := "manager-create-token"
	manager := model.User{Username: "manager-create", Role: common.RoleUserManager, Group: "restricted", Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &pat}
	require.NoError(t, db.Create(&manager).Error)
	setManageUserGroups(t, db, manager.Id, []string{"default", "vip"})
	response := managerRequest(t, manager, "POST", "/api/user/", `{"username":"created-child","password":"child-password"}`, CreateUser)
	require.Contains(t, response.Body.String(), `"success":true`)
	var child model.User
	require.NoError(t, db.First(&child, "username = ?", "created-child").Error)
	assert.Equal(t, manager.Id, child.CreatorID)
	assert.Equal(t, common.RoleCommonUser, child.Role)
	assert.Equal(t, "restricted", child.Group)
	groups, err := model.GetUserGroupAssignments(child.Id)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"default", "vip"}, groups, "new delegated users inherit all of their manager's channel groups")
	assert.Zero(t, child.Quota)
	assert.Zero(t, child.InviterId)
	var gifts int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("user_id = ?", child.Id).Count(&gifts).Error)
	assert.Zero(t, gifts)
	for _, field := range []string{`"role":5`, `"role":10`, `"creator_id":123`, `"admin_permissions":{}`, `"group":"privileged"`, `"quota":100`} {
		response := managerRequest(t, manager, "POST", "/api/user/", `{"username":"forged-child","password":"child-password",`+field+`}`, CreateUser)
		assert.Contains(t, response.Body.String(), `"success":false`, field)
	}
	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("username = ?", "forged-child").Count(&count).Error)
	assert.Zero(t, count)
}

func TestDelegatedUserGroupAssignmentsAllowSubsetsAndRejectUnassignedGroups(t *testing.T) {
	db := setupManageUserTestDB(t)
	pat := "manager-group-scope-token"
	manager := model.User{Username: "manager-group-scope", Role: common.RoleUserManager, Group: "default", Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &pat, AffCode: "manager-group-scope"}
	require.NoError(t, db.Create(&manager).Error)
	setManageUserGroups(t, db, manager.Id, []string{"default", "vip"})

	response := managerRequest(t, manager, http.MethodPost, "/api/user/", `{"username":"manager-group-child","password":"child-password"}`, CreateUser)
	require.Contains(t, response.Body.String(), `"success":true`)
	var child model.User
	require.NoError(t, db.First(&child, "username = ?", "manager-group-child").Error)

	response = managerRequest(t, manager, http.MethodPut, "/api/user/", fmt.Sprintf(`{"id":%d,"username":%q,"user_groups":["vip"]}`, child.Id, child.Username), UpdateUser)
	require.Contains(t, response.Body.String(), `"success":true`)
	groups, err := model.GetUserGroupAssignments(child.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"vip"}, groups, "a manager may assign a subset of their own groups")

	limitedToken := "manager-limited-group-token"
	limitedManager := model.User{Username: "limited-manager", Role: common.RoleUserManager, Group: "default", Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &limitedToken, AffCode: "limited-manager"}
	require.NoError(t, db.Create(&limitedManager).Error)
	setManageUserGroups(t, db, limitedManager.Id, []string{"default"})
	response = managerRequest(t, limitedManager, http.MethodPost, "/api/user/", `{"username":"limited-child","password":"child-password"}`, CreateUser)
	require.Contains(t, response.Body.String(), `"success":true`)
	child = model.User{}
	require.NoError(t, db.First(&child, "username = ?", "limited-child").Error)

	response = managerRequest(t, limitedManager, http.MethodPut, "/api/user/", fmt.Sprintf(`{"id":%d,"username":%q,"user_groups":["vip"]}`, child.Id, child.Username), UpdateUser)
	assert.Contains(t, response.Body.String(), `"success":false`, "a valid global group must still be rejected when the manager does not hold it")
	groups, err = model.GetUserGroupAssignments(child.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, groups, "rejected group assignments must leave the child's grants unchanged")
}

func TestAdminCannotChangeThirdLevelDescendantGroupsAndParentRemovalPrunesTree(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, authz.Init(db))
	adminToken := "group-admin-token"
	thirdToken := "group-third-token"
	admin := model.User{Username: "group-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &adminToken, AffCode: "admin-user-group-assignment"}
	third := model.User{Username: "group-third", Role: common.RoleThirdLevelUser, Group: "default", Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &thirdToken, AffCode: "third-level-user-group-assignment"}
	require.NoError(t, db.Create(&admin).Error)
	require.NoError(t, db.Create(&third).Error)
	setManageUserGroups(t, db, admin.Id, []string{"default"})
	setManageUserGroups(t, db, third.Id, []string{"default", "vip"})

	response := managerRequest(t, third, http.MethodPost, "/api/user/", `{"username":"third-second","password":"child-password","role":5}`, CreateUser)
	require.Contains(t, response.Body.String(), `"success":true`)
	var secondLevel model.User
	require.NoError(t, db.First(&secondLevel, "username = ?", "third-second").Error)
	secondLevelToken := "second-level-token"
	require.NoError(t, db.Model(&secondLevel).Update("access_token", secondLevelToken).Error)
	secondLevel.SetAccessToken(secondLevelToken)
	secondGroups, err := model.GetUserGroupAssignments(secondLevel.Id)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"default", "vip"}, secondGroups)

	response = managerRequest(t, secondLevel, http.MethodPost, "/api/user/", `{"username":"third-leaf","password":"child-password"}`, CreateUser)
	require.Contains(t, response.Body.String(), `"success":true`)
	var ordinary model.User
	require.NoError(t, db.First(&ordinary, "username = ?", "third-leaf").Error)
	ordinaryGroups, err := model.GetUserGroupAssignments(ordinary.Id)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"default", "vip"}, ordinaryGroups)
	deletedOrdinary := model.User{
		Username:   "third-deleted-leaf",
		Role:       common.RoleCommonUser,
		Group:      "default",
		UserGroups: []string{"default", "vip"},
		Status:     common.UserStatusEnabled,
		CreatorID:  third.Id,
	}
	require.NoError(t, db.Create(&deletedOrdinary).Error)
	require.NoError(t, db.Delete(&deletedOrdinary).Error)

	for _, descendant := range []model.User{secondLevel, ordinary} {
		response = managerRequest(t, admin, http.MethodPut, "/api/user/", fmt.Sprintf(`{"id":%d,"username":%q,"user_groups":["vip"]}`, descendant.Id, descendant.Username), UpdateUser)
		assert.Contains(t, response.Body.String(), `"success":false`, "admins cannot assign groups directly to users below a third-level user")
		groups, err := model.GetUserGroupAssignments(descendant.Id)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"default", "vip"}, groups)
	}

	response = managerRequest(t, admin, http.MethodPut, "/api/user/", fmt.Sprintf(`{"id":%d,"username":%q,"user_groups":["default"]}`, third.Id, third.Username), UpdateUser)
	require.Contains(t, response.Body.String(), `"success":true`)
	deletedGroups, err := model.GetUserGroupAssignments(deletedOrdinary.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, deletedGroups, "deleted descendants must still have revoked assignments persisted")
	for _, descendant := range []model.User{secondLevel, ordinary} {
		groups, err := model.GetUserGroupAssignments(descendant.Id)
		require.NoError(t, err)
		assert.Equal(t, []string{"default"}, groups, "removing a parent grant must prune it throughout the management tree")
		var current model.User
		require.NoError(t, db.First(&current, descendant.Id).Error)
		assert.EqualValues(t, 2, current.AuthVersion, "pruned descendants must advance their authentication version")
	}
}

func TestThirdLevelUserManagesOnlyDirectUsersAndCannotOrphanSecondLevelUsers(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, authz.Init(db))
	pat := "third-level-scope-token"
	manager := model.User{Username: "third-level-manager", Role: common.RoleThirdLevelUser, Group: "restricted", Quota: 1000, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &pat, AffCode: "third-level-manager"}
	require.NoError(t, db.Create(&manager).Error)
	secondLevel := model.User{Username: "second-level-child", Role: common.RoleUserManager, CreatorID: manager.Id, Group: manager.Group, Quota: 100, AffCode: "second-level-child"}
	directUser := model.User{Username: "direct-common-child", Role: common.RoleCommonUser, CreatorID: manager.Id, Group: manager.Group, AffCode: "direct-common-child"}
	otherUser := model.User{Username: "unrelated-common-user", Role: common.RoleCommonUser, CreatorID: manager.Id + 100, AffCode: "unrelated-common-user"}
	require.NoError(t, db.Create(&secondLevel).Error)
	require.NoError(t, db.Create(&directUser).Error)
	require.NoError(t, db.Create(&otherUser).Error)
	grandchild := model.User{Username: "second-level-grandchild", Role: common.RoleCommonUser, CreatorID: secondLevel.Id, AffCode: "second-level-grandchild"}
	require.NoError(t, db.Create(&grandchild).Error)

	response := managerRequest(t, manager, http.MethodGet, "/api/user/", "", GetAllUsers)
	require.Equal(t, http.StatusOK, response.Code)
	var result struct {
		Data struct {
			Total int          `json:"total"`
			Items []model.User `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.Equal(t, 2, result.Data.Total)
	assert.ElementsMatch(t, []string{secondLevel.Username, directUser.Username}, []string{result.Data.Items[0].Username, result.Data.Items[1].Username})

	for _, target := range []struct {
		user    model.User
		visible bool
	}{{secondLevel, true}, {directUser, true}, {grandchild, false}, {otherUser, false}} {
		response = managerRequestWithRoute(t, manager, http.MethodGet, "/api/user/:id", fmt.Sprintf("/api/user/%d", target.user.Id), "", GetUser)
		if target.visible {
			assert.Contains(t, response.Body.String(), `"success":true`)
		} else {
			assert.Contains(t, response.Body.String(), `"success":false`)
			assert.NotContains(t, response.Body.String(), target.user.Username)
		}
	}

	for _, request := range []struct {
		method, path, body string
		handler            gin.HandlerFunc
	}{
		{http.MethodDelete, fmt.Sprintf("/api/user/%d", secondLevel.Id), "", DeleteUser},
		{http.MethodPost, "/api/user/manage", fmt.Sprintf(`{"id":%d,"action":"demote"}`, secondLevel.Id), ManageUser},
	} {
		if request.method == http.MethodDelete {
			response = managerRequestWithRoute(t, manager, request.method, "/api/user/:id", request.path, request.body, request.handler)
		} else {
			response = managerRequest(t, manager, request.method, request.path, request.body, request.handler)
		}
		assert.Contains(t, response.Body.String(), `"success":false`)
	}
	require.NoError(t, db.First(&secondLevel, secondLevel.Id).Error)
	assert.Equal(t, common.RoleUserManager, secondLevel.Role)

	response = managerRequest(t, manager, http.MethodPost, "/api/user/manage", fmt.Sprintf(`{"id":%d,"action":"promote_manager"}`, directUser.Id), ManageUser)
	require.Contains(t, response.Body.String(), `"success":true`)
	require.NoError(t, db.First(&directUser, directUser.Id).Error)
	assert.Equal(t, common.RoleUserManager, directUser.Role)

	response = managerRequest(t, manager, http.MethodPost, "/api/user/", `{"username":"created-second-level","password":"child-password","role":5}`, CreateUser)
	require.Contains(t, response.Body.String(), `"success":true`)
	var created model.User
	require.NoError(t, db.First(&created, "username = ?", "created-second-level").Error)
	assert.Equal(t, common.RoleUserManager, created.Role)
	assert.Equal(t, manager.Id, created.CreatorID)
	assert.Equal(t, manager.Group, created.Group)
	assert.Zero(t, created.Quota)

	archivedManager := model.User{Username: "archived-child-manager", Role: common.RoleUserManager, CreatorID: manager.Id, Group: manager.Group, AffCode: "archived-child-manager"}
	archivedUser := model.User{Username: "archived-grandchild", Role: common.RoleCommonUser, CreatorID: archivedManager.Id, AffCode: "archived-grandchild"}
	require.NoError(t, db.Create(&archivedManager).Error)
	require.NoError(t, db.Create(&archivedUser).Error)
	require.NoError(t, db.Delete(&archivedUser).Error)
	response = managerRequest(t, manager, http.MethodPost, "/api/user/manage", fmt.Sprintf(`{"id":%d,"action":"demote"}`, archivedManager.Id), ManageUser)
	require.Contains(t, response.Body.String(), `"success":true`, "soft-deleted children do not keep their parent permanently undeletable")
	require.NoError(t, db.First(&archivedManager, archivedManager.Id).Error)
	assert.Equal(t, common.RoleCommonUser, archivedManager.Role)
}

func TestAdminCanReadEveryUserDetailWithoutGainingWriteAccess(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, authz.Init(db))
	pat := "admin-details-token"
	admin := model.User{Username: "details-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &pat, AffCode: "details-admin"}
	peerAdmin := model.User{Username: "peer-admin", Role: common.RoleAdminUser, AffCode: "peer-admin"}
	root := model.User{Username: "details-root", Role: common.RoleRootUser, AffCode: "details-root"}
	require.NoError(t, db.Create(&admin).Error)
	require.NoError(t, db.Create(&peerAdmin).Error)
	require.NoError(t, db.Create(&root).Error)

	for _, target := range []model.User{peerAdmin, root} {
		response := managerRequestWithRoute(t, admin, http.MethodGet, "/api/user/:id", fmt.Sprintf("/api/user/%d", target.Id), "", GetUser)
		assert.Contains(t, response.Body.String(), `"success":true`)
		assert.Contains(t, response.Body.String(), target.Username)
	}
	response := managerRequest(t, admin, http.MethodPost, "/api/user/manage", fmt.Sprintf(`{"id":%d,"action":"disable"}`, peerAdmin.Id), ManageUser)
	assert.Contains(t, response.Body.String(), `"success":false`)
	require.NoError(t, db.First(&peerAdmin, peerAdmin.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, peerAdmin.Status)
}

func TestAdminPromotionToThirdLevelClearsPreviousManagementParent(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, authz.Init(db))
	pat := "third-level-promotion-admin"
	admin := model.User{Username: "third-level-promotion-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &pat, AffCode: "third-level-promotion-admin"}
	parent := model.User{Username: "third-level-promotion-parent", Role: common.RoleThirdLevelUser, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "third-level-promotion-parent"}
	child := model.User{Username: "third-level-promotion-child", Role: common.RoleCommonUser, CreatorID: parent.Id, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "third-level-promotion-child"}
	require.NoError(t, db.Create(&admin).Error)
	require.NoError(t, db.Create(&parent).Error)
	child.CreatorID = parent.Id
	require.NoError(t, db.Create(&child).Error)

	response := managerRequest(t, admin, http.MethodPost, "/api/user/manage", fmt.Sprintf(`{"id":%d,"action":"promote_third_level"}`, child.Id), ManageUser)
	require.Contains(t, response.Body.String(), `"success":true`)
	require.NoError(t, db.First(&child, child.Id).Error)
	assert.Equal(t, common.RoleThirdLevelUser, child.Role)
	assert.Zero(t, child.CreatorID)

	scope := model.UserManagementScope{OperatorID: admin.Id, OperatorRole: admin.Role}
	children, total, err := model.FindManagedUserChildren(scope, parent.Id, "", "", nil, nil, 0, 20)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.NotContains(t, children, &child)
	roots, total, err := model.FindManagedUserTreeRoots(scope, "", "", nil, nil, 0, 20)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, int64(1))
	assert.Contains(t, roots, &child)
}

func TestUserManagerCannotMutateOtherUsersOrElevateChildren(t *testing.T) {
	db := setupManageUserTestDB(t)
	pat := "manager-scope-token"
	manager := model.User{Username: "manager-scope", Role: common.RoleUserManager, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &pat}
	require.NoError(t, db.Create(&manager).Error)
	owned := model.User{Username: "owned-scope", Role: common.RoleCommonUser, CreatorID: manager.Id, Group: "restricted", Quota: 100, AffCode: "owned-scope"}
	other := model.User{Username: "other-scope", Role: common.RoleCommonUser, CreatorID: manager.Id + 100, Group: "default", Quota: 100, AffCode: "other-scope"}
	require.NoError(t, db.Create(&owned).Error)
	require.NoError(t, db.Create(&other).Error)
	for _, action := range []string{"enable", "disable", "delete", "promote", "promote_manager", "demote", "add_quota"} {
		response := managerRequest(t, manager, "POST", "/api/user/manage", fmt.Sprintf(`{"id":%d,"action":%q,"mode":"add","value":10}`, other.Id, action), ManageUser)
		assert.Contains(t, response.Body.String(), `"success":false`, action)
	}
	for _, action := range []string{"promote", "promote_manager", "demote"} {
		response := managerRequest(t, manager, "POST", "/api/user/manage", fmt.Sprintf(`{"id":%d,"action":%q}`, owned.Id, action), ManageUser)
		assert.Contains(t, response.Body.String(), `"success":false`, action)
	}
	for _, field := range []string{`"role":5`, `"creator_id":123`, `"admin_permissions":{}`, `"group":"privileged"`} {
		response := managerRequest(t, manager, "PUT", "/api/user/", fmt.Sprintf(`{"id":%d,"username":"owned-scope",%s}`, owned.Id, field), UpdateUser)
		assert.Contains(t, response.Body.String(), `"success":false`, field)
	}
	response := managerRequest(t, manager, "PUT", "/api/user/", fmt.Sprintf(`{"id":%d,"username":"owned-scope","display_name":"Updated"}`, owned.Id), UpdateUser)
	require.Contains(t, response.Body.String(), `"success":true`)
	require.NoError(t, db.First(&owned, owned.Id).Error)
	assert.Equal(t, "Updated", owned.DisplayName)
	assert.Equal(t, "restricted", owned.Group)
	assert.Equal(t, manager.Id, owned.CreatorID)
	response = managerRequest(t, manager, "POST", "/api/user/manage", fmt.Sprintf(`{"id":%d,"action":"delete"}`, owned.Id), ManageUser)
	assert.Contains(t, response.Body.String(), `"success":false`, "remaining quota must be reclaimed before deletion")
	require.NoError(t, db.First(&other, other.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, other.Status)
	assert.Equal(t, 100, other.Quota)
}

func TestUserManagerQuotaTransfersConserveBalancesAndRollBackTogether(t *testing.T) {
	for _, tc := range []struct {
		name, mode                                    string
		manager, child, value, wantManager, wantChild int
		failUpdate, failRole, failOwner, disabled     bool
	}{
		{name: "allocate", mode: "add", manager: 1000, child: 100, value: 500, wantManager: 500, wantChild: 600},
		{name: "reclaim", mode: "subtract", manager: 1000, child: 100, value: 50, wantManager: 1050, wantChild: 50},
		{name: "override_up", mode: "override", manager: 1000, child: 100, value: 500, wantManager: 600, wantChild: 500},
		{name: "override_zero", mode: "override", manager: 1000, child: 100, value: 0, wantManager: 1100, wantChild: 0},
		{name: "same_value", mode: "override", manager: 1000, child: 100, value: 100, wantManager: 1000, wantChild: 100},
		{name: "insufficient_manager", mode: "add", manager: 10, child: 100, value: 500, wantManager: 10, wantChild: 100},
		{name: "insufficient_child", mode: "subtract", manager: 1000, child: 100, value: 500, wantManager: 1000, wantChild: 100},
		{name: "negative_override", mode: "override", manager: 1000, child: 100, value: -1, wantManager: 1000, wantChild: 100},
		{name: "destination_overflow", mode: "subtract", manager: common.MaxWalletQuota, child: 100, value: 1, wantManager: common.MaxWalletQuota, wantChild: 100},
		{name: "target_overflow", mode: "add", manager: 1000, child: common.MaxWalletQuota, value: 1, wantManager: 1000, wantChild: common.MaxWalletQuota},
		{name: "rollback_second_write", mode: "add", manager: 1000, child: 100, value: 500, wantManager: 1000, wantChild: 100, failUpdate: true},
		{name: "revoked_manager", mode: "add", manager: 1000, child: 100, value: 500, wantManager: 1000, wantChild: 100, failRole: true},
		{name: "wrong_owner", mode: "add", manager: 1000, child: 100, value: 500, wantManager: 1000, wantChild: 100, failOwner: true},
		{name: "disabled_manager", mode: "add", manager: 1000, child: 100, value: 500, wantManager: 1000, wantChild: 100, disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			manager := model.User{Username: "wallet-manager", Role: common.RoleUserManager, Quota: tc.manager, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "wallet-manager"}
			require.NoError(t, db.Create(&manager).Error)
			child := model.User{Username: "wallet-child", Role: common.RoleCommonUser, CreatorID: manager.Id, Quota: tc.child, AuthVersion: 1, AffCode: "wallet-child"}
			require.NoError(t, db.Create(&child).Error)
			if tc.failRole {
				require.NoError(t, db.Model(&manager).Update("role", common.RoleCommonUser).Error)
			}
			if tc.disabled {
				require.NoError(t, db.Model(&manager).Update("status", common.UserStatusDisabled).Error)
			}
			if tc.failOwner {
				require.NoError(t, db.Model(&child).Update("creator_id", manager.Id+100).Error)
			}
			if tc.failUpdate {
				writes := 0
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:fail_second_transfer_write", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" {
						writes++
						if writes == 2 {
							tx.AddError(errors.New("second transfer write failed"))
						}
					}
				}))
			}
			adjustment, err := model.TransferManagedUserQuota(child.Id, manager.Id, manager.Role, tc.mode, tc.value)
			if tc.wantChild != tc.child || tc.name == "same_value" {
				require.NoError(t, err)
				require.NotNil(t, adjustment)
			} else {
				require.Error(t, err)
				assert.Nil(t, adjustment)
			}
			require.NoError(t, db.First(&manager, manager.Id).Error)
			require.NoError(t, db.First(&child, child.Id).Error)
			assert.Equal(t, tc.wantManager, manager.Quota)
			assert.Equal(t, tc.wantChild, child.Quota)
			assert.Equal(t, int64(tc.manager)+int64(tc.child), int64(manager.Quota)+int64(child.Quota))
		})
	}
}

func TestUserManagerQuotaCacheReservationsArePreserved(t *testing.T) {
	for _, tc := range []struct {
		name, mode                                                                   string
		sourceCache, value, wantManager, wantChild, wantManagerCache, wantChildCache int
		failUpdate, failCache                                                        bool
	}{
		{name: "reserved_manager_funds", mode: "add", sourceCache: 100, value: 200, wantManager: 1000, wantChild: 1000, wantManagerCache: 100, wantChildCache: 900},
		{name: "reserved_child_funds", mode: "subtract", sourceCache: 100, value: 200, wantManager: 1000, wantChild: 1000, wantManagerCache: 900, wantChildCache: 100},
		{name: "allocate_preserves_pending", mode: "add", sourceCache: 900, value: 200, wantManager: 800, wantChild: 1200, wantManagerCache: 700, wantChildCache: 1100},
		{name: "rollback_restores_cache", mode: "add", sourceCache: 900, value: 200, wantManager: 1000, wantChild: 1000, wantManagerCache: 900, wantChildCache: 900, failUpdate: true},
		{name: "redis_failure_rejects_transfer", mode: "add", sourceCache: 900, value: 200, wantManager: 1000, wantChild: 1000, failCache: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			manager := model.User{Username: "cache-manager", Role: common.RoleUserManager, Quota: 1000, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "cache-manager"}
			require.NoError(t, db.Create(&manager).Error)
			child := model.User{Username: "cache-child", Role: common.RoleCommonUser, CreatorID: manager.Id, Quota: 1000, AuthVersion: 1, AffCode: "cache-child"}
			require.NoError(t, db.Create(&child).Error)
			server := miniredis.RunT(t)
			oldRDB := common.RDB
			common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
			common.RedisEnabled = true
			t.Cleanup(func() { _ = common.RDB.Close(); common.RDB = oldRDB })
			for _, id := range []int{manager.Id, child.Id} {
				_, err := model.GetUserCache(id)
				require.NoError(t, err)
			}
			var managerKey, childKey string
			for _, key := range server.Keys() {
				if server.HGet(key, "Id") == strconv.Itoa(manager.Id) {
					managerKey = key
				}
				if server.HGet(key, "Id") == strconv.Itoa(child.Id) {
					childKey = key
				}
			}
			require.NotEmpty(t, managerKey)
			require.NotEmpty(t, childKey)
			server.HSet(managerKey, "Quota", "900")
			server.HSet(childKey, "Quota", "900")
			if tc.mode == "add" {
				server.HSet(managerKey, "Quota", strconv.Itoa(tc.sourceCache))
			} else {
				server.HSet(childKey, "Quota", strconv.Itoa(tc.sourceCache))
			}
			if tc.failCache {
				server.SetError("ERR transfer cache unavailable")
			}
			if tc.failUpdate {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:transfer_cache_rollback", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" {
						tx.AddError(errors.New("transfer rejected"))
					}
				}))
			}
			_, err := model.TransferManagedUserQuota(child.Id, manager.Id, manager.Role, tc.mode, tc.value)
			if tc.wantChild != 1000 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.NoError(t, db.First(&manager, manager.Id).Error)
			require.NoError(t, db.First(&child, child.Id).Error)
			assert.Equal(t, tc.wantManager, manager.Quota)
			assert.Equal(t, tc.wantChild, child.Quota)
			if !tc.failCache {
				assert.Equal(t, strconv.Itoa(tc.wantManagerCache), server.HGet(managerKey, "Quota"))
				assert.Equal(t, strconv.Itoa(tc.wantChildCache), server.HGet(childKey, "Quota"))
			}
		})
	}
}

func TestUserManagerPasswordResetRequiresBoundSingleUseProof(t *testing.T) {
	for _, tc := range []struct{ name, proofCase string }{
		{"missing", "missing"}, {"wrong_target", "wrong_target"}, {"expired", "expired"}, {"success_and_replay", "valid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager, identity := setupSecurityEnrollmentTest(t)
			require.NoError(t, model.DB.Model(manager).Update("role", common.RoleUserManager).Error)
			manager.Role = common.RoleUserManager
			oldHash, err := common.HashAccountPassword("original-child-password")
			require.NoError(t, err)
			child := model.User{Username: "proof-child", Password: oldHash, CreatorID: manager.Id, Role: common.RoleCommonUser, Group: "default", AuthVersion: 1, AffCode: "proof-child"}
			require.NoError(t, model.DB.Create(&child).Error)
			proof := ""
			if tc.proofCase != "missing" {
				targetID := child.Id
				if tc.proofCase == "wrong_target" {
					targetID++
				}
				payload, err := common.Marshal(service.UserPasswordResetContext{TargetUserID: targetID})
				require.NoError(t, err)
				proof = issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: service.VerificationScopeUserPasswordReset, Context: payload}, service.VerificationMethodPassword)
			}
			if tc.proofCase == "expired" {
				require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("purpose = ?", model.AuthFlowPurposeSecurityProof).Update("expires_at", time.Now().Add(-time.Minute)).Error)
			}
			body := fmt.Sprintf(`{"id":%d,"username":"proof-child","password":"reset-child-password"}`, child.Id)
			handler := func(c *gin.Context) { c.Set("role", common.RoleUserManager); UpdateUser(c) }
			response := securityEnrollmentRequest("PUT", "/api/user/", body, proof, identity, handler)
			if tc.proofCase != "valid" {
				assert.Equal(t, http.StatusForbidden, response.Code)
				assert.Contains(t, response.Body.String(), `"success":false`)
				require.NoError(t, model.DB.First(&child, child.Id).Error)
				assert.Equal(t, oldHash, child.Password)
				assert.EqualValues(t, 1, child.AuthVersion)
				return
			}
			require.Contains(t, response.Body.String(), `"success":true`)
			require.NoError(t, model.DB.First(&child, child.Id).Error)
			assert.True(t, common.ValidatePasswordAndHash("reset-child-password", child.Password))
			assert.EqualValues(t, 2, child.AuthVersion)
			response = securityEnrollmentRequest("PUT", "/api/user/", body, proof, identity, handler)
			assert.Equal(t, http.StatusForbidden, response.Code)
			assert.Contains(t, response.Body.String(), "SECURITY_PROOF_CONSUMED")
			var audits []model.AuditLog
			require.NoError(t, model.LOG_DB.Find(&audits).Error)
			for _, audit := range audits {
				encoded, err := common.Marshal(audit)
				require.NoError(t, err)
				assert.NotContains(t, string(encoded), "reset-child-password")
				assert.NotContains(t, string(encoded), proof)
			}
		})
	}
	for _, payload := range []string{`{}`, `{"target_user_id":0}`, `{"target_user_id":-1}`, `{"target_user_id":1,"role":100}`} {
		_, err := service.BindVerificationOperation(service.VerificationOperation{Scope: service.VerificationScopeUserPasswordReset, Context: []byte(payload)})
		assert.Error(t, err, payload)
	}
}

func TestAdminPasswordResetSendsTargetBoundEmailAfterStepUp(t *testing.T) {
	operator, identity := setupSecurityEnrollmentTest(t)
	operator.Role = common.RoleAdminUser
	require.NoError(t, model.DB.Model(operator).Update("role", operator.Role).Error)
	target := model.User{Username: "admin-reset-target", Email: "reset-target@example.com", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: "admin-reset-target"}
	require.NoError(t, model.DB.Create(&target).Error)

	passwordHash, err := common.HashAccountPassword("original-target-password")
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&target).Update("password", passwordHash).Error)
	updateBody := fmt.Sprintf(`{"id":%d,"username":%q,"role":%d,"password":"admin-chosen-password"}`, target.Id, target.Username, target.Role)
	updateResponse := securityEnrollmentRequest("PUT", "/api/user/", updateBody, "", identity, func(c *gin.Context) {
		c.Set("role", common.RoleAdminUser)
		UpdateUser(c)
	})
	assert.Contains(t, updateResponse.Body.String(), "管理员不能直接设置其他用户的密码")
	stored := model.User{}
	require.NoError(t, model.DB.First(&stored, target.Id).Error)
	assert.Equal(t, passwordHash, stored.Password)

	adminResetHandler := func(c *gin.Context) {
		c.Set("role", common.RoleAdminUser)
		c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(target.Id)}}
		AdminSendUserPasswordResetEmail(c)
	}
	missingProof := securityEnrollmentRequest("POST", "/api/user/2/reset_password", "", "", identity, adminResetHandler)
	assert.Equal(t, http.StatusForbidden, missingProof.Code)
	assert.Contains(t, missingProof.Body.String(), "SECURITY_PROOF_REQUIRED")

	port, messages := startUserManagementSMTPServer(t)
	oldSMTPServer, oldSMTPPort := common.SMTPServer, common.SMTPPort
	oldSMTPSSL, oldSMTPStartTLS, oldSMTPInsecure := common.SMTPSSLEnabled, common.SMTPStartTLSEnabled, common.SMTPInsecureSkipVerify
	oldSMTPAccount, oldSMTPFrom, oldSMTPToken := common.SMTPAccount, common.SMTPFrom, common.SMTPToken
	oldSMTPForceAuth := common.SMTPForceAuthLogin
	oldServerAddress := system_setting.ServerAddress
	t.Cleanup(func() {
		common.SMTPServer, common.SMTPPort = oldSMTPServer, oldSMTPPort
		common.SMTPSSLEnabled, common.SMTPStartTLSEnabled, common.SMTPInsecureSkipVerify = oldSMTPSSL, oldSMTPStartTLS, oldSMTPInsecure
		common.SMTPAccount, common.SMTPFrom, common.SMTPToken = oldSMTPAccount, oldSMTPFrom, oldSMTPToken
		common.SMTPForceAuthLogin = oldSMTPForceAuth
		system_setting.ServerAddress = oldServerAddress
	})
	common.SMTPServer = "127.0.0.1"
	common.SMTPPort = port
	common.SMTPSSLEnabled = false
	common.SMTPStartTLSEnabled = false
	common.SMTPInsecureSkipVerify = false
	common.SMTPAccount = ""
	common.SMTPFrom = "no-reply@example.com"
	common.SMTPToken = ""
	common.SMTPForceAuthLogin = false
	system_setting.ServerAddress = "https://example.test"

	payload, err := common.Marshal(service.UserPasswordResetContext{TargetUserID: target.Id})
	require.NoError(t, err)
	proof := issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: service.VerificationScopeUserPasswordReset, Context: payload}, service.VerificationMethodPassword)
	response := securityEnrollmentRequest("POST", "/api/user/2/reset_password", "", proof, identity, adminResetHandler)
	assert.Contains(t, response.Body.String(), `"success":true`)
	select {
	case message := <-messages:
		assert.Contains(t, message, "To: reset-target@example.com")
		assert.Contains(t, message, "https://example.test/user/reset?email=reset-target%40example.com&amp;token=")
		assert.Contains(t, message, fmt.Sprintf("&amp;user_id=%d", target.Id))
		match := regexp.MustCompile(`token=([0-9a-f]{32})`).FindStringSubmatch(message)
		require.Len(t, match, 2)
		verificationKey := managedPasswordResetVerificationKey(target.Id, target.Email)
		assert.True(t, common.VerifyCodeWithKey(verificationKey, match[1], common.ManagedPasswordResetPurpose))
		duplicate := model.User{Username: "admin-reset-duplicate", Password: "duplicate-password", Email: "RESET-TARGET@example.com", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "admin-reset-duplicate"}
		require.NoError(t, model.DB.Create(&duplicate).Error)
		resetBody, err := common.Marshal(PasswordResetRequest{Email: target.Email, Token: match[1], UserID: target.Id})
		require.NoError(t, err)
		resetRecorder := httptest.NewRecorder()
		resetContext, _ := gin.CreateTestContext(resetRecorder)
		resetContext.Request = httptest.NewRequest(http.MethodPost, "/api/user/reset", strings.NewReader(string(resetBody)))
		ResetPassword(resetContext)
		require.Contains(t, resetRecorder.Body.String(), `"success":true`)
		assert.False(t, common.VerifyCodeWithKey(verificationKey, match[1], common.ManagedPasswordResetPurpose))
		var duplicateStored model.User
		require.NoError(t, model.DB.First(&duplicateStored, duplicate.Id).Error)
		assert.Equal(t, duplicate.Password, duplicateStored.Password)

		replayRecorder := httptest.NewRecorder()
		replayContext, _ := gin.CreateTestContext(replayRecorder)
		replayContext.Request = httptest.NewRequest(http.MethodPost, "/api/user/reset", strings.NewReader(string(resetBody)))
		ResetPassword(replayContext)
		assert.NotContains(t, replayRecorder.Body.String(), `"success":true`)

		oldEmail := target.Email
		require.NoError(t, model.DB.Model(&target).Update("email", "changed-reset-target@example.com").Error)
		require.NoError(t, model.DB.Model(&duplicate).Update("email", "moved-reset-duplicate@example.com").Error)
		replacement := model.User{Username: "admin-reset-replacement", Password: "replacement-password", Email: oldEmail, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "admin-reset-replacement"}
		require.NoError(t, model.DB.Create(&replacement).Error)
		common.RegisterVerificationCodeWithKey(verificationKey, "reassigned-token", common.ManagedPasswordResetPurpose)
		reassignedBody, err := common.Marshal(PasswordResetRequest{Email: oldEmail, Token: "reassigned-token", UserID: target.Id})
		require.NoError(t, err)
		reassignedRecorder := httptest.NewRecorder()
		reassignedContext, _ := gin.CreateTestContext(reassignedRecorder)
		reassignedContext.Request = httptest.NewRequest(http.MethodPost, "/api/user/reset", strings.NewReader(string(reassignedBody)))
		ResetPassword(reassignedContext)
		assert.NotContains(t, reassignedRecorder.Body.String(), `"success":true`)
		var replacementStored model.User
		require.NoError(t, model.DB.First(&replacementStored, replacement.Id).Error)
		assert.Equal(t, replacement.Password, replacementStored.Password)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for password reset email")
	}
	require.NoError(t, model.DB.First(&stored, target.Id).Error)
	assert.NotEqual(t, passwordHash, stored.Password)
	assert.NotEmpty(t, stored.Password)
}

func startUserManagementSMTPServer(t *testing.T) (int, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	messages := make(chan string, 1)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		reader := bufio.NewReader(connection)
		writer := bufio.NewWriter(connection)
		respond := func(value string) bool {
			if _, err := fmt.Fprint(writer, value+"\r\n"); err != nil {
				return false
			}
			return writer.Flush() == nil
		}
		if !respond("220 localhost ESMTP") {
			return
		}
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			command := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(command, "EHLO "), strings.HasPrefix(command, "HELO "), strings.HasPrefix(command, "MAIL FROM:"), strings.HasPrefix(command, "RCPT TO:"):
				if !respond("250 localhost") {
					return
				}
			case command == "DATA":
				if !respond("354 end with <CRLF>.<CRLF>") {
					return
				}
				var message strings.Builder
				for {
					dataLine, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if dataLine == ".\r\n" {
						break
					}
					message.WriteString(dataLine)
				}
				messages <- message.String()
				if !respond("250 queued") {
					return
				}
			case command == "QUIT":
				respond("221 bye")
				return
			default:
				if !respond("250 ok") {
					return
				}
			}
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, messages
}

func TestUserHierarchyScopesChildrenAndInheritsThirdLevelGroup(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}, &model.UserSubscription{}))
	adminToken := "hierarchy-admin-token"
	rootToken := "hierarchy-root-token"
	thirdToken := "hierarchy-third-token"
	admin := model.User{Username: "hierarchy-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &adminToken, AffCode: "hierarchy-admin"}
	root := model.User{Username: "hierarchy-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &rootToken, AffCode: "hierarchy-root"}
	third := model.User{Username: "hierarchy-third", Role: common.RoleThirdLevelUser, Group: "default", Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &thirdToken, AffCode: "hierarchy-third"}
	for _, user := range []*model.User{&admin, &root, &third} {
		require.NoError(t, db.Create(user).Error)
	}
	secondLevel := model.User{Username: "hierarchy-second", Role: common.RoleUserManager, CreatorID: third.Id, Group: "default", AuthVersion: 1, AffCode: "hierarchy-second"}
	directUser := model.User{Username: "hierarchy-direct", Role: common.RoleCommonUser, CreatorID: third.Id, Group: "default", AuthVersion: 1, AffCode: "hierarchy-direct"}
	grandchild := model.User{Username: "hierarchy-grandchild", Role: common.RoleCommonUser, CreatorID: secondLevel.Id, Group: "default", AuthVersion: 1, AffCode: "hierarchy-grandchild"}
	archivedManager := model.User{Username: "hierarchy-archived-manager", Role: common.RoleUserManager, CreatorID: third.Id, Group: "default", AuthVersion: 1, AffCode: "hierarchy-archived-manager"}
	archivedDescendant := model.User{Username: "hierarchy-archived-descendant", Role: common.RoleCommonUser, CreatorID: archivedManager.Id, Group: "default", AuthVersion: 1, AffCode: "hierarchy-archived-descendant"}
	unassignedManager := model.User{Username: "hierarchy-unassigned-manager", Role: common.RoleUserManager, AffCode: "hierarchy-unassigned-manager"}
	unassignedUser := model.User{Username: "hierarchy-unassigned-user", Role: common.RoleCommonUser, AffCode: "hierarchy-unassigned-user"}
	unrelatedUser := model.User{Username: "hierarchy-unrelated", Role: common.RoleCommonUser, Group: "other", AffCode: "hierarchy-unrelated"}
	for _, user := range []*model.User{&secondLevel, &directUser, &archivedManager, &unassignedManager, &unassignedUser, &unrelatedUser} {
		require.NoError(t, db.Create(user).Error)
	}
	grandchild.CreatorID = secondLevel.Id
	require.NoError(t, db.Create(&grandchild).Error)
	archivedDescendant.CreatorID = archivedManager.Id
	require.NoError(t, db.Create(&archivedDescendant).Error)
	require.NoError(t, db.Delete(&archivedManager).Error)

	response := managerRequestWithRoute(t, root, http.MethodGet, "/api/user/", "/api/user/?hierarchy=true", "", GetAllUsers)
	require.Equal(t, http.StatusOK, response.Code)
	var roots struct {
		Data struct {
			Items []model.User `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &roots))
	assert.ElementsMatch(t,
		[]string{root.Username, admin.Username, third.Username, unassignedManager.Username, unassignedUser.Username, unrelatedUser.Username},
		func() []string {
			names := make([]string, len(roots.Data.Items))
			for i := range roots.Data.Items {
				names[i] = roots.Data.Items[i].Username
			}
			return names
		}(),
	)
	rootScope := model.UserManagementScope{OperatorID: root.Id, OperatorRole: root.Role}
	ordinaryRole := common.RoleCommonUser
	filteredRoots, filteredTotal, err := model.FindManagedUserTreeRoots(rootScope, grandchild.Username, "", &ordinaryRole, nil, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, filteredTotal)
	require.Len(t, filteredRoots, 1)
	assert.Equal(t, third.Id, filteredRoots[0].Id)
	assert.Equal(t, []string{"default"}, filteredRoots[0].UserGroups)
	assert.True(t, filteredRoots[0].HierarchyAutoExpand)
	filteredChildren, filteredTotal, err := model.FindManagedUserChildren(rootScope, third.Id, grandchild.Username, "", &ordinaryRole, nil, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, filteredTotal)
	require.Len(t, filteredChildren, 1)
	assert.Equal(t, secondLevel.Id, filteredChildren[0].Id)
	assert.True(t, filteredChildren[0].HierarchyAutoExpand)
	filteredLeaves, filteredTotal, err := model.FindManagedUserChildren(rootScope, secondLevel.Id, grandchild.Username, "", &ordinaryRole, nil, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, filteredTotal)
	require.Len(t, filteredLeaves, 1)
	assert.Equal(t, grandchild.Id, filteredLeaves[0].Id)
	for _, nested := range []struct {
		parent model.User
		want   []string
	}{{third, []string{secondLevel.Username, directUser.Username, archivedManager.Username}}, {secondLevel, []string{grandchild.Username}}, {archivedManager, []string{archivedDescendant.Username}}} {
		response = managerRequestWithRoute(t, root, http.MethodGet, "/api/user/children/:id", fmt.Sprintf("/api/user/children/%d", nested.parent.Id), "", GetManagedUserChildren)
		require.Equal(t, http.StatusOK, response.Code)
		var children struct {
			Data struct {
				Items []model.User `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &children))
		childNames := make([]string, len(children.Data.Items))
		for _, child := range children.Data.Items {
			assert.True(t, child.GroupInherited)
		}
		for i := range children.Data.Items {
			childNames[i] = children.Data.Items[i].Username
		}
		assert.ElementsMatch(t, nested.want, childNames)
	}
	response = managerRequestWithRoute(t, third, http.MethodGet, "/api/user/children/:id", fmt.Sprintf("/api/user/children/%d", secondLevel.Id), "", GetManagedUserChildren)
	require.Equal(t, http.StatusOK, response.Code)
	var hiddenChildren struct {
		Data struct {
			Total int `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &hiddenChildren))
	assert.Zero(t, hiddenChildren.Data.Total)

	response = managerRequest(t, admin, http.MethodPut, "/api/user/", fmt.Sprintf(`{"id":%d,"username":%q,"group":"vip"}`, third.Id, third.Username), UpdateUser)
	require.Contains(t, response.Body.String(), `"success":true`)
	for _, user := range []*model.User{&third, &secondLevel, &directUser, &grandchild} {
		require.NoError(t, db.First(user, user.Id).Error)
		assert.Equal(t, "vip", user.Group, user.Username)
	}
	for _, user := range []*model.User{&archivedManager, &archivedDescendant} {
		require.NoError(t, db.Unscoped().First(user, user.Id).Error)
		assert.Equal(t, "vip", user.Group, user.Username)
	}
	require.NoError(t, db.First(&unrelatedUser, unrelatedUser.Id).Error)
	assert.Equal(t, "other", unrelatedUser.Group)
	assert.EqualValues(t, 2, secondLevel.AuthVersion)
	assert.EqualValues(t, 2, directUser.AuthVersion)
	assert.EqualValues(t, 2, grandchild.AuthVersion)
	assert.EqualValues(t, 2, archivedManager.AuthVersion)
	assert.EqualValues(t, 2, archivedDescendant.AuthVersion)
	response = managerRequest(t, admin, http.MethodPut, "/api/user/", fmt.Sprintf(`{"id":%d,"username":%q}`, third.Id, third.Username), UpdateUser)
	require.Contains(t, response.Body.String(), `"success":true`)
	for _, user := range []*model.User{&third, &secondLevel, &directUser, &grandchild, &archivedManager, &archivedDescendant} {
		require.NoError(t, db.Unscoped().First(user, user.Id).Error)
		assert.Equal(t, "vip", user.Group, user.Username)
	}

	plan := model.SubscriptionPlan{
		Title:          "Inherited group plan",
		DurationUnit:   model.SubscriptionDurationDay,
		DurationValue:  1,
		TotalAmount:    100,
		UpgradeGroup:   "subscription-upgrade",
		DowngradeGroup: "subscription-downgrade",
		Enabled:        true,
	}
	require.NoError(t, db.Create(&plan).Error)
	var subscription *model.UserSubscription
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		subscription, err = model.CreateUserSubscriptionFromPlanTx(tx, secondLevel.Id, &plan, "test")
		return err
	}))
	assert.Empty(t, subscription.UpgradeGroup)
	assert.Empty(t, subscription.DowngradeGroup)
	require.NoError(t, db.Model(subscription).Update("end_time", time.Now().Unix()-1).Error)
	expiredCount, err := model.ExpireDueSubscriptions(10)
	require.NoError(t, err)
	assert.Equal(t, 1, expiredCount)
	require.NoError(t, db.First(&secondLevel, secondLevel.Id).Error)
	assert.Equal(t, "vip", secondLevel.Group, "subscription transitions must preserve the inherited group")

	for _, user := range []*model.User{&secondLevel, &grandchild} {
		response = managerRequest(t, admin, http.MethodPut, "/api/user/", fmt.Sprintf(`{"id":%d,"username":%q,"group":"forged"}`, user.Id, user.Username), UpdateUser)
		assert.Contains(t, response.Body.String(), `"success":false`)
		require.NoError(t, db.First(user, user.Id).Error)
		assert.Equal(t, "vip", user.Group)
	}
}

func TestUserManagerMiddlewareKeepsGlobalAdminAccessClosed(t *testing.T) {
	db := setupManageUserTestDB(t)
	for _, role := range []int{common.RoleCommonUser, common.RoleUserManager, common.RoleAdminUser, common.RoleRootUser} {
		pat := fmt.Sprintf("role-token-%d", role)
		operator := model.User{Username: fmt.Sprintf("role-user-%d", role), Role: role, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &pat, AffCode: fmt.Sprintf("role-%d", role)}
		require.NoError(t, db.Create(&operator).Error)
		router := gin.New()
		router.GET("/management", middleware.UserManagementAuth(), func(c *gin.Context) { common.ApiSuccess(c, nil) })
		router.GET("/global-admin", middleware.AdminAuth(), func(c *gin.Context) { common.ApiSuccess(c, nil) })
		for _, path := range []string{"/management", "/global-admin"} {
			request := httptest.NewRequest("GET", path, nil)
			request.Header.Set("Authorization", "Bearer "+pat)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			allowed := role >= common.RoleAdminUser || (path == "/management" && role == common.RoleUserManager)
			if allowed {
				assert.Equal(t, http.StatusOK, response.Code)
			} else {
				assert.Equal(t, http.StatusForbidden, response.Code)
			}
		}
	}
}

func TestDelegatedUserManagementDoesNotRevealOutOfScopeIDs(t *testing.T) {
	for _, role := range []int{common.RoleUserManager, common.RoleThirdLevelUser} {
		t.Run(fmt.Sprintf("role_%d", role), func(t *testing.T) {
			db := setupManageUserTestDB(t)
			previousBatchUpdateEnabled := common.BatchUpdateEnabled
			common.BatchUpdateEnabled = false
			t.Cleanup(func() { common.BatchUpdateEnabled = previousBatchUpdateEnabled })

			pat := fmt.Sprintf("guessed-id-manager-%d", role)
			manager := model.User{Username: fmt.Sprintf("guessed-manager-%d", role), Role: role, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &pat, AffCode: fmt.Sprintf("guessed-manager-%d", role)}
			require.NoError(t, db.Create(&manager).Error)
			other := model.User{Username: fmt.Sprintf("private-other-%d", role), Role: common.RoleCommonUser, CreatorID: manager.Id + 10, AffCode: fmt.Sprintf("private-other-%d", role)}
			require.NoError(t, db.Create(&other).Error)

			tests := []struct {
				name    string
				method  string
				path    func(int) string
				body    func(int) string
				handler gin.HandlerFunc
			}{
				{name: "detail", method: http.MethodGet, path: func(id int) string { return fmt.Sprintf("/api/user/%d", id) }, handler: GetUser},
				{name: "hard delete", method: http.MethodDelete, path: func(id int) string { return fmt.Sprintf("/api/user/%d", id) }, handler: DeleteUser},
				{name: "update", method: http.MethodPost, path: func(int) string { return "/api/user/" }, body: func(id int) string { return fmt.Sprintf(`{"id":%d,"username":"target","role":1}`, id) }, handler: UpdateUser},
				{name: "enable", method: http.MethodPost, path: func(int) string { return "/api/user/manage" }, body: func(id int) string { return fmt.Sprintf(`{"id":%d,"action":"enable"}`, id) }, handler: ManageUser},
				{name: "soft delete", method: http.MethodPost, path: func(int) string { return "/api/user/manage" }, body: func(id int) string { return fmt.Sprintf(`{"id":%d,"action":"delete"}`, id) }, handler: ManageUser},
				{name: "quota", method: http.MethodPost, path: func(int) string { return "/api/user/manage" }, body: func(id int) string { return fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":1}`, id) }, handler: ManageUser},
			}

			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					perform := func(id int) *httptest.ResponseRecorder {
						router := gin.New()
						router.Handle(test.method, test.path(id), middleware.UserManagementAuth(), test.handler)
						var body *strings.Reader
						if test.body != nil {
							body = strings.NewReader(test.body(id))
						} else {
							body = strings.NewReader("")
						}
						request := httptest.NewRequest(test.method, test.path(id), body)
						request.Header.Set("Authorization", "Bearer "+pat)
						request.Header.Set("Content-Type", "application/json")
						response := httptest.NewRecorder()
						router.ServeHTTP(response, request)
						return response
					}

					outOfScope := perform(other.Id)
					missing := perform(other.Id + 1000)
					assert.Contains(t, outOfScope.Body.String(), `"success":false`)
					assert.JSONEq(t, outOfScope.Body.String(), missing.Body.String())
					assert.NotContains(t, outOfScope.Body.String(), other.Username)
				})
			}

			var count int64
			require.NoError(t, db.Model(&model.User{}).Where("id = ?", other.Id).Count(&count).Error)
			assert.EqualValues(t, 1, count)
		})
	}
}

func TestUserManagerRootHardDeletionRemainsForbidden(t *testing.T) {
	db := setupManageUserTestDB(t)
	pat := "root-delete-regression"
	root := model.User{Username: "root-delete", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &pat, AffCode: "root-delete"}
	require.NoError(t, db.Create(&root).Error)
	router := gin.New()
	router.DELETE("/api/user/:id", middleware.UserManagementAuth(), DeleteUser)
	request := httptest.NewRequest("DELETE", fmt.Sprintf("/api/user/%d", root.Id), nil)
	request.Header.Set("Authorization", "Bearer "+pat)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Contains(t, response.Body.String(), `"success":false`)
	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", root.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestManagedUsersWithChildrenCannotBeDeletedOrDemoted(t *testing.T) {
	for _, role := range []int{common.RoleUserManager, common.RoleThirdLevelUser} {
		for _, method := range []string{"hard delete", "soft delete", "direct hard delete", "demote"} {
			t.Run(fmt.Sprintf("role_%d_%s", role, strings.ReplaceAll(method, " ", "_")), func(t *testing.T) {
				db := setupManageUserTestDB(t)
				adminToken := fmt.Sprintf("delete-admin-%d-%d", role, len(method))
				admin := model.User{Username: fmt.Sprintf("managed-delete-admin-%d", role), Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &adminToken, AffCode: fmt.Sprintf("managed-delete-admin-%d", role)}
				parent := model.User{Username: fmt.Sprintf("managed-delete-parent-%d", role), Role: role, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: fmt.Sprintf("managed-delete-parent-%d", role)}
				child := model.User{Username: fmt.Sprintf("managed-delete-child-%d", role), Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: fmt.Sprintf("managed-delete-child-%d", role)}
				require.NoError(t, db.Create(&admin).Error)
				require.NoError(t, db.Create(&parent).Error)
				child.CreatorID = parent.Id
				require.NoError(t, db.Create(&child).Error)
				if method != "demote" {
					require.NoError(t, db.Delete(&child).Error)
				}

				switch method {
				case "hard delete":
					response := managerRequestWithRoute(t, admin, http.MethodDelete, "/api/user/:id", fmt.Sprintf("/api/user/%d", parent.Id), "", DeleteUser)
					assert.Contains(t, response.Body.String(), `"success":false`)
				case "soft delete":
					response := managerRequest(t, admin, http.MethodPost, "/api/user/manage", fmt.Sprintf(`{"id":%d,"action":"delete"}`, parent.Id), ManageUser)
					assert.Contains(t, response.Body.String(), `"success":false`)
				case "direct hard delete":
					assert.ErrorIs(t, model.HardDeleteUserById(parent.Id), model.ErrManagedUserHasChildren)
				case "demote":
					response := managerRequest(t, admin, http.MethodPost, "/api/user/manage", fmt.Sprintf(`{"id":%d,"action":"demote"}`, parent.Id), ManageUser)
					assert.Contains(t, response.Body.String(), `"success":false`)
				}

				var parentCount int64
				require.NoError(t, db.Model(&model.User{}).Where("id = ?", parent.Id).Count(&parentCount).Error)
				assert.EqualValues(t, 1, parentCount)
				if method == "demote" {
					require.NoError(t, db.First(&parent, parent.Id).Error)
					assert.Equal(t, role, parent.Role)
				}
			})
		}
	}
}

func TestUserManagerQuotaConcurrentTransfersCannotDoubleSpend(t *testing.T) {
	db := setupManageUserTestDB(t)
	// A lower child ID also covers transfers where the manager is not the
	// first row in the global ascending lock order.
	first := model.User{Username: "first-wallet", Role: common.RoleCommonUser, AffCode: "first-wallet"}
	require.NoError(t, db.Create(&first).Error)
	manager := model.User{Username: "shared-wallet", Role: common.RoleUserManager, Quota: 1000, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "shared-wallet"}
	require.NoError(t, db.Create(&manager).Error)
	require.NoError(t, db.Model(&first).Update("creator_id", manager.Id).Error)
	second := model.User{Username: "second-wallet", Role: common.RoleCommonUser, CreatorID: manager.Id, AffCode: "second-wallet"}
	require.NoError(t, db.Create(&second).Error)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []int{first.Id, second.Id} {
		go func() {
			<-start
			_, err := model.TransferManagedUserQuota(id, manager.Id, manager.Role, "add", 750)
			results <- err
		}()
	}
	close(start)
	successes := 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, model.ErrQuotaTransferInsufficient) {
			require.True(t, common.UsingMainDatabase(common.DatabaseTypeSQLite), "row-locking databases must serialize transfers: %v", err)
			assert.Contains(t, strings.ToLower(err.Error()), "locked")
		}
	}
	assert.Equal(t, 1, successes)
	require.NoError(t, db.First(&manager, manager.Id).Error)
	require.NoError(t, db.First(&first, first.Id).Error)
	require.NoError(t, db.First(&second, second.Id).Error)
	assert.Equal(t, 250, manager.Quota)
	assert.Equal(t, 750, first.Quota+second.Quota)
}

func TestUserManagerQuotaAuditRetainsActorAndBothWalletSnapshots(t *testing.T) {
	db := setupManageUserTestDB(t)
	pat := "manager-audit-token"
	manager := model.User{Username: "audit-manager", Role: common.RoleUserManager, Quota: 1000, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &pat, AffCode: "audit-manager"}
	require.NoError(t, db.Create(&manager).Error)
	child := model.User{Username: "audit-child", Role: common.RoleCommonUser, CreatorID: manager.Id, AffCode: "audit-child"}
	require.NoError(t, db.Create(&child).Error)
	response := managerRequest(t, manager, "POST", "/api/user/manage", fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":250}`, child.Id), ManageUser)
	require.Contains(t, response.Body.String(), `"success":true`)
	logs, total, err := model.GetAuditLogs(model.AuditLogFilter{UserId: manager.Id, Category: model.AuditCategoryOperation}, 0, 20, common.RoleAdminUser)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, logs, 1)
	assert.Equal(t, common.RoleUserManager, logs[0].ActorRole)
	assert.True(t, logs[0].Success)
	params, err := common.Marshal(logs[0].Other.Op.Params)
	require.NoError(t, err)
	assert.Contains(t, string(params), `"manager_from":1000`)
	assert.Contains(t, string(params), `"manager_to":750`)
	assert.Contains(t, string(params), `"from":0`)
	assert.Contains(t, string(params), `"to":250`)
	var entries []model.Log
	require.NoError(t, model.LOG_DB.Order("id ASC").Find(&entries).Error)
	require.Len(t, entries, 2, "both participants receive a transfer record")
	assert.ElementsMatch(t, []int{manager.Id, child.Id}, []int{entries[0].UserId, entries[1].UserId})
}

func TestUserManagerQuotaRejectsUnsynchronizedBatchWithoutRedis(t *testing.T) {
	db := setupManageUserTestDB(t)
	manager := model.User{Username: "batch-manager", Role: common.RoleUserManager, Quota: 1000, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "batch-manager"}
	require.NoError(t, db.Create(&manager).Error)
	child := model.User{Username: "batch-child", Role: common.RoleCommonUser, CreatorID: manager.Id, AffCode: "batch-child"}
	require.NoError(t, db.Create(&child).Error)
	previousBatch := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() { common.BatchUpdateEnabled = previousBatch })
	adjustment, err := model.TransferManagedUserQuota(child.Id, manager.Id, manager.Role, "add", 100)
	assert.ErrorIs(t, err, model.ErrQuotaTransferUnavailable)
	assert.Nil(t, adjustment)
	require.NoError(t, db.First(&manager, manager.Id).Error)
	require.NoError(t, db.First(&child, child.Id).Error)
	assert.Equal(t, 1000, manager.Quota)
	assert.Zero(t, child.Quota)
}
