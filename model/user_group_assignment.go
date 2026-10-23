package model

import (
	"errors"
	"slices"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UserGroupAssignment struct {
	UserID    int    `gorm:"primaryKey;autoIncrement:false;index:idx_user_group_assignment_group_user,priority:2"`
	GroupName string `gorm:"primaryKey;size:64;index:idx_user_group_assignment_group_user,priority:1"`
}

func (UserGroupAssignment) TableName() string {
	return "user_group_assignments"
}

func normalizeUserGroupAssignments(groups []string) []string {
	seen := make(map[string]struct{}, len(groups))
	normalized := make([]string, 0, len(groups))
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" || group == "auto" {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		normalized = append(normalized, group)
	}
	slices.Sort(normalized)
	return normalized
}

func GetUserGroupAssignmentsTx(tx *gorm.DB, userID int) ([]string, error) {
	var assignments []UserGroupAssignment
	if err := tx.Where("user_id = ?", userID).Order("group_name").Find(&assignments).Error; err != nil {
		return nil, err
	}
	groups := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		if assignment.GroupName != "" {
			groups = append(groups, assignment.GroupName)
		}
	}
	return groups, nil
}

func GetUserGroupAssignments(userID int) ([]string, error) {
	return GetUserGroupAssignmentsTx(DB, userID)
}

func ReplaceUserGroupAssignmentsTx(tx *gorm.DB, userID int, groups []string) error {
	groups = normalizeUserGroupAssignments(groups)
	if userID <= 0 {
		return errors.New("invalid user group assignment target")
	}
	if err := tx.Where("user_id = ?", userID).Delete(&UserGroupAssignment{}).Error; err != nil {
		return err
	}
	assignments := make([]UserGroupAssignment, 0, len(groups))
	if len(groups) == 0 {
		// Keep an explicit row so startup backfill does not mistake a deliberate
		// no-access user for an older account that needs its group migrated.
		assignments = append(assignments, UserGroupAssignment{UserID: userID})
	}
	for _, group := range groups {
		assignments = append(assignments, UserGroupAssignment{UserID: userID, GroupName: group})
	}
	return tx.Create(&assignments).Error
}

func SetUserGroupAssignmentsTx(tx *gorm.DB, userID int, groups []string) (bool, error) {
	groups = normalizeUserGroupAssignments(groups)
	current, err := GetUserGroupAssignmentsTx(tx, userID)
	if err != nil {
		return false, err
	}
	if sameUserGroupAssignments(current, groups) {
		return false, nil
	}
	if _, err := IncrementUserAuthVersionWithTx(tx, userID); err != nil {
		return false, err
	}
	if err := ReplaceUserGroupAssignmentsTx(tx, userID, groups); err != nil {
		return false, err
	}
	return true, nil
}

func sameUserGroupAssignments(left, right []string) bool {
	left = normalizeUserGroupAssignments(left)
	right = normalizeUserGroupAssignments(right)
	return slices.Equal(left, right)
}

func SameUserGroupAssignments(left, right []string) bool {
	return sameUserGroupAssignments(left, right)
}

func userGroupAssignmentsSubset(candidate, allowed []string) bool {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, group := range allowed {
		allowedSet[group] = struct{}{}
	}
	for _, group := range normalizeUserGroupAssignments(candidate) {
		if _, ok := allowedSet[group]; !ok {
			return false
		}
	}
	return true
}

func UserGroupAssignmentsSubset(candidate, allowed []string) bool {
	return userGroupAssignmentsSubset(candidate, allowed)
}

func intersectUserGroupAssignments(left, right []string) []string {
	allowed := make(map[string]struct{}, len(right))
	for _, group := range right {
		allowed[group] = struct{}{}
	}
	intersection := make([]string, 0, min(len(left), len(right)))
	for _, group := range left {
		if _, ok := allowed[group]; ok {
			intersection = append(intersection, group)
		}
	}
	return normalizeUserGroupAssignments(intersection)
}

// PruneDescendantUserGroupAssignmentsTx removes assignments that are no longer
// granted by an ancestor and advances auth versions for affected descendants.
func PruneDescendantUserGroupAssignmentsTx(tx *gorm.DB, parentID int, parentGroups []string) ([]int, error) {
	if tx == nil || parentID <= 0 {
		return nil, errors.New("invalid user group assignment hierarchy")
	}
	changedIDs := make([]int, 0)
	frontier := []int{parentID}
	frontierGroups := map[int][]string{parentID: normalizeUserGroupAssignments(parentGroups)}
	seen := map[int]struct{}{parentID: {}}
	for len(frontier) > 0 {
		var descendants []User
		if err := lockForUpdate(tx.Unscoped()).Select("id", "creator_id", "deleted_at").
			Where("creator_id IN ?", frontier).Order("id").Find(&descendants).Error; err != nil {
			return nil, err
		}
		nextFrontier := make([]int, 0, len(descendants))
		nextGroups := make(map[int][]string, len(descendants))
		for i := range descendants {
			descendant := &descendants[i]
			if _, ok := seen[descendant.Id]; ok {
				continue
			}
			seen[descendant.Id] = struct{}{}
			nextFrontier = append(nextFrontier, descendant.Id)
			assigned, err := GetUserGroupAssignmentsTx(tx, descendant.Id)
			if err != nil {
				return nil, err
			}
			allowed := intersectUserGroupAssignments(assigned, frontierGroups[descendant.CreatorID])
			if !sameUserGroupAssignments(assigned, allowed) {
				if _, err := IncrementUserAuthVersionWithTx(tx, descendant.Id); err != nil {
					return nil, err
				}
				if err := ReplaceUserGroupAssignmentsTx(tx, descendant.Id, allowed); err != nil {
					return nil, err
				}
				if !descendant.DeletedAt.Valid {
					changedIDs = append(changedIDs, descendant.Id)
				}
			}
			nextGroups[descendant.Id] = allowed
		}
		frontier = nextFrontier
		frontierGroups = nextGroups
	}
	return changedIDs, nil
}

