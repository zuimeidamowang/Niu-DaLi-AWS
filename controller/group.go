package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func userBillingGroupForDisplay(userGroup string, userGroups []string, channelGroup string) string {
	if userGroup != "" && userGroups != nil &&
		(len(userGroups) != 1 || userGroups[0] != userGroup) {
		return userGroup
	}
	return channelGroup
}

func GetGroups(c *gin.Context) {
	groupNames := make([]string, 0)
	for groupName := range ratio_setting.GetGroupRatioCopy() {
		groupNames = append(groupNames, groupName)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    groupNames,
	})
}

func GetUserGroupNames(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success":              true,
		"message":              "",
		"data":                 service.GetUserGroupNames(),
		"legacy_compatibility": !setting.UserGroupsConfigured(),
	})
}

func GetUserGroups(c *gin.Context) {
	usableGroups := make(map[string]map[string]any)
	userId := c.GetInt("id")
	userGroup := ""
	var userGroups []string
	var userUsableGroups map[string]string
	if userId == 0 {
		// This endpoint remains public for login and registration flows.
		userUsableGroups = service.GetUserUsableGroups(userGroup)
	} else {
		user, err := model.GetUserCache(userId)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		userGroup = user.Group
		userGroups = user.UserGroups
		userUsableGroups = service.GetUserUsableGroupsForUser(userGroup, user.UserGroups)
	}
	for groupName, _ := range ratio_setting.GetGroupRatioCopy() {
		// UserUsableGroups contains the groups that the user can use
		if desc, ok := userUsableGroups[groupName]; ok {
			usableGroups[groupName] = map[string]any{
				"ratio": service.GetUserGroupRatio(userGroup, userBillingGroupForDisplay(userGroup, userGroups, groupName)),
				"desc":  desc,
			}
		}
	}
	if _, ok := userUsableGroups["auto"]; ok {
		usableGroups["auto"] = map[string]any{
			"ratio": "自动",
			"desc":  setting.GetUsableGroupDescription("auto"),
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    usableGroups,
	})
}
