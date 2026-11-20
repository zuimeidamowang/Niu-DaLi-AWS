package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	_ "golang.org/x/image/webp"
)

var (
	ErrMediaAssetUnavailable = errors.New("media asset storage is unavailable")
	ErrMediaAssetInvalid     = errors.New("invalid or unsupported media file")
	ErrMediaAssetTooLarge    = errors.New("media file exceeds the upload limit")
	mediaAssetIDPattern      = regexp.MustCompile(`^media_[a-zA-Z0-9_-]{32}$`)
	mediaAssetService        atomic.Pointer[MediaAssetService]
)

const mediaAssetSourceTTL = int64(48 * 60 * 60)

type MediaAssetObject struct {
	Body         io.ReadCloser
	Size         int64
	ContentRange string
}

// MediaAssetStorage is deliberately independent of any model provider. Readers
// and credentials stay in the host; plugins receive only scoped URLs/file refs.
type MediaAssetStorage interface {
	Put(context.Context, string, io.ReadSeeker, int64, string) error
	Get(context.Context, string, string) (*MediaAssetObject, error)
	Delete(context.Context, string) error
}

type MediaAssetService struct {
	config system_setting.MediaAssetConfig
	store  MediaAssetStorage
}

type MediaAssetInput struct {
	ID       string
	Type     string
	MIMEType string
	Size     int64
	URL      string
	FileRef  string
}

func InitMediaAssets(ctx context.Context) error {
	config, err := system_setting.LoadMediaAssetConfig()
	if err != nil {
		mediaAssetService.Store(&MediaAssetService{})
		return err
	}
	service := &MediaAssetService{config: config}
	if config.Enabled {
		service.store, err = newS3MediaAssetStorage(ctx, config)
		if err != nil {
			mediaAssetService.Store(&MediaAssetService{})
			return ErrMediaAssetUnavailable
		}
	}
	mediaAssetService.Store(service)
	return nil
}

func GetMediaAssetService() *MediaAssetService {
	if service := mediaAssetService.Load(); service != nil {
		return service
	}
	return &MediaAssetService{}
}

func (s *MediaAssetService) Enabled() bool { return s.config.Enabled && s.store != nil }

func (s *MediaAssetService) MaxUploadBytes() int64 {
	return max(s.config.ImageMaxBytes, s.config.VideoMaxBytes)
}

