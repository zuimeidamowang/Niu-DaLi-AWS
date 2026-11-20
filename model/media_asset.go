package model

import (
	"context"
	"errors"
	"slices"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MediaAssetUploading = "uploading"
	MediaAssetReady     = "ready"
	MediaAssetDeleting  = "deleting"
	MediaAssetRetention = int64(24 * 60 * 60)
	MediaAssetLeaseTTL  = int64(2 * 60 * 60)
)

var (
	ErrMediaAssetNotFound = errors.New("media asset not found or expired")
	ErrMediaStorageQuota  = errors.New("media storage quota exceeded")
)

// MediaAsset stores input media metadata only. Object keys never appear in a
// public response, and objects remain private in the configured storage bucket.
type MediaAsset struct {
	ID        string `gorm:"type:varchar(64);primaryKey"`
	UserID    int    `gorm:"index"`
	ObjectKey string `gorm:"type:varchar(255)"`
	Type      string `gorm:"type:varchar(12)"`
	MIMEType  string `gorm:"type:varchar(64)"`
	Size      int64
	State     string `gorm:"type:varchar(16);index"`
	CreatedAt int64
	ExpiresAt int64 `gorm:"index"`
}

// A reference is recorded before contacting an upstream. A crashed submit with
// no durable task is protected for two hours; existing nonterminal tasks stay
// protected without a time limit. Terminal tasks retain their media for 24h.
type MediaAssetTask struct {
	AssetID   string `gorm:"type:varchar(64);primaryKey"`
	TaskID    string `gorm:"type:varchar(64);primaryKey;index"`
	UserID    int    `gorm:"index"`
	CreatedAt int64
}

func ReserveMediaAsset(ctx context.Context, asset *MediaAsset, quota int64) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id").Where("id = ?", asset.UserID).First(&user).Error; err != nil {
			return err
		}
		var used int64
		if err := tx.Model(&MediaAsset{}).Where("user_id = ?", asset.UserID).Select("COALESCE(SUM(size), 0)").Scan(&used).Error; err != nil {
			return err
		}
		if asset.Size < 1 || used < 0 || used >= quota {
			return ErrMediaStorageQuota
		}
		asset.Size = min(asset.Size, quota-used)
		return tx.Create(asset).Error
	})
}

// mediaAssetExpiry runs while holding the asset row lock. Zero means a task is
// active (or its submission lease is active), so an expiry cannot yet be given.
func mediaAssetExpiry(tx *gorm.DB, asset *MediaAsset, now int64) (int64, error) {
	expires := asset.CreatedAt + MediaAssetRetention
	var refs []MediaAssetTask
	if err := tx.Where("asset_id = ?", asset.ID).Find(&refs).Error; err != nil {
		return 0, err
	}
	for _, ref := range refs {
		var task Task
		err := tx.Select("status", "finish_time", "updated_at").Where("task_id = ? AND user_id = ?", ref.TaskID, ref.UserID).First(&task).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if ref.CreatedAt+MediaAssetLeaseTTL > now {
				return 0, nil
			}
			continue
		}
		if err != nil {
			return 0, err
		}
		if task.Status != TaskStatusSuccess && task.Status != TaskStatusFailure {
			return 0, nil
		}
		expires = max(expires, max(task.FinishTime, task.UpdatedAt)+MediaAssetRetention)
	}
	return expires, nil
}

func GetMediaAsset(ctx context.Context, userID int, id string) (*MediaAsset, error) {
	var asset MediaAsset
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ? AND state = ?", id, userID, MediaAssetReady).First(&asset).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrMediaAssetNotFound
			}
			return err
		}
		expires, err := mediaAssetExpiry(tx, &asset, time.Now().Unix())
		if err != nil {
			return err
		}
		if expires != 0 && expires <= time.Now().Unix() {
			return ErrMediaAssetNotFound
		}
		asset.ExpiresAt = expires
		return nil
	})
	return &asset, err
}

func LeaseMediaAssets(ctx context.Context, userID int, taskID string, ids []string) error {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().Unix()
		for _, id := range ids {
			var asset MediaAsset
			if err := lockForUpdate(tx).Where("id = ? AND user_id = ? AND state = ?", id, userID, MediaAssetReady).First(&asset).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrMediaAssetNotFound
				}
				return err
			}
			expires, err := mediaAssetExpiry(tx, &asset, now)
			if err != nil {
				return err
			}
			if expires != 0 && expires <= now {
				return ErrMediaAssetNotFound
			}
			ref := MediaAssetTask{AssetID: id, TaskID: taskID, UserID: userID, CreatedAt: now}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&ref).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func CancelMediaAssetTask(ctx context.Context, taskID string) error {
	var count int64
	if err := DB.WithContext(ctx).Model(&Task{}).Where("task_id = ?", taskID).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	return DB.WithContext(ctx).Where("task_id = ?", taskID).Delete(&MediaAssetTask{}).Error
}

// ClaimExpiredMediaAsset changes state under the same row lock used by leases.
// Once claimed, no new task can use the object; storage deletion can be retried.
func ClaimExpiredMediaAsset(ctx context.Context, id string, now int64) (*MediaAsset, error) {
	var asset MediaAsset
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("id = ?", id).First(&asset).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrMediaAssetNotFound
			}
			return err
		}
		if asset.State == MediaAssetDeleting {
			return nil
		}
		expires := asset.ExpiresAt
		if asset.State == MediaAssetReady {
			var err error
			expires, err = mediaAssetExpiry(tx, &asset, now)
			if err != nil {
				return err
			}
		}
		if expires == 0 || expires > now {
			return ErrMediaAssetNotFound
		}
		asset.State = MediaAssetDeleting
		return tx.Model(&MediaAsset{}).Where("id = ?", id).Update("state", MediaAssetDeleting).Error
	})
	return &asset, err
}

func DeleteClaimedMediaAsset(ctx context.Context, id string) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var asset MediaAsset
		if err := lockForUpdate(tx).Where("id = ? AND state = ?", id, MediaAssetDeleting).First(&asset).Error; err != nil {
			return err
		}
		if err := tx.Where("asset_id = ?", id).Delete(&MediaAssetTask{}).Error; err != nil {
			return err
		}
		return tx.Delete(&asset).Error
	})
}
