package setting

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateUserGroupsByJSONString(t *testing.T) {
	originalGroups := GetUserGroupsCopy()
	originalConfigured := UserGroupsConfigured()
	t.Cleanup(func() {
		userGroupsMutex.Lock()
		userGroups = originalGroups
		userGroupsConfigured = originalConfigured
		userGroupsMutex.Unlock()
	})

	tests := []struct {
		name      string
		value     string
		wantError string
	}{
		{name: "inserts required default group", value: `{"vip":"Premium"}`},
		{name: "rejects blank group names", value: `{" ":"Invalid"}`, wantError: "user group names must be non-empty and trimmed"},
		{name: "rejects untrimmed group names", value: `{" vip":"Premium"}`, wantError: "user group names must be non-empty and trimmed"},
		{name: "rejects reserved auto group", value: `{"auto":"Automatic"}`, wantError: "auto is reserved for API key routing"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := UpdateUserGroupsByJSONString(test.value)
			if test.wantError != "" {
				require.EqualError(t, err, test.wantError)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, map[string]string{"default": "默认用户组", "vip": "Premium"}, GetUserGroupsCopy())
			assert.True(t, UserGroupsConfigured())
		})
	}
}

func TestUpdateUserGroupsEnforcesMaximumNameLength(t *testing.T) {
	originalGroups := GetUserGroupsCopy()
	originalConfigured := UserGroupsConfigured()
	t.Cleanup(func() {
		userGroupsMutex.Lock()
		userGroups = originalGroups
		userGroupsConfigured = originalConfigured
		userGroupsMutex.Unlock()
	})

	tooLong, err := common.Marshal(map[string]string{strings.Repeat("a", MaxUserGroupNameLength+1): "Too long"})
	require.NoError(t, err)
	require.Error(t, UpdateUserGroupsByJSONString(string(tooLong)))
	assert.Equal(t, originalGroups, GetUserGroupsCopy())
	assert.Equal(t, originalConfigured, UserGroupsConfigured())

	maxLengthName := strings.Repeat("界", MaxUserGroupNameLength)
	maxLength, err := common.Marshal(map[string]string{maxLengthName: "Valid"})
	require.NoError(t, err)
	require.NoError(t, UpdateUserGroupsByJSONString(string(maxLength)))
	assert.Equal(t, map[string]string{"default": "默认用户组", maxLengthName: "Valid"}, GetUserGroupsCopy())
}
