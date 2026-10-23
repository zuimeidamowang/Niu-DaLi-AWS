package service

import (
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func GetUserUsableGroups(userGroup string) map[string]string {
	return getUserUsableGroupsForSource(userGroup)
}

func getUserUsableGroupsForSource(userGroup string) map[string]string {
	groupsCopy := setting.GetUserUsableGroupsCopy()
	if ratio_setting.ContainsGroupRatio("default") {
		if _, ok := groupsCopy["default"]; !ok {
			groupsCopy["default"] = setting.GetUsableGroupDescription("default")
		}
	}
	if !setting.UserGroupsConfigured() && userGroup != "" && ratio_setting.ContainsGroupRatio(userGroup) {
		if _, ok := groupsCopy[userGroup]; !ok {
			groupsCopy[userGroup] = "用户分组"
		}
	}
	if userGroup != "" {
		specialSettings, b := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
		if b {
			// 处理特殊可用分组
			for specialGroup, desc := range specialSettings {
				if after, ok := strings.CutPrefix(specialGroup, "-:"); ok {
					// 移除分组
					groupToRemove := after
					delete(groupsCopy, groupToRemove)
				} else if after, ok := strings.CutPrefix(specialGroup, "+:"); ok {
					// 添加分组
					groupToAdd := after
					groupsCopy[groupToAdd] = desc
				} else {
					// 直接添加分组
					groupsCopy[specialGroup] = desc
				}
			}
		}
	}
	return groupsCopy
}

func normalizeUserGroupSources(userGroup string, userGroups []string) []string {
	seen := make(map[string]struct{}, len(userGroups))
	sources := make([]string, 0, len(userGroups))
	for _, group := range userGroups {
		if group == "" || group == "auto" {
			continue
		}
		if _, exists := seen[group]; exists {
			continue
		}
		seen[group] = struct{}{}
		sources = append(sources, group)
	}
	if userGroups == nil {
		sources = append(sources, userGroup)
	}
	slices.Sort(sources)
	return sources
}

// GetUserUsableGroupsForUser unions the channel groups granted by each user
// group assignment. Special exclusions apply only to the source assignment
// that declares them, so another assignment can independently grant a group.
func GetUserUsableGroupsForUser(userGroup string, userGroups []string) map[string]string {
	usableGroups := make(map[string]string)
	for _, source := range normalizeUserGroupSources(userGroup, userGroups) {
		for group, description := range getUserUsableGroupsForSource(source) {
			if _, exists := usableGroups[group]; !exists {
				usableGroups[group] = description
			}
		}
	}
	return usableGroups
}

// GetUserChannelGroupsForUser returns only routable channel groups. Auto is a
// selector for expanding configured groups, not a channel group itself.
func GetUserChannelGroupsForUser(userGroup string, userGroups []string) map[string]string {
	channelGroups := GetUserUsableGroupsForUser(userGroup, userGroups)
	for group := range channelGroups {
		if group == "auto" || !ratio_setting.ContainsGroupRatio(group) {
			delete(channelGroups, group)
		}
	}
	return channelGroups
}

// GetRequestUserGroups returns the authorization sources attached by auth
// middleware. The single group fallback keeps non-authenticated test and
// internal contexts compatible with their existing behavior.
func GetRequestUserGroups(c *gin.Context, userGroup string) []string {
	if groups, ok := common.GetContextKeyType[[]string](c, constant.ContextKeyUserGroups); ok {
		return groups
	}
	return normalizeUserGroupSources(userGroup, nil)
}

func GroupInUserUsableGroups(userGroup, groupName string) bool {
	_, ok := GetUserUsableGroups(userGroup)[groupName]
	return ok
}

func GroupInUserUsableGroupsForUser(userGroup, groupName string, userGroups []string) bool {
	_, ok := GetUserUsableGroupsForUser(userGroup, userGroups)[groupName]
	return ok
}

func IsUserSelectableGroup(userGroup, groupName string) bool {
	return IsUserSelectableGroupForUser(userGroup, groupName, nil)
}

func IsUserSelectableGroupForUser(userGroup, groupName string, userGroups []string) bool {
	if groupName == "" || groupName == "auto" {
		return false
	}
	return GroupInUserUsableGroupsForUser(userGroup, groupName, userGroups) && ratio_setting.ContainsGroupRatio(groupName)
}

func GetUserGroupNames() []string {
	names := make(map[string]struct{})
	addName := func(name string) {
		if name != "" && name != "auto" {
			names[name] = struct{}{}
		}
	}
	for name := range setting.GetUserGroupsCopy() {
		addName(name)
	}
	if !setting.UserGroupsConfigured() {
		for name := range ratio_setting.GetGroupRatioCopy() {
			addName(name)
		}
		for name := range setting.GetUserUsableGroupsCopy() {
			addName(name)
		}
		for name := range common.GetTopupGroupRatioCopy() {
			addName(name)
		}
		for name := range ratio_setting.GetGroupRatioSetting().GroupGroupRatio.ReadAll() {
			addName(name)
		}
		for name := range ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.ReadAll() {
			addName(name)
		}
	}
	names["default"] = struct{}{}
	groupNames := make([]string, 0, len(names))
	for name := range names {
		groupNames = append(groupNames, name)
	}
	slices.Sort(groupNames)
	return groupNames
}

func IsKnownUserGroup(name string) bool {
	if name == "" || name == "auto" {
		return false
	}
	for _, userGroup := range GetUserGroupNames() {
		if userGroup == name {
			return true
		}
	}
	return false
}

// GetUserAutoGroup 根据用户分组获取自动分组设置
func GetUserAutoGroup(userGroup string) []string {
	return GetUserAutoGroupForUser(userGroup, nil)
}

func GetUserAutoGroupForUser(userGroup string, userGroups []string) []string {
	autoGroups := make([]string, 0)
	seen := make(map[string]struct{})
	for _, group := range setting.GetAutoGroups() {
		if !IsUserSelectableGroupForUser(userGroup, group, userGroups) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		autoGroups = append(autoGroups, group)
	}
	return autoGroups
}

// FilterUserTokenAutoGroups applies current permissions before the current
// per-token limit. It intentionally does not fall back to the global Auto list.
func FilterUserTokenAutoGroups(userGroup string, groups []string) []string {
	return FilterUserTokenAutoGroupsForUser(userGroup, groups, nil)
}

func FilterUserTokenAutoGroupsForUser(userGroup string, groups []string, userGroups []string) []string {
	maxCount := setting.GetMaxTokenAutoGroups()
	filtered := make([]string, 0, min(len(groups), maxCount))
	seen := make(map[string]struct{})
	for _, group := range groups {
		if !IsUserSelectableGroupForUser(userGroup, group, userGroups) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		filtered = append(filtered, group)
		if len(filtered) == maxCount {
			break
		}
	}
	return filtered
}

// GetRequestAutoGroups resolves the ordered Auto groups for the current token.
// The absence of the context value means that the token inherits the complete
// global Auto list; a present (even empty) value is an explicit token snapshot.
func GetRequestAutoGroups(c *gin.Context, userGroup string) []string {
	return GetRequestAutoGroupsForUser(c, userGroup, GetRequestUserGroups(c, userGroup))
}

func GetRequestAutoGroupsForUser(c *gin.Context, userGroup string, userGroups []string) []string {
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenAutoGroups)
	if !ok {
		return GetUserAutoGroupForUser(userGroup, userGroups)
	}
	groups, ok := value.([]string)
	if !ok {
		return []string{}
	}
	return FilterUserTokenAutoGroupsForUser(userGroup, groups, userGroups)
}

// GetGroupsEnabledModels 按 groups 顺序获取各分组启用的模型并去重
func GetGroupsEnabledModels(groups []string) []string {
	seen := make(map[string]struct{})
	models := make([]string, 0)
	for _, group := range groups {
		for _, modelName := range model.GetGroupEnabledModels(group) {
			if _, ok := seen[modelName]; !ok {
				seen[modelName] = struct{}{}
				models = append(models, modelName)
			}
		}
	}
	return models
}

// GetUserGroupRatio 获取用户使用某个分组的倍率
// userGroup 用户分组
// group 需要获取倍率的分组
func GetUserGroupRatio(userGroup, group string) float64 {
	ratio, ok := ratio_setting.GetGroupGroupRatio(userGroup, group)
	if ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}
