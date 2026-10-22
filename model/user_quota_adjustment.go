package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var (
	ErrInvalidUserQuotaAdjustment = errors.New("invalid user quota adjustment")
	ErrUserQuotaPermission        = errors.New("cannot adjust quota for this user role")
	ErrQuotaTransferInsufficient  = errors.New("insufficient available quota for transfer")
	ErrQuotaTransferUnavailable   = errors.New("quota transfers are temporarily unavailable")
)

// UserQuotaAdjustment is the immutable database snapshot of a committed manual
// adjustment. Pending relay deductions in the quota cache are not part of it.
type UserQuotaAdjustment struct {
	UserID         int
	Username       string
	Before         int
	After          int
	OperatorID     int
	OperatorBefore int
	OperatorAfter  int
}

// TransferManagedUserQuota moves existing wallet quota without minting credits.
// Source cache reservation participates in the relay's available-balance check;
// the database transfer commits both wallets or neither.
func TransferManagedUserQuota(userID, managerID, managerRole int, mode string, value int) (*UserQuotaAdjustment, error) {
	if userID <= 0 || managerID <= 0 || userID == managerID || (mode != "add" && mode != "subtract" && mode != "override") || (mode != "override" && value <= 0) || (mode == "override" && value < 0) {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if !common.IsDelegatedUserManager(managerRole) {
		return nil, ErrUserQuotaPermission
	}
	if value > common.MaxWalletQuota {
		return nil, ErrWalletQuotaLimitExceeded
	}
	// Without Redis, batched settlements are not reflected in the database's
	// available balance. Never authorize a new transfer using that stale value.
	if common.BatchUpdateEnabled && !common.RedisEnabled {
		return nil, ErrQuotaTransferUnavailable
	}
	scope := UserManagementScope{OperatorID: managerID, OperatorRole: managerRole}
	if _, err := GetUserByIdInManagementScope(userID, scope, false); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserQuotaPermission
		}
		return nil, err
	}
	if common.RedisEnabled {
		for _, id := range []int{managerID, userID} {
			if _, err := GetUserCache(id); err != nil {
				return nil, ErrQuotaTransferUnavailable
			}
		}
	}
	var adjustment UserQuotaAdjustment
	var reservedUserID int
	var reservedAmount int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		user, err := scope.LockManagedUser(tx, userID, false)
		if err != nil {
			if errors.Is(err, ErrUserManagementPermission) {
				return ErrUserQuotaPermission
			}
			return err
		}
		var manager User
		if err := tx.First(&manager, managerID).Error; err != nil {
			return err
		}
		if user.Quota > common.MaxWalletQuota || user.Quota < -common.MaxWalletQuota {
			return ErrWalletQuotaLimitExceeded
		}
		if manager.Quota > common.MaxWalletQuota || manager.Quota < -common.MaxWalletQuota {
			return ErrWalletQuotaLimitExceeded
		}
		after := decimal.NewFromInt(int64(value))
		switch mode {
		case "add":
			after = decimal.NewFromInt(int64(user.Quota)).Add(after)
		case "subtract":
			after = decimal.NewFromInt(int64(user.Quota)).Sub(after)
		}
		userAfter, err := common.WalletQuotaFromDecimalStrict(after)
		if err != nil {
			return ErrWalletQuotaLimitExceeded
		}
		delta := int64(userAfter) - int64(user.Quota)
		managerAfter, err := common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(int64(manager.Quota)).Sub(decimal.NewFromInt(delta)))
		if err != nil {
			return ErrWalletQuotaLimitExceeded
		}
		if userAfter < 0 || managerAfter < 0 {
			return ErrQuotaTransferInsufficient
		}
		if delta != 0 {
			sourceID, destinationID := managerID, userID
			amount := delta
			if delta < 0 {
				sourceID, destinationID, amount = userID, managerID, -delta
			}
			if common.RedisEnabled {
				result, err := cacheTryReserveUserQuota(sourceID, amount)
				if err != nil || result == cacheQuotaMiss {
					return ErrQuotaTransferUnavailable
				}
				if result == cacheQuotaInsufficient {
					return ErrQuotaTransferInsufficient
				}
				reservedUserID, reservedAmount = sourceID, amount
			}
			// Delta updates preserve any already committed settlement changes.
			for _, change := range []struct {
				id    int
				delta int64
			}{{sourceID, -amount}, {destinationID, amount}} {
				result := tx.Model(&User{}).Where("id = ?", change.id).Update("quota", gorm.Expr("quota + ?", change.delta))
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return gorm.ErrRecordNotFound
				}
			}
		}
		adjustment = UserQuotaAdjustment{UserID: user.Id, Username: user.Username, Before: user.Quota, After: userAfter,
			OperatorID: managerID, OperatorBefore: manager.Quota, OperatorAfter: managerAfter}
		return nil
	})
	if err != nil {
		if reservedAmount != 0 {
			result, cacheErr := cacheApplyUserQuotaDelta(reservedUserID, reservedAmount)
			if cacheErr != nil || result != cacheQuotaOK {
				common.SysError(fmt.Sprintf("failed to restore rolled back managed quota transfer for user %d: result=%d error=%v", reservedUserID, result, cacheErr))
			}
		}
		return nil, err
	}
	delta := int64(adjustment.After) - int64(adjustment.Before)
	if delta != 0 {
		destinationID, amount := userID, delta
		if delta < 0 {
			destinationID, amount = managerID, -delta
		}
		if err := cacheIncrUserQuota(destinationID, amount); err != nil {
			common.SysError(fmt.Sprintf("failed to sync managed quota transfer credit for user %d: %v", destinationID, err))
		}
	}
	return &adjustment, nil
}

func AdjustUserQuota(userID, operatorRole int, mode string, value int) (*UserQuotaAdjustment, error) {
	if userID <= 0 || (mode != "add" && mode != "subtract" && mode != "override") {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if mode != "override" && value <= 0 {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if value > common.MaxWalletQuota || value < -common.MaxWalletQuota {
		return nil, ErrWalletQuotaLimitExceeded
	}

	var adjustment UserQuotaAdjustment
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		if operatorRole != common.RoleRootUser && operatorRole <= user.Role {
			return ErrUserQuotaPermission
		}
		if user.Quota > common.MaxWalletQuota || user.Quota < -common.MaxWalletQuota {
			return ErrWalletQuotaLimitExceeded
		}
		quota := decimal.NewFromInt(int64(value))
		switch mode {
		case "add":
			quota = decimal.NewFromInt(int64(user.Quota)).Add(quota)
		case "subtract":
			quota = decimal.NewFromInt(int64(user.Quota)).Sub(quota)
		}
		after, err := common.WalletQuotaFromDecimalStrict(quota)
		if err != nil {
			return ErrWalletQuotaLimitExceeded
		}
		// An unchanged override is a successful operation, including on MySQL
		// configurations that count only changed rows in RowsAffected.
		if after != user.Quota {
			result := tx.Model(&User{}).Where("id = ?", userID).Update("quota", after)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
		}
		adjustment = UserQuotaAdjustment{UserID: user.Id, Username: user.Username, Before: user.Quota, After: after}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Apply only the committed difference, preserving outstanding reservations.
	// Both balances are bounded above, so their difference fits in int64.
	delta := int64(adjustment.After) - int64(adjustment.Before)
	if delta != 0 {
		if err := cacheIncrUserQuota(userID, delta); err != nil {
			common.SysError(fmt.Sprintf("failed to sync manual quota adjustment for user %d: %s", userID, err))
		}
	}
	return &adjustment, nil
}