func (s *MediaAssetService) Upload(ctx context.Context, userID int, filename, suppliedMIME string, content io.Reader) (*model.MediaAsset, error) {
	if !s.Enabled() {
		return nil, ErrMediaAssetUnavailable
	}
	ext := strings.ToLower(filepath.Ext(filename))
	mimeType := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".mp4": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime"}[ext]
	if mimeType == "" || len(filename) > 255 || strings.ContainsAny(filename, "\x00\r\n/\\:") {
		return nil, ErrMediaAssetInvalid
	}
	kind := "video"
	limit := s.config.VideoMaxBytes
	if strings.HasPrefix(mimeType, "image/") {
		kind = "image"
		limit = s.config.ImageMaxBytes
	}
	if suppliedMIME != "" && suppliedMIME != "application/octet-stream" && suppliedMIME != mimeType {
		return nil, ErrMediaAssetInvalid
	}
	random, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return nil, ErrMediaAssetUnavailable
	}
	now := time.Now().Unix()
	asset := &model.MediaAsset{ID: "media_" + random, UserID: userID, ObjectKey: s.config.Prefix + "/" + random, Type: kind, MIMEType: mimeType, Size: limit, State: model.MediaAssetUploading, CreatedAt: now, ExpiresAt: now + model.MediaAssetLeaseTTL}
	// Reserve available capacity, bounded by the file limit, before reading the
	// body. Parallel uploads cannot bypass quota or fill temporary storage.
	if err := model.ReserveMediaAsset(ctx, asset, s.config.UserMaxBytes); err != nil {
		return nil, err
	}
	complete := false
	defer func() {
		if complete {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := s.store.Delete(cleanupCtx, asset.ObjectKey); err != nil {
			_ = model.DB.WithContext(cleanupCtx).Model(asset).Updates(map[string]any{"state": model.MediaAssetDeleting, "expires_at": now}).Error
			return
		}
		_ = model.DB.WithContext(cleanupCtx).Where("id = ? AND state = ?", asset.ID, model.MediaAssetUploading).Delete(&model.MediaAsset{}).Error
	}()
	file, err := os.CreateTemp("", "niu-media-upload-*")
	if err != nil {
		return nil, ErrMediaAssetUnavailable
	}
	defer os.Remove(file.Name())
	defer file.Close()
	size, err := io.Copy(file, io.LimitReader(content, asset.Size+1))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, ErrMediaAssetTooLarge
		}
		return nil, ErrMediaAssetInvalid
	}
	if size > asset.Size && asset.Size < limit {
		return nil, model.ErrMediaStorageQuota
	}
	if size > limit {
		return nil, ErrMediaAssetTooLarge
	}
	if size == 0 {
		return nil, ErrMediaAssetInvalid
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, ErrMediaAssetUnavailable
	}
	header := make([]byte, min(size, 512))
	if _, err = io.ReadFull(file, header); err != nil {
		return nil, ErrMediaAssetInvalid
	}
	detected := http.DetectContentType(header)
	if mimeType == "video/quicktime" {
		if len(header) < 12 || string(header[4:8]) != "ftyp" || string(header[8:12]) != "qt  " {
			return nil, ErrMediaAssetInvalid
		}
	} else if detected != mimeType {
		return nil, ErrMediaAssetInvalid
	}
	if kind == "image" {
		if _, err = file.Seek(0, io.SeekStart); err != nil {
			return nil, ErrMediaAssetUnavailable
		}
		info, _, decodeErr := image.DecodeConfig(file)
		if decodeErr != nil || info.Width < 1 || info.Height < 1 || int64(info.Width) > 100_000_000/int64(info.Height) {
			return nil, ErrMediaAssetInvalid
		}
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, ErrMediaAssetUnavailable
	}
	if err = s.store.Put(ctx, asset.ObjectKey, file, size, mimeType); err != nil {
		return nil, ErrMediaAssetUnavailable
	}
	asset.Size = size
	asset.State = model.MediaAssetReady
	asset.ExpiresAt = now + model.MediaAssetRetention
	result := model.DB.WithContext(ctx).Model(&model.MediaAsset{}).Where("id = ? AND state = ?", asset.ID, model.MediaAssetUploading).Updates(map[string]any{"size": asset.Size, "state": asset.State, "expires_at": asset.ExpiresAt})
	if result.Error != nil || result.RowsAffected != 1 {
		return nil, ErrMediaAssetUnavailable
	}
	complete = true
	return asset, nil
}

func (s *MediaAssetService) sourceSignature(id string, expires int64) string {
	hash := hmac.New(sha256.New, []byte(s.config.SigningKey))
	_, _ = fmt.Fprintf(hash, "media-source-v1\n%s\n%d", id, expires)
	return hex.EncodeToString(hash.Sum(nil))
}

