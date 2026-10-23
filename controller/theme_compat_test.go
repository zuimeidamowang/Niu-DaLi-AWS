package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateOptionRejectsRetiredFrontendTheme(t *testing.T) {
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/option/",
		strings.NewReader(`{"key":"theme.frontend","value":"classic"}`),
	)

	UpdateOption(context)

	assert.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, `{"success":false,"message":"Classic 前端已移除，主题只能设置为 default"}`, response.Body.String())
}

func TestGetStatusAdvertisesDefaultDashboard(t *testing.T) {
	previousMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousMap })
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)

	GetStatus(context)

	var payload struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.True(t, payload.Success)
	assert.Equal(t, "default", payload.Data["theme"])
}

func TestGetStatusUsesPublicDomainForConfiguredIPAddress(t *testing.T) {
	consoleSettings := console_setting.GetConsoleSetting()
	previousAPIInfo, previousAPIInfoEnabled := consoleSettings.ApiInfo, consoleSettings.ApiInfoEnabled
	previousMap := common.OptionMap
	previousServerAddress := system_setting.ServerAddress
	generalSetting := operation_setting.GetGeneralSetting()
	previousCanvasLink := generalSetting.CanvasLink
	apiInfo, err := common.Marshal([]map[string]any{{
		"route":       "Global",
		"description": "Primary API",
		"url":         "https://13.112.137.141/v1",
		"color":       "blue",
	}})
	require.NoError(t, err)
	consoleSettings.ApiInfo = string(apiInfo)
	consoleSettings.ApiInfoEnabled = true
	common.OptionMap = map[string]string{}
	system_setting.ServerAddress = "http://13.112.137.141"
	generalSetting.CanvasLink = "http://8.216.57.93/canvas/"
	t.Cleanup(func() {
		consoleSettings.ApiInfo = previousAPIInfo
		consoleSettings.ApiInfoEnabled = previousAPIInfoEnabled
		common.OptionMap = previousMap
		system_setting.ServerAddress = previousServerAddress
		generalSetting.CanvasLink = previousCanvasLink
	})

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	GetStatus(context)

	var payload struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	assert.Equal(t, "https://www.daliniuai.com", payload.Data["server_address"])
	assert.Equal(t, "https://www.daliniuai.com/canvas/", payload.Data["canvas_link"])
	apiInfoResponse, ok := payload.Data["api_info"].([]any)
	require.True(t, ok)
	require.Len(t, apiInfoResponse, 1)
	apiItem, ok := apiInfoResponse[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://www.daliniuai.com/v1", apiItem["url"])
}

func TestReplaceIPAddressWithPublicDomainPreservesConfiguredHostname(t *testing.T) {
	assert.Equal(t, "https://gateway.example.com/proxy/", replaceIPAddressWithPublicDomain("https://gateway.example.com/proxy/"))
	assert.Equal(t, "http://127.0.0.1:3000", replaceIPAddressWithPublicDomain("http://127.0.0.1:3000"))
}
