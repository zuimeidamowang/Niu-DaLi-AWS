package model

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSubscriptionGroupTransitionsInvalidateSessionsWhenAssignmentsChange(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	now := time.Now().Unix()
	user := User{
		Username:    "subscription-auth-user",
		Password:    "unused-password-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AffCode:     "subscription-auth-user",
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, CreateUserSession(&UserSession{
		SID:             "subscription-auth-session",
		UserID:          user.Id,
		Version:         1,
		UserAuthVersion: 1,
		Status:          UserSessionStatusActive,
		RefreshHash:     "refresh-hash",
		LoginMethod:     "password",
		LastActiveAt:    now,
		ExpiresAt:       now + 3600,
	}))
	require.NoError(t, populateUserCache(user))
	plan := &SubscriptionPlan{
		Title:         "Upgraded",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   100,
		UpgradeGroup:  "pro",
		Enabled:       true,
	}
	require.NoError(t, DB.Create(plan).Error)

	subscription, err := CreateUserSubscriptionFromPlanTx(DB, user.Id, plan, "test")
	require.NoError(t, err)
	require.Equal(t, "default", subscription.PrevUserGroup)
	finalizeSubscriptionGroupAccessChanges(subscription.groupAccessChangedIDs, "subscription test upgrade")

	var updated User
	require.NoError(t, DB.First(&updated, user.Id).Error)
	assert.Equal(t, "pro", updated.Group)
	assert.EqualValues(t, 2, updated.AuthVersion)
	assignedGroups, err := GetUserGroupAssignments(user.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"pro"}, assignedGroups)
	var session UserSession
	require.NoError(t, DB.First(&session, "sid = ?", "subscription-auth-session").Error)
	assert.Equal(t, UserSessionStatusRevoked, session.Status)
	cached, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, "pro", cached.Group)
	assert.Equal(t, []string{"pro"}, cached.UserGroups)
	assert.EqualValues(t, 2, cached.AuthVersion)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		target, err := downgradeUserGroupForSubscriptionTx(tx, subscription, now+1)
		assert.Equal(t, "default", target)
		return err
	}))
	finalizeSubscriptionGroupAccessChanges(subscription.groupAccessChangedIDs, "subscription test downgrade")
	require.NoError(t, DB.First(&updated, user.Id).Error)
	assert.Equal(t, "default", updated.Group)
	assert.EqualValues(t, 3, updated.AuthVersion)
	assignedGroups, err = GetUserGroupAssignments(user.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, assignedGroups)
	require.NoError(t, DB.First(&session, "sid = ?", "subscription-auth-session").Error)
	assert.Equal(t, UserSessionStatusRevoked, session.Status)
	cached, err = GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, "default", cached.Group)
	assert.Equal(t, []string{"default"}, cached.UserGroups)

	multiGroupUser := User{
		Username:    "subscription-multi-group-user",
		Password:    "unused-password-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AffCode:     "subscription-multi-group-user",
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&multiGroupUser).Error)
	require.NoError(t, ReplaceUserGroupAssignmentsTx(DB, multiGroupUser.Id, []string{"default", "vip"}))
	multiSubscription, err := CreateUserSubscriptionFromPlanTx(DB, multiGroupUser.Id, plan, "test")
	require.NoError(t, err)
	require.NoError(t, RefreshUserGroupCache(multiGroupUser.Id))
	require.NoError(t, DB.First(&multiGroupUser, multiGroupUser.Id).Error)
	assert.Equal(t, "pro", multiGroupUser.Group)
	assignedGroups, err = GetUserGroupAssignments(multiGroupUser.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"default", "vip"}, assignedGroups)
	multiCached, err := GetUserCache(multiGroupUser.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"default", "vip"}, multiCached.UserGroups)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		_, err := downgradeUserGroupForSubscriptionTx(tx, multiSubscription, now+1)
		return err
	}))
	assignedGroups, err = GetUserGroupAssignments(multiGroupUser.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"default", "vip"}, assignedGroups)
}

func TestSubscriptionGroupCacheRefreshFailureDoesNotChangeCommittedResult(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&User{}, &UserGroupAssignment{}, &SubscriptionPlan{}, &UserSubscription{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		_ = sqlDB.Close()
	})

	user := User{
		Username:    "subscription-cache-failure",
		Password:    "unused-password-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AffCode:     "subscription-cache-failure",
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, ReplaceUserGroupAssignmentsTx(DB, user.Id, []string{"default", "vip"}))
	plan := &SubscriptionPlan{
		Title:         "Cache failure plan",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   100,
		UpgradeGroup:  "pro",
		Enabled:       true,
	}
	require.NoError(t, DB.Create(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)

	oldRedisEnabled, oldRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("forced redis failure")
		},
		MaxRetries: -1,
	})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled, common.RDB = oldRedisEnabled, oldRDB
	})

	message, err := AdminBindSubscription(user.Id, plan.Id, "test")
	require.NoError(t, err)
	assert.Contains(t, message, "pro")

	var updated User
	require.NoError(t, DB.First(&updated, user.Id).Error)
	assert.Equal(t, "pro", updated.Group)
	assert.EqualValues(t, 1, updated.AuthVersion)
	var subscription UserSubscription
	require.NoError(t, DB.Where("user_id = ?", user.Id).First(&subscription).Error)
	assert.Equal(t, "active", subscription.Status)
}

