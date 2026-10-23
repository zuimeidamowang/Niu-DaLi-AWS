package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func configureTokenAutoGroupsTest(t *testing.T, maxCount string, autoGroups string) {
	t.Helper()
	originalMax := setting.GetMaxTokenAutoGroups()
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, setting.UpdateMaxTokenAutoGroups(maxCount))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(autoGroups))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateMaxTokenAutoGroups(stringInt(originalMax)))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
	})
}

func stringInt(value int) string {
	return fmt.Sprintf("%d", value)
}

func setupTokenAutoGroupsControllerTest(t *testing.T) *model.User {
	t.Helper()
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserGroupAssignment{}))
	user := &model.User{
		Id:       101,
		Username: "token-auto-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)
	return user
}

func baseAutoTokenRequest(name string) map[string]any {
	return map[string]any{
		"name":              name,
		"expired_time":      -1,
		"remain_quota":      0,
		"unlimited_quota":   true,
		"group":             "auto",
		"cross_group_retry": true,
	}
}

func newTokenAutoGroupsAuthenticatedContext(t *testing.T, method string, target string, body any, userID int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	ctx, recorder := newAuthenticatedContext(t, method, target, body, userID)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	return ctx, recorder
}

func TestAddTokenSupportsAutoGroupInheritanceAndCustomOrder(t *testing.T) {
	tests := []struct {
		name               string
		includeGroups      bool
		expectedAutoGroups string
	}{
		{name: "inherits global order"},
		{name: "persists custom order", includeGroups: true, expectedAutoGroups: `["vip","default"]`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureTokenAutoGroupsTest(t, "5", `["default","vip"]`)
			user := setupTokenAutoGroupsControllerTest(t)
			request := baseAutoTokenRequest("create-auto")
			if test.includeGroups {
				request["auto_groups"] = []string{"vip", "default"}
			}

			ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", request, user.Id)
			AddToken(ctx)

			require.True(t, decodeAPIResponse(t, recorder).Success)
			var token model.Token
			require.NoError(t, model.DB.Where("name = ?", request["name"]).First(&token).Error)
			assert.Equal(t, "auto", token.Group)
			assert.True(t, token.CrossGroupRetry)
			if test.expectedAutoGroups == "" {
				assert.Empty(t, token.AutoGroups)
			} else {
				assert.JSONEq(t, test.expectedAutoGroups, token.AutoGroups)
			}
		})
	}
}

func TestUpdateTokenAutoGroupsTriStateAndNonAutoCleanup(t *testing.T) {
	tests := []struct {
		name               string
		includeField       bool
		value              any
		group              string
		expectedAutoGroups string
		expectedRetry      bool
	}{
		{name: "omitted preserves", group: "auto", expectedAutoGroups: `["vip","default"]`, expectedRetry: true},
		{name: "null inherits", includeField: true, value: nil, group: "auto", expectedRetry: true},
		{name: "empty inherits", includeField: true, value: []string{}, group: "auto", expectedRetry: true},
		{name: "non auto clears and disables retry", includeField: true, value: []string{"vip"}, group: "default"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureTokenAutoGroupsTest(t, "5", `["default","vip"]`)
			user := setupTokenAutoGroupsControllerTest(t)
			token := seedToken(t, model.DB, user.Id, "update-auto", "update-auto-key")
			token.Group = "auto"
			token.CrossGroupRetry = true
			require.NoError(t, token.SetAutoGroups([]string{"vip", "default"}))
			require.NoError(t, model.DB.Save(token).Error)

			request := baseAutoTokenRequest("updated-auto")
			request["id"] = token.Id
			request["status"] = common.TokenStatusEnabled
			request["group"] = test.group
			if test.includeField {
				request["auto_groups"] = test.value
			}
			ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPut, "/api/token/", request, user.Id)
			UpdateToken(ctx)
			response := decodeAPIResponse(t, recorder)
			require.True(t, response.Success, response.Message)

			var updated model.Token
			require.NoError(t, model.DB.First(&updated, token.Id).Error)
			if test.expectedAutoGroups == "" {
				assert.Empty(t, updated.AutoGroups)
			} else {
				assert.JSONEq(t, test.expectedAutoGroups, updated.AutoGroups)
			}
			assert.Equal(t, test.expectedRetry, updated.CrossGroupRetry)
		})
	}
}

func TestUpdateTokenCanSwitchToAutoGroup(t *testing.T) {
	configureTokenAutoGroupsTest(t, "5", `["default","vip"]`)
	user := setupTokenAutoGroupsControllerTest(t)
	token := seedToken(t, model.DB, user.Id, "channel-key", "channel-key-value")
	token.Group = "default"
	require.NoError(t, model.DB.Save(token).Error)

	request := map[string]any{
		"id":                token.Id,
		"name":              token.Name,
		"expired_time":      -1,
		"remain_quota":      0,
		"unlimited_quota":   true,
		"status":            common.TokenStatusEnabled,
		"group":             "auto",
		"cross_group_retry": true,
		"auto_groups":       []string{"vip"},
	}
	ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPut, "/api/token/", request, user.Id)
	UpdateToken(ctx)

	require.True(t, decodeAPIResponse(t, recorder).Success)
	var updated model.Token
	require.NoError(t, model.DB.First(&updated, token.Id).Error)
	assert.Equal(t, "auto", updated.Group)
	assert.True(t, updated.CrossGroupRetry)
	assert.JSONEq(t, `["vip"]`, updated.AutoGroups)
}