func loadUserGroupAssignments(tx *gorm.DB, users []*User) error {
	if len(users) == 0 {
		return nil
	}
	userIDs := make(map[int]struct{}, len(users)*3)
	parents := make(map[int]int, len(users)*2)
	frontier := make([]int, 0, len(users))
	for _, user := range users {
		if user.Id <= 0 {
			continue
		}
		userIDs[user.Id] = struct{}{}
		parents[user.Id] = user.CreatorID
	}
	for _, user := range users {
		if user.Id <= 0 {
			continue
		}
		if user.CreatorID > 0 {
			if _, ok := userIDs[user.CreatorID]; !ok {
				userIDs[user.CreatorID] = struct{}{}
				frontier = append(frontier, user.CreatorID)
			}
		}
	}
	knownParents := make(map[int]struct{}, len(userIDs))
	for id := range userIDs {
		knownParents[id] = struct{}{}
	}
	for len(frontier) > 0 {
		var ancestors []User
		if err := tx.Unscoped().Select("id", "creator_id").Where("id IN ?", frontier).Find(&ancestors).Error; err != nil {
			return err
		}
		next := make([]int, 0, len(ancestors))
		for i := range ancestors {
			ancestor := &ancestors[i]
			parents[ancestor.Id] = ancestor.CreatorID
			if ancestor.CreatorID <= 0 {
				continue
			}
			if _, ok := knownParents[ancestor.CreatorID]; !ok {
				knownParents[ancestor.CreatorID] = struct{}{}
				userIDs[ancestor.CreatorID] = struct{}{}
				next = append(next, ancestor.CreatorID)
			}
		}
		frontier = next
	}
	allIDs := make([]int, 0, len(userIDs))
	for id := range userIDs {
		allIDs = append(allIDs, id)
	}
	slices.Sort(allIDs)
	var assignments []UserGroupAssignment
	if err := tx.Where("user_id IN ?", allIDs).Order("user_id, group_name").Find(&assignments).Error; err != nil {
		return err
	}
	groupsByUser := make(map[int][]string, len(allIDs))
	for _, assignment := range assignments {
		if _, ok := groupsByUser[assignment.UserID]; !ok {
			groupsByUser[assignment.UserID] = make([]string, 0)
		}
		if assignment.GroupName != "" {
			groupsByUser[assignment.UserID] = append(groupsByUser[assignment.UserID], assignment.GroupName)
		}
	}
	for _, user := range users {
		groups := slices.Clone(groupsByUser[user.Id])
		seenAncestors := make(map[int]struct{}, 3)
		ancestorID := parents[user.Id]
		for ancestorID > 0 {
			if _, ok := seenAncestors[ancestorID]; ok {
				groups = nil
				break
			}
			seenAncestors[ancestorID] = struct{}{}
			groups = intersectUserGroupAssignments(groups, groupsByUser[ancestorID])
			ancestorID = parents[ancestorID]
		}
		if groups == nil {
			groups = make([]string, 0)
		}
		user.UserGroups = groups
	}
	return nil
}

func GetEffectiveUserGroupAssignmentsTx(tx *gorm.DB, userID int) ([]string, error) {
	if tx == nil || userID <= 0 {
		return nil, errors.New("invalid user group assignment target")
	}
	var user User
	if err := tx.Unscoped().Select("id", "creator_id").First(&user, userID).Error; err != nil {
		return nil, err
	}
	if err := loadUserGroupAssignments(tx, []*User{&user}); err != nil {
		return nil, err
	}
	return slices.Clone(user.UserGroups), nil
}

func InitializeUserGroupAssignments() error {
	initCol()
	var users []User
	if err := DB.Unscoped().Model(&User{}).
		Where("NOT EXISTS (SELECT 1 FROM user_group_assignments WHERE user_group_assignments.user_id = users.id)").
		Select("id", commonGroupCol).Find(&users).Error; err != nil {
		return err
	}
	assignments := make([]UserGroupAssignment, 0, len(users))
	for _, user := range users {
		group := user.Group
		if group == "" {
			group = "default"
		}
		assignments = append(assignments, UserGroupAssignment{UserID: user.Id, GroupName: group})
	}
	if len(assignments) == 0 {
		return nil
	}
	return DB.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&assignments, 500).Error
}