func TestAdminBindSubscriptionRefreshesAndRevokesChangedDescendantAccess(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	parent := User{
		Username: "subscription-admin-bind-third-level",
		Role:     common.RoleThirdLevelUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "subscription-admin-bind-third-level",
	}
	require.NoError(t, DB.Create(&parent).Error)
	manager := User{
		Username:  "subscription-admin-bind-manager",
		Role:      common.RoleUserManager,
		Status:    common.UserStatusEnabled,
		Group:     "default",
		CreatorID: parent.Id,
		AffCode:   "subscription-admin-bind-manager",
	}
	require.NoError(t, DB.Create(&manager).Error)
	child := User{
		Username:  "subscription-admin-bind-child",
		Role:      common.RoleCommonUser,
		Status:    common.UserStatusEnabled,
		Group:     "default",
		CreatorID: manager.Id,
		AffCode:   "subscription-admin-bind-child",
	}
	require.NoError(t, DB.Create(&child).Error)
	for _, user := range []User{manager, child} {
		require.NoError(t, DB.Create(&UserSession{
			SID:             user.Username + "-session",
			UserID:          user.Id,
			Version:         1,
			UserAuthVersion: 1,
			Status:          UserSessionStatusActive,
			RefreshHash:     user.Username + "-refresh-hash",
			LoginMethod:     "password",
			LastActiveAt:    time.Now().Unix(),
			ExpiresAt:       time.Now().Add(time.Hour).Unix(),
		}).Error)
	}

	for _, userID := range []int{parent.Id, manager.Id, child.Id} {
		_, err := GetUserCache(userID)
		require.NoError(t, err)
	}
	plan := &SubscriptionPlan{
		Title:         "Admin bind inherited group plan",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   100,
		UpgradeGroup:  "pro",
		Enabled:       true,
	}
	require.NoError(t, DB.Create(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)

	message, err := AdminBindSubscription(parent.Id, plan.Id, "test")
	require.NoError(t, err)
	assert.Contains(t, message, "pro")

	for _, expected := range []struct {
		user *User
		want string
	}{{&parent, "pro"}, {&manager, "pro"}, {&child, "pro"}} {
		var stored User
		require.NoError(t, DB.First(&stored, expected.user.Id).Error)
		assert.Equal(t, expected.want, stored.Group)
		cached, err := GetUserCache(expected.user.Id)
		require.NoError(t, err)
		assert.Equal(t, expected.want, cached.Group)
	}

	for _, userID := range []int{manager.Id, child.Id} {
		groups, err := GetUserGroupAssignments(userID)
		require.NoError(t, err)
		assert.Empty(t, groups)
		cached, err := GetUserCache(userID)
		require.NoError(t, err)
		assert.Empty(t, cached.UserGroups)
		var session UserSession
		require.NoError(t, DB.First(&session, "user_id = ?", userID).Error)
		assert.Equal(t, UserSessionStatusRevoked, session.Status)
	}
}

func TestExpiredSubscriptionsForHardDeletedUsersDoNotBlockExpiration(t *testing.T) {
	truncateTables(t)
	now := GetDBTimestamp()
	activeUser := User{
		Username:    "subscription-expiration-active-user",
		Password:    "unused-password-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "pro",
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&activeUser).Error)
	for _, subscription := range []*UserSubscription{
		{
			UserId:         activeUser.Id,
			Status:         "active",
			EndTime:        now - 1,
			UpgradeGroup:   "pro",
			PrevUserGroup:  "default",
			DowngradeGroup: "default",
		},
		{
			UserId:         activeUser.Id + 1,
			Status:         "active",
			EndTime:        now - 2,
			UpgradeGroup:   "pro",
			PrevUserGroup:  "default",
			DowngradeGroup: "default",
		},
	} {
		require.NoError(t, DB.Create(subscription).Error)
	}

	expiredCount, err := ExpireDueSubscriptions(100)

	require.NoError(t, err)
	assert.Equal(t, 2, expiredCount)
	var updated User
	require.NoError(t, DB.First(&updated, activeUser.Id).Error)
	assert.Equal(t, "default", updated.Group)
	assignedGroups, err := GetUserGroupAssignments(activeUser.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, assignedGroups)
	var expiredSubscriptions []UserSubscription
	require.NoError(t, DB.Order("user_id asc").Find(&expiredSubscriptions).Error)
	require.Len(t, expiredSubscriptions, 2)
	assert.Equal(t, "expired", expiredSubscriptions[0].Status)
	assert.Equal(t, "expired", expiredSubscriptions[1].Status)
}

func TestDowngradeSubscriptionForHardDeletedUserIsSkipped(t *testing.T) {
	truncateTables(t)
	subscription := &UserSubscription{
		UserId:         999999,
		Status:         "expired",
		UpgradeGroup:   "pro",
		PrevUserGroup:  "default",
		DowngradeGroup: "default",
	}

	target, err := downgradeUserGroupForSubscriptionTx(DB, subscription, GetDBTimestamp())

	require.NoError(t, err)
	assert.Empty(t, target)
}
