package controller_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// External DSNs must identify disposable databases. The same API and retention
// contracts run on real dialects; the HTTP fixture exercises the AWS SDK rather
// than replacing the storage adapter. Set the endpoint to exercise real S3 or a
// local S3-compatible server instead of the self-contained transport fixture.
func TestMediaAssetContracts(t *testing.T) {
	for _, dialect := range []struct {
		name common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "NIU_MEDIA_TEST_MYSQL_DSN"},
		{common.DatabaseTypePostgreSQL, "NIU_MEDIA_TEST_POSTGRES_DSN"},
	} {
		t.Run(string(dialect.name), func(t *testing.T) {
			var driver gorm.Dialector = sqlite.Open(filepath.Join(t.TempDir(), "media.db") + "?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate")
			if dialect.env != "" {
				dsn := os.Getenv(dialect.env)
				if dsn == "" {
					t.Skip(dialect.env + " is not configured")
				}
				if dialect.name == common.DatabaseTypeMySQL {
					driver = mysql.Open(dsn)
				} else {
					driver = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
				}
			}
			db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			oldDB, oldLogDB, oldType, oldLogType, oldRedis := model.DB, model.LOG_DB, common.MainDatabaseType(), common.LogDatabaseType(), common.RedisEnabled
			model.DB, common.RedisEnabled = db, false
			common.SetMainDatabaseType(dialect.name)
			t.Setenv("LOG_SQL_DSN", "")
			// Initialize the dialect-specific column names used by API-key auth.
			require.NoError(t, model.InitLogDB())
			t.Cleanup(func() {
				model.DB, common.RedisEnabled = oldDB, oldRedis
				common.SetMainDatabaseType(oldType)
				require.NoError(t, model.InitLogDB())
				model.LOG_DB = oldLogDB
				common.SetLogDatabaseType(oldLogType)
			})
			var version string
			versionSQL := "SELECT version()"
			if dialect.name == common.DatabaseTypeSQLite {
				versionSQL = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
			t.Log("database version:", version)

			for _, scenario := range []string{"fresh", "upgrade"} {
				t.Run(scenario, func(t *testing.T) {
					require.NoError(t, db.Migrator().DropTable(&model.MediaAssetTask{}, &model.MediaAsset{}, &model.Task{}, &model.Token{}, &model.UserGroupAssignment{}, &model.User{}))
					legacy := &model.Task{TaskID: "task_pre_media", UserId: 1001, Status: model.TaskStatusSuccess, FinishTime: 12345}
					if scenario == "upgrade" {
						// These unchanged released models predate media-asset tables.
						require.NoError(t, db.AutoMigrate(&model.User{}, &model.Task{}, &model.Token{}, &model.UserGroupAssignment{}))
						require.NoError(t, db.Create(legacy).Error)
					}
					for range 2 {
						require.NoError(t, db.AutoMigrate(&model.User{}, &model.Task{}, &model.Token{}, &model.UserGroupAssignment{}, &model.MediaAsset{}, &model.MediaAssetTask{}))
					}
					if scenario == "upgrade" {
						var preserved model.Task
						require.NoError(t, db.Where("task_id = ?", legacy.TaskID).First(&preserved).Error)
						assert.Equal(t, legacy.FinishTime, preserved.FinishTime)
						assert.Equal(t, legacy.Status, preserved.Status)
					}
					runMediaAssetContracts(t, db)
				})
			}
		})
	}
}

type mediaUploadPart struct {
	name, filename, mime string
	data                 []byte
}

type mediaFixture struct {
	t      *testing.T
	router *gin.Engine
	png    []byte
	logs   *bytes.Buffer
}

func (f *mediaFixture) request(method, path, token string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequest(method, path, body)
	if token != "" {
		req.Header.Set("Authorization", "Bearer sk-"+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, req)
	return response
}

func (f *mediaFixture) upload(parts ...mediaUploadPart) *httptest.ResponseRecorder {
	f.t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, item := range parts {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, item.name, item.filename))
		if item.mime != "" {
			header.Set("Content-Type", item.mime)
		}
		part, err := writer.CreatePart(header)
		require.NoError(f.t, err)
		_, err = part.Write(item.data)
		require.NoError(f.t, err)
	}
	require.NoError(f.t, writer.Close())
	return f.request(http.MethodPost, "/v1/media/assets", "mediaowner", &body, writer.FormDataContentType())
}

