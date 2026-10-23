package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetUserGroupsAnonymousReturnsGlobalGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalGroups := setting.UserUsableGroups2JSONString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2}`))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/user/groups", nil)
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	GetUserGroups(context)

	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"success":true`)
	require.Contains(t, response.Body.String(), `"default"`)
	require.Contains(t, response.Body.String(), `"vip"`)
}
