package setting

import (
	"errors"
	"maps"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

var userGroups = map[string]string{
	"default": "默认用户组",
}
var userGroupsMutex sync.RWMutex
var userGroupsConfigured bool

const MaxUserGroupNameLength = 64

func GetUserGroupsCopy() map[string]string {
	userGroupsMutex.RLock()
	defer userGroupsMutex.RUnlock()

	copyUserGroups := make(map[string]string, len(userGroups))
	maps.Copy(copyUserGroups, userGroups)
	return copyUserGroups
}

func UserGroups2JSONString() string {
	data, err := common.Marshal(GetUserGroupsCopy())
	if err != nil {
		common.SysLog("error marshalling configured user groups: " + err.Error())
		return "{}"
	}
	return string(data)
}

func parseUserGroups(value string) (map[string]string, error) {
	var updated map[string]string
	if err := common.UnmarshalJsonStr(value, &updated); err != nil {
		return nil, err
	}
	if updated == nil {
		updated = make(map[string]string)
	}
	for name := range updated {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(name) != name {
			return nil, errors.New("user group names must be non-empty and trimmed")
		}
		if name == "auto" {
			return nil, errors.New("auto is reserved for API key routing")
		}
		if utf8.RuneCountInString(name) > MaxUserGroupNameLength {
			return nil, errors.New("user group names must be 64 characters or fewer")
		}
	}
	if _, ok := updated["default"]; !ok {
		updated["default"] = "默认用户组"
	}
	return updated, nil
}

func ValidateUserGroupsJSON(value string) error {
	_, err := parseUserGroups(value)
	return err
}

func UpdateUserGroupsByJSONString(value string) error {
	updated, err := parseUserGroups(value)
	if err != nil {
		return err
	}

	userGroupsMutex.Lock()
	userGroups = updated
	userGroupsConfigured = true
	userGroupsMutex.Unlock()
	return nil
}

func UserGroupsConfigured() bool {
	userGroupsMutex.RLock()
	defer userGroupsMutex.RUnlock()
	return userGroupsConfigured
}