func TestUpdateTokenRejectsInvalidAutoGroups(t *testing.T) {
	tests := []struct {
		name     string
		maxCount string
		groups   []string
	}{
		{name: "over limit", maxCount: "1", groups: []string{"default", "vip"}},
		{name: "duplicate", maxCount: "5", groups: []string{"default", "default"}},
		{name: "auto pseudo group", maxCount: "5", groups: []string{"auto"}},
		{name: "unavailable", maxCount: "5", groups: []string{"missing"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureTokenAutoGroupsTest(t, test.maxCount, `["default","vip"]`)
			user := setupTokenAutoGroupsControllerTest(t)
			token := seedToken(t, model.DB, user.Id, "legacy-auto", "legacy-auto-key")
			token.Group = "auto"
			token.CrossGroupRetry = true
			require.NoError(t, token.SetAutoGroups([]string{"vip"}))
			require.NoError(t, model.DB.Save(token).Error)

			request := baseAutoTokenRequest("legacy-auto")
			request["id"] = token.Id
			request["status"] = common.TokenStatusEnabled
			request["auto_groups"] = test.groups

			ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPut, "/api/token/", request, user.Id)
			UpdateToken(ctx)

			response := decodeAPIResponse(t, recorder)
			assert.False(t, response.Success)
			var unchanged model.Token
			require.NoError(t, model.DB.First(&unchanged, token.Id).Error)
			assert.JSONEq(t, `["vip"]`, unchanged.AutoGroups)
		})
	}
}

func TestAddTokenAllowsVisibleChannelGroupAndDefaultsEmptyGroup(t *testing.T) {
	tests := []struct {
		name  string
		group string
	}{
		{name: "explicit default", group: "default"},
		{name: "empty group defaults to default"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureTokenAutoGroupsTest(t, "5", `[]`)
			user := setupTokenAutoGroupsControllerTest(t)
			request := map[string]any{
				"name":            "visible-channel-group",
				"expired_time":    -1,
				"remain_quota":    0,
				"unlimited_quota": true,
				"group":           test.group,
			}

			ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", request, user.Id)
			AddToken(ctx)

			require.True(t, decodeAPIResponse(t, recorder).Success)
			var token model.Token
			require.NoError(t, model.DB.Where("name = ?", request["name"]).First(&token).Error)
			assert.Equal(t, "default", token.Group)
			assert.False(t, token.CrossGroupRetry)
		})
	}
}

func TestAddTokenRejectsChannelGroupsNotVisibleToUserGroup(t *testing.T) {
	t.Run("globally hidden channel group", func(t *testing.T) {
		configureTokenAutoGroupsTest(t, "5", `[]`)
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
		user := setupTokenAutoGroupsControllerTest(t)
		request := map[string]any{
			"name":            "hidden-channel-group",
			"expired_time":    -1,
			"remain_quota":    0,
			"unlimited_quota": true,
			"group":           "vip",
		}

		ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", request, user.Id)
		AddToken(ctx)

		assert.False(t, decodeAPIResponse(t, recorder).Success)
		var count int64
		require.NoError(t, model.DB.Model(&model.Token{}).Where("user_id = ?", user.Id).Count(&count).Error)
		assert.Zero(t, count)
	})

	t.Run("explicitly hidden default channel group", func(t *testing.T) {
		configureTokenAutoGroupsTest(t, "5", `[]`)
		specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
		originalSpecialGroups := specialGroups.ReadAll()
		t.Cleanup(func() {
			specialGroups.Clear()
			specialGroups.AddAll(originalSpecialGroups)
		})
		specialGroups.Clear()
		specialGroups.Set("default", map[string]string{"-:default": ""})
		user := setupTokenAutoGroupsControllerTest(t)
		request := map[string]any{
			"name":            "hidden-default-group",
			"expired_time":    -1,
			"remain_quota":    0,
			"unlimited_quota": true,
			"group":           "default",
		}

		ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", request, user.Id)
		AddToken(ctx)

		assert.False(t, decodeAPIResponse(t, recorder).Success)
	})
}

func TestGetTokenAutoGroupsReturnsFullFilteredGlobalOrderAndLimit(t *testing.T) {
	configureTokenAutoGroupsTest(t, "1", `["vip","missing","default"]`)
	user := setupTokenAutoGroupsControllerTest(t)

	ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodGet, "/api/token/auto-groups", nil, user.Id)
	GetTokenAutoGroups(ctx)

	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	var data struct {
		Groups   []string `json:"groups"`
		MaxCount int      `json:"max_count"`
	}
	require.NoError(t, common.Unmarshal(response.Data, &data))
	assert.Equal(t, []string{"vip", "default"}, data.Groups)
	assert.Equal(t, 1, data.MaxCount)
}
