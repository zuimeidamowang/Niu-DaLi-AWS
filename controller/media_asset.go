package controller

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type mediaAssetResponse struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Type      string `json:"type"`
	MIMEType  string `json:"mime_type"`
	Size      int64  `json:"size"`
	Status    string `json:"status"`
	ExpiresAt int64  `json:"expires_at"`
}

func publicMediaAsset(asset *model.MediaAsset) mediaAssetResponse {
	return mediaAssetResponse{ID: asset.ID, Object: "media.asset", Type: asset.Type, MIMEType: asset.MIMEType, Size: asset.Size, Status: asset.State, ExpiresAt: asset.ExpiresAt}
}

// This reader rejects extra fields/parts before Upload can publish the asset.
// It keeps a single file streaming through the bounded upload implementation.
type singleMediaUploadReader struct {
	part   *multipart.Part
	reader *multipart.Reader
	ended  bool
}

func (r *singleMediaUploadReader) Read(p []byte) (int, error) {
	if r.ended {
		return 0, io.EOF
	}
	n, err := r.part.Read(p)
	if err != io.EOF {
		return n, err
	}
	if _, nextErr := r.reader.NextPart(); nextErr != io.EOF {
		if nextErr != nil {
			return n, nextErr
		}
		return n, service.ErrMediaAssetInvalid
	}
	r.ended = true
	return n, io.EOF
}

func UploadMediaAsset(c *gin.Context) {
	s := service.GetMediaAssetService()
	if !s.Enabled() {
		writeMediaAssetError(c, service.ErrMediaAssetUnavailable)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, s.MaxUploadBytes()+(1<<20))
	reader, err := c.Request.MultipartReader()
	if err != nil {
		writeMediaAssetError(c, service.ErrMediaAssetInvalid)
		return
	}
	part, err := reader.NextPart()
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeMediaAssetError(c, err)
		} else {
			writeMediaAssetError(c, service.ErrMediaAssetInvalid)
		}
		return
	}
	if part.FormName() != "file" || part.FileName() == "" {
		writeMediaAssetError(c, service.ErrMediaAssetInvalid)
		return
	}
	defer part.Close()
	suppliedMIME := part.Header.Get("Content-Type")
	if suppliedMIME != "" {
		suppliedMIME, _, err = mime.ParseMediaType(suppliedMIME)
		if err != nil {
			writeMediaAssetError(c, service.ErrMediaAssetInvalid)
			return
		}
	}
	asset, err := s.Upload(c.Request.Context(), common.GetContextKeyInt(c, constant.ContextKeyUserId), part.FileName(), suppliedMIME, &singleMediaUploadReader{part: part, reader: reader})
	if err != nil {
		writeMediaAssetError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusCreated, publicMediaAsset(asset))
}

func RetrieveMediaAsset(c *gin.Context) {
	if !service.GetMediaAssetService().Enabled() {
		writeMediaAssetError(c, service.ErrMediaAssetUnavailable)
		return
	}
	asset, err := model.GetMediaAsset(c.Request.Context(), common.GetContextKeyInt(c, constant.ContextKeyUserId), c.Param("asset_id"))
	if err != nil {
		writeMediaAssetError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, publicMediaAsset(asset))
}

func MediaAssetContent(c *gin.Context) {
	if !service.GetMediaAssetService().Enabled() {
		writeMediaAssetError(c, service.ErrMediaAssetUnavailable)
		return
	}
	asset, err := model.GetMediaAsset(c.Request.Context(), common.GetContextKeyInt(c, constant.ContextKeyUserId), c.Param("asset_id"))
	if err != nil {
		writeMediaAssetError(c, err)
		return
	}
	serveMediaAsset(c, asset)
}

// MediaAssetSource is the sole credentialless surface. Its scoped HMAC URL is
// generated only for an authorized driver and never returned by the upload API.
func MediaAssetSource(c *gin.Context) {
	query := c.Request.URL.Query()
	if len(query["expires"]) != 1 || len(query["signature"]) != 1 {
		writeMediaAssetError(c, model.ErrMediaAssetNotFound)
		return
	}
	asset, err := service.GetMediaAssetService().Source(c.Request.Context(), c.Param("asset_id"), c.Query("expires"), c.Query("signature"))
	if err != nil {
		writeMediaAssetError(c, err)
		return
	}
	serveMediaAsset(c, asset)
}

func serveMediaAsset(c *gin.Context, asset *model.MediaAsset) {
	byteRange := c.GetHeader("Range")
	if byteRange != "" {
		startValue, endValue, ok := strings.Cut(strings.TrimPrefix(byteRange, "bytes="), "-")
		start, startErr := strconv.ParseInt(startValue, 10, 64)
		end, endErr := strconv.ParseInt(endValue, 10, 64)
		valid := strings.HasPrefix(byteRange, "bytes=") && ok && !strings.Contains(byteRange, ",")
		if startValue == "" {
			valid = valid && endErr == nil && end > 0
		} else {
			valid = valid && startErr == nil && start >= 0 && start < asset.Size && (endValue == "" || endErr == nil && end >= start)
		}
		if !valid {
			c.Header("Content-Range", "bytes */"+strconv.FormatInt(asset.Size, 10))
			c.Status(http.StatusRequestedRangeNotSatisfiable)
			return
		}
	}
	object, err := service.GetMediaAssetService().Open(c.Request.Context(), asset, byteRange)
	if err != nil {
		writeMediaAssetError(c, err)
		return
	}
	defer object.Body.Close()
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Content-Disposition", `attachment; filename="media"`)
	c.Header("Accept-Ranges", "bytes")
	status := http.StatusOK
	if object.ContentRange != "" {
		status = http.StatusPartialContent
		c.Header("Content-Range", object.ContentRange)
	}
	if c.Request.Method == http.MethodHead {
		c.Header("Content-Type", asset.MIMEType)
		c.Header("Content-Length", strconv.FormatInt(object.Size, 10))
		c.Status(status)
		return
	}
	c.DataFromReader(status, object.Size, asset.MIMEType, object.Body, nil)
}

func writeMediaAssetError(c *gin.Context, err error) {
	status, code, message := http.StatusServiceUnavailable, "media_storage_unavailable", "Media storage is unavailable"
	var tooLarge *http.MaxBytesError
	switch {
	case errors.Is(err, model.ErrMediaAssetNotFound):
		status, code, message = http.StatusNotFound, "media_asset_not_found", "Media asset not found or expired"
	case errors.Is(err, service.ErrMediaAssetInvalid):
		status, code, message = http.StatusBadRequest, "invalid_media_file", "Upload exactly one supported image or video in the file field"
	case errors.Is(err, service.ErrMediaAssetTooLarge), errors.As(err, &tooLarge):
		status, code, message = http.StatusRequestEntityTooLarge, "media_file_too_large", "Media file exceeds the upload limit"
	case errors.Is(err, model.ErrMediaStorageQuota):
		status, code, message = http.StatusTooManyRequests, "media_storage_quota_exceeded", "Media storage quota exceeded"
	}
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