func runMediaAssetContracts(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	for _, user := range []model.User{
		{Id: 1001, Username: "mediaowner", Password: "fixture-only", Status: common.UserStatusEnabled, Group: "default", AffCode: "mediaowner"},
		{Id: 1002, Username: "mediaother", Password: "fixture-only", Status: common.UserStatusEnabled, Group: "default", AffCode: "mediaother"},
	} {
		require.NoError(t, db.Create(&user).Error)
	}
	for _, token := range []model.Token{
		{UserId: 1001, Key: "mediaowner", Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true},
		{UserId: 1002, Key: "mediaother", Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true},
		{UserId: 1001, Key: "mediaexpired", Status: common.TokenStatusEnabled, ExpiredTime: 1, UnlimitedQuota: true},
		{UserId: 1001, Key: "mediadisabled", Status: common.TokenStatusDisabled, ExpiredTime: -1, UnlimitedQuota: true},
	} {
		require.NoError(t, db.Create(&token).Error)
	}
	endpoint := os.Getenv("NIU_MEDIA_TEST_S3_ENDPOINT")
	if endpoint == "" {
		endpoint = newMediaS3Fixture(t)
	}
	for key, value := range map[string]string{
		"MEDIA_ASSET_ENABLED": "true", "MEDIA_ASSET_S3_BUCKET": "media-test", "MEDIA_ASSET_S3_REGION": "us-east-1", "MEDIA_ASSET_S3_ENDPOINT": endpoint,
		"MEDIA_ASSET_PUBLIC_BASE_URL": "https://media.example.test", "MEDIA_ASSET_SIGNING_KEY": "test-only-signing-secret-at-least-32-bytes",
		"MEDIA_ASSET_MAX_IMAGE_BYTES": "4096", "MEDIA_ASSET_MAX_VIDEO_BYTES": "8192", "MEDIA_ASSET_USER_MAX_BYTES": "32768",
		"AWS_ACCESS_KEY_ID": "testing", "AWS_SECRET_ACCESS_KEY": "testing", "AWS_SESSION_TOKEN": "", "AWS_EC2_METADATA_DISABLED": "true",
	} {
		t.Setenv(key, value)
	}
	req, err := http.NewRequest(http.MethodPut, endpoint+"/media-test", nil)
	require.NoError(t, err)
	bucketResponse, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, bucketResponse.Body.Close())
	require.True(t, bucketResponse.StatusCode < 300 || bucketResponse.StatusCode == http.StatusConflict)
	require.NoError(t, service.InitMediaAssets(ctx))
	t.Cleanup(func() { _ = os.Setenv("MEDIA_ASSET_ENABLED", "false"); _ = service.InitMediaAssets(ctx) })
	var imageBytes bytes.Buffer
	require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	logs := &bytes.Buffer{}
	oldWriter := gin.DefaultWriter
	gin.DefaultWriter = logs
	t.Cleanup(func() { gin.DefaultWriter = oldWriter })
	router := gin.New()
	middleware.SetUpLogger(router)
	router.Use(middleware.DecompressRequestMiddleware())
	router.Use(middleware.RouteTag("relay"))
	router.GET("/v1/media/assets/:asset_id/source", controller.MediaAssetSource)
	router.HEAD("/v1/media/assets/:asset_id/source", controller.MediaAssetSource)
	router.GET("/v1/media/assets/:asset_id/source/", controller.MediaAssetSource)
	router.HEAD("/v1/media/assets/:asset_id/source/", controller.MediaAssetSource)
	private := router.Group("/v1/media/assets", middleware.TokenAuth())
	private.POST("", controller.UploadMediaAsset)
	private.GET("/:asset_id", controller.RetrieveMediaAsset)
	private.GET("/:asset_id/content", controller.MediaAssetContent)
	private.HEAD("/:asset_id/content", controller.MediaAssetContent)
	f := &mediaFixture{t: t, router: router, png: imageBytes.Bytes(), logs: logs}
	good := mediaUploadPart{"file", "input.png", "image/png", f.png}

	response := f.upload(good)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var asset struct {
		ID, Object, Type, MIMEType, Status string
		Size, ExpiresAt                    int64
	}
	var metadata map[string]any
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &metadata))
	require.Len(t, metadata, 7)
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &asset))
	require.Regexp(t, `^media_[A-Za-z0-9_-]{32}$`, asset.ID)
	assert.Equal(t, "image", asset.Type)
	assert.Equal(t, "media.asset", asset.Object)
	assert.Equal(t, "ready", metadata["status"])
	assert.Equal(t, "image/png", metadata["mime_type"])
	assert.InDelta(t, time.Now().Unix()+24*60*60, metadata["expires_at"], 3)
	assert.Equal(t, int64(len(f.png)), asset.Size)
	assert.NotContains(t, response.Body.String(), "input-media")
	assert.NotContains(t, response.Body.String(), "signature")
	assert.NotContains(t, response.Body.String(), endpoint)
	for _, key := range []string{"", "invalid", "mediaexpired", "mediadisabled"} {
		assert.Equal(t, http.StatusUnauthorized, f.request(http.MethodGet, "/v1/media/assets/"+asset.ID, key, nil, "").Code)
	}
	assert.Equal(t, http.StatusOK, f.request(http.MethodGet, "/v1/media/assets/"+asset.ID, "mediaowner", nil, "").Code)
	assert.Equal(t, http.StatusNotFound, f.request(http.MethodGet, "/v1/media/assets/"+asset.ID, "mediaother", nil, "").Code)
	assert.Equal(t, http.StatusNotFound, f.request(http.MethodGet, "/v1/media/assets/"+asset.ID+"/content", "mediaother", nil, "").Code)
	content := f.request(http.MethodGet, "/v1/media/assets/"+asset.ID+"/content", "mediaowner", nil, "")
	assert.Equal(t, f.png, content.Body.Bytes())
	assert.Equal(t, "nosniff", content.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "no-referrer", content.Header().Get("Referrer-Policy"))
	assert.Equal(t, "bytes", content.Header().Get("Accept-Ranges"))

	for _, tc := range []struct {
		name   string
		parts  []mediaUploadPart
		status int
	}{
		{"empty", nil, 400},
		{"wrong field", []mediaUploadPart{{"other", "input.png", "image/png", f.png}}, 400},
		{"extra field", []mediaUploadPart{good, {"model", "", "text/plain", []byte("x")}}, 400},
		{"repeated file", []mediaUploadPart{good, good}, 400},
		{"unsupported suffix", []mediaUploadPart{{"file", "input.svg", "image/svg+xml", []byte("<svg/>")}}, 400},
		{"forged MIME", []mediaUploadPart{{"file", "input.png", "video/mp4", f.png}}, 400},
		{"forged bytes", []mediaUploadPart{{"file", "input.png", "image/png", []byte("not a PNG")}}, 400},
		{"zero bytes", []mediaUploadPart{{"file", "input.png", "image/png", nil}}, 400},
		{"oversize", []mediaUploadPart{{"file", "input.png", "image/png", bytes.Repeat([]byte{0}, 4097)}}, 413},
	} {
		t.Run(tc.name, func(t *testing.T) { r := f.upload(tc.parts...); assert.Equal(t, tc.status, r.Code, r.Body.String()) })
	}
	var readyCount int64
	require.NoError(t, db.Model(&model.MediaAsset{}).Count(&readyCount).Error)
	assert.Equal(t, int64(1), readyCount, "invalid uploads release reservations")

	resolved, err := service.ResolveMediaAssets(ctx, 1001, []string{asset.ID})
	require.NoError(t, err)
	source, err := url.Parse(resolved[asset.ID].URL)
	require.NoError(t, err)
	assert.Equal(t, "asset_file:"+asset.ID, resolved[asset.ID].FileRef)
	assert.Equal(t, "media.example.test", source.Host)
	sourceResponse := f.request(http.MethodGet, source.RequestURI(), "", nil, "")
	require.Equal(t, 200, sourceResponse.Code, sourceResponse.Body.String())
	assert.Equal(t, f.png, sourceResponse.Body.Bytes())
	head := f.request(http.MethodHead, source.RequestURI(), "", nil, "")
	assert.Equal(t, "", head.Body.String())
	assert.Equal(t, strconv.Itoa(len(f.png)), head.Header().Get("Content-Length"))
	rangeRequest := httptest.NewRequest(http.MethodGet, source.RequestURI(), nil)
	rangeRequest.Header.Set("Range", "bytes=0-7")
	ranged := httptest.NewRecorder()
	router.ServeHTTP(ranged, rangeRequest)
	assert.Equal(t, 206, ranged.Code)
	assert.Equal(t, f.png[:8], ranged.Body.Bytes())
	assert.Equal(t, fmt.Sprintf("bytes 0-7/%d", len(f.png)), ranged.Header().Get("Content-Range"))
	for _, path := range []string{source.Path, source.RequestURI() + "&signature=duplicate", source.Path + "?expires=1&signature=" + source.Query().Get("signature"), strings.Replace(source.RequestURI(), "signature=", "signature=00", 1)} {
		assert.Equal(t, 404, f.request(http.MethodGet, path, "", nil, "").Code)
	}
	assert.NotContains(t, logs.String(), source.Query().Get("signature"))
	redirected := f.request(http.MethodGet, source.Path+"/?"+source.RawQuery, "", nil, "")
	assert.Equal(t, http.StatusOK, redirected.Code)
	assert.NotContains(t, logs.String(), source.Query().Get("signature"))

	// Remaining capacity accepts a small valid file and rejects one that would
	// exceed it, despite both being below the individual file limit.
	quotaAsset := &model.MediaAsset{ID: "quota_reservation", UserID: 1001, Size: 32768 - int64(len(f.png))*2, State: model.MediaAssetUploading}
	require.NoError(t, db.Create(quotaAsset).Error)
	assert.Equal(t, 201, f.upload(good).Code)
	assert.Equal(t, 429, f.upload(good).Code)
	require.NoError(t, db.Delete(quotaAsset).Error)

	{
		// These real concurrent transactions prove the shared user lock prevents
		// two in-flight reservations from spending the same remaining capacity.
		var wg sync.WaitGroup
		results := make(chan *model.MediaAsset, 2)
		errors := make(chan error, 2)
		for i := range 2 {
			wg.Go(func() {
				a := &model.MediaAsset{ID: fmt.Sprintf("concurrent_%d", i), UserID: 1002, Size: 80, State: model.MediaAssetUploading}
				errors <- model.ReserveMediaAsset(ctx, a, 100)
				results <- a
			})
		}
		wg.Wait()
		close(results)
		close(errors)
		for err := range errors {
			require.NoError(t, err)
		}
		var reserved int64
		for a := range results {
			reserved += a.Size
		}
		assert.Equal(t, int64(100), reserved)
		require.NoError(t, db.Where("user_id = ?", 1002).Delete(&model.MediaAsset{}).Error)
	}

	// Retention follows the last associated task. The same asset can be reused;
	// active tasks and submission leases protect it beyond its original expiry.
	now := time.Now().Unix()
	require.NoError(t, service.LeaseMediaAssets(ctx, 1001, "task_media_active", []string{asset.ID}))
	require.NoError(t, db.Model(&model.MediaAsset{}).Where("id = ?", asset.ID).Update("created_at", now-2*model.MediaAssetRetention).Error)
	leased, err := model.GetMediaAsset(ctx, 1001, asset.ID)
	require.NoError(t, err)
	assert.Zero(t, leased.ExpiresAt)
	_, err = model.ClaimExpiredMediaAsset(ctx, asset.ID, now)
	assert.ErrorIs(t, err, model.ErrMediaAssetNotFound)
	task := &model.Task{TaskID: "task_media_active", UserId: 1001, Status: model.TaskStatusInProgress, UpdatedAt: now}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Model(&model.MediaAssetTask{}).Where("task_id = ?", task.TaskID).Update("created_at", now-model.MediaAssetLeaseTTL-1).Error)
	_, err = model.ClaimExpiredMediaAsset(ctx, asset.ID, now+10*model.MediaAssetRetention)
	assert.ErrorIs(t, err, model.ErrMediaAssetNotFound)
	require.NoError(t, service.CancelMediaAssetTask(ctx, task.TaskID))
	var referenceCount int64
	require.NoError(t, db.Model(&model.MediaAssetTask{}).Where("task_id = ?", task.TaskID).Count(&referenceCount).Error)
	assert.Equal(t, int64(1), referenceCount, "cancel cannot remove references for a durable task")
	require.NoError(t, db.Model(task).Updates(map[string]any{"status": model.TaskStatusSuccess, "finish_time": now, "updated_at": now}).Error)
	terminal, err := model.GetMediaAsset(ctx, 1001, asset.ID)
	require.NoError(t, err)
	assert.Equal(t, now+model.MediaAssetRetention, terminal.ExpiresAt)
	require.NoError(t, service.LeaseMediaAssets(ctx, 1001, "task_media_reuse", []string{asset.ID, asset.ID}))
	require.NoError(t, db.Model(&model.MediaAssetTask{}).Where("task_id = ?", "task_media_reuse").Count(&referenceCount).Error)
	assert.Equal(t, int64(1), referenceCount, "repeated references preserve the composite primary-key constraint")
	reused := &model.Task{TaskID: "task_media_reuse", UserId: 1001, Status: model.TaskStatusFailure, FinishTime: now + 100, UpdatedAt: now + 100}
	require.NoError(t, db.Create(reused).Error)
	_, err = model.ClaimExpiredMediaAsset(ctx, asset.ID, now+model.MediaAssetRetention+50)
	assert.ErrorIs(t, err, model.ErrMediaAssetNotFound)
	_, err = model.ClaimExpiredMediaAsset(ctx, asset.ID, now+model.MediaAssetRetention+101)
	require.NoError(t, err)
	assert.ErrorIs(t, service.LeaseMediaAssets(ctx, 1001, "task_too_late", []string{asset.ID}), model.ErrMediaAssetNotFound)
	_, err = model.GetMediaAsset(ctx, 1001, asset.ID)
	assert.ErrorIs(t, err, model.ErrMediaAssetNotFound)
	require.NoError(t, service.GetMediaAssetService().Cleanup(ctx, now+model.MediaAssetRetention+101))
	var count int64
	require.NoError(t, db.Model(&model.MediaAsset{}).Where("id = ?", asset.ID).Count(&count).Error)
	assert.Zero(t, count)
	assert.Equal(t, 404, f.request(http.MethodGet, source.RequestURI(), "", nil, "").Code)

	stale := &model.MediaAsset{ID: "stale_lease", UserID: 1001, ObjectKey: "input-media/stale", Size: 1, State: model.MediaAssetReady, CreatedAt: now - 2*model.MediaAssetRetention}
	require.NoError(t, db.Create(stale).Error)
	require.NoError(t, db.Create(&model.MediaAssetTask{AssetID: stale.ID, TaskID: "task_crashed", UserID: 1001, CreatedAt: now - model.MediaAssetLeaseTTL - 1}).Error)
	_, err = model.ClaimExpiredMediaAsset(ctx, stale.ID, now)
	require.NoError(t, err, "abandoned submission leases eventually release originals")
	require.NoError(t, service.GetMediaAssetService().Cleanup(ctx, now+2*model.MediaAssetRetention))
}

func newMediaS3Fixture(t *testing.T) string {
	t.Helper()
	var mutex sync.Mutex
	objects := map[string][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/media-test" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/media-test/input-media/") {
			http.NotFound(w, r)
			return
		}
		mutex.Lock()
		defer mutex.Unlock()
		switch r.Method {
		case http.MethodPut:
			if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") || r.Header.Get("X-Amz-Server-Side-Encryption") != "AES256" {
				w.WriteHeader(403)
				return
			}
			data, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(500)
				return
			}
			objects[r.URL.Path] = data
			w.WriteHeader(200)
		case http.MethodGet:
			data, ok := objects[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			http.ServeContent(w, r, "media", time.Time{}, bytes.NewReader(data))
		case http.MethodDelete:
			delete(objects, r.URL.Path)
			w.WriteHeader(204)
		default:
			w.WriteHeader(405)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}