func ResolveMediaAssets(ctx context.Context, userID int, ids []string) (map[string]MediaAssetInput, error) {
	s := GetMediaAssetService()
	if !s.Enabled() {
		return nil, ErrMediaAssetUnavailable
	}
	if len(ids) > 32 {
		return nil, ErrMediaAssetInvalid
	}
	assets := make(map[string]MediaAssetInput, len(ids))
	expires := time.Now().Unix() + mediaAssetSourceTTL
	for _, id := range ids {
		if !mediaAssetIDPattern.MatchString(id) {
			return nil, model.ErrMediaAssetNotFound
		}
		asset, err := model.GetMediaAsset(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		assets[id] = MediaAssetInput{ID: id, Type: asset.Type, MIMEType: asset.MIMEType, Size: asset.Size, URL: s.config.PublicBaseURL + "/v1/media/assets/" + id + "/source?expires=" + strconv.FormatInt(expires, 10) + "&signature=" + s.sourceSignature(id, expires), FileRef: "asset_file:" + id}
	}
	return assets, nil
}

func OpenMediaAsset(ctx context.Context, userID int, id string) (io.ReadCloser, int64, string, error) {
	s := GetMediaAssetService()
	if !s.Enabled() {
		return nil, 0, "", ErrMediaAssetUnavailable
	}
	asset, err := model.GetMediaAsset(ctx, userID, id)
	if err != nil {
		return nil, 0, "", err
	}
	object, err := s.store.Get(ctx, asset.ObjectKey, "")
	if err != nil {
		return nil, 0, "", ErrMediaAssetUnavailable
	}
	return object.Body, object.Size, asset.MIMEType, nil
}

// Source authorizes only the signed object and expiry. These bearer capabilities
// stay inside driver contexts; API metadata never contains them.
func (s *MediaAssetService) Source(ctx context.Context, id, expiresValue, signature string) (*model.MediaAsset, error) {
	if !s.Enabled() {
		return nil, ErrMediaAssetUnavailable
	}
	expires, err := strconv.ParseInt(expiresValue, 10, 64)
	provided, decodeErr := hex.DecodeString(signature)
	now := time.Now().Unix()
	if err != nil || decodeErr != nil || len(provided) != sha256.Size || !mediaAssetIDPattern.MatchString(id) || expires <= now || expires > now+mediaAssetSourceTTL || strconv.FormatInt(expires, 10) != expiresValue {
		return nil, model.ErrMediaAssetNotFound
	}
	expected, _ := hex.DecodeString(s.sourceSignature(id, expires))
	if !hmac.Equal(expected, provided) {
		return nil, model.ErrMediaAssetNotFound
	}
	var asset model.MediaAsset
	if err := model.DB.WithContext(ctx).Select("user_id").Where("id = ?", id).First(&asset).Error; err != nil {
		return nil, model.ErrMediaAssetNotFound
	}
	return model.GetMediaAsset(ctx, asset.UserID, id)
}

func (s *MediaAssetService) Open(ctx context.Context, asset *model.MediaAsset, byteRange string) (*MediaAssetObject, error) {
	if !s.Enabled() {
		return nil, ErrMediaAssetUnavailable
	}
	object, err := s.store.Get(ctx, asset.ObjectKey, byteRange)
	if err != nil {
		return nil, ErrMediaAssetUnavailable
	}
	return object, nil
}

func LeaseMediaAssets(ctx context.Context, userID int, taskID string, ids []string) error {
	return model.LeaseMediaAssets(ctx, userID, taskID, ids)
}

func CancelMediaAssetTask(ctx context.Context, taskID string) error {
	return model.CancelMediaAssetTask(ctx, taskID)
}

func (s *MediaAssetService) Cleanup(ctx context.Context, now int64) error {
	if !s.Enabled() {
		return nil
	}
	cursor := ""
	var cleanupErr error
	for {
		var ids []string
		if err := model.DB.WithContext(ctx).Model(&model.MediaAsset{}).Where("id > ?", cursor).Order("id").Limit(100).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return cleanupErr
		}
		for _, id := range ids {
			asset, err := model.ClaimExpiredMediaAsset(ctx, id, now)
			if errors.Is(err, model.ErrMediaAssetNotFound) {
				continue
			}
			if err != nil {
				cleanupErr = err
				continue
			}
			if err := s.store.Delete(ctx, asset.ObjectKey); err != nil {
				cleanupErr = ErrMediaAssetUnavailable
				continue
			}
			if err := model.DeleteClaimedMediaAsset(ctx, id); err != nil {
				cleanupErr = err
			}
		}
		cursor = ids[len(ids)-1]
	}
}

func StartMediaAssetCleanup(ctx context.Context) {
	if !GetMediaAssetService().Enabled() {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			cleanupCtx, cancel := context.WithTimeout(ctx, 50*time.Second)
			if err := GetMediaAssetService().Cleanup(cleanupCtx, time.Now().Unix()); err != nil {
				// Transport errors can contain signed requests or credentials.
				common.SysError("media asset cleanup failed; expired objects will be retried")
			}
			cancel()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
