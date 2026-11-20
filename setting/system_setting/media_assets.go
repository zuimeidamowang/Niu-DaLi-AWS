package system_setting

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// MediaAssetConfig is startup-only. It is independent of upstream channels and
// of generated task artifacts. No credentials belong in database options.
type MediaAssetConfig struct {
	Enabled       bool
	Bucket        string
	Region        string
	Endpoint      string
	Prefix        string
	PublicBaseURL string
	SigningKey    string
	ImageMaxBytes int64
	VideoMaxBytes int64
	UserMaxBytes  int64
}

func LoadMediaAssetConfig() (MediaAssetConfig, error) {
	c := MediaAssetConfig{
		Enabled:       os.Getenv("MEDIA_ASSET_ENABLED") == "true",
		Bucket:        os.Getenv("MEDIA_ASSET_S3_BUCKET"),
		Region:        os.Getenv("MEDIA_ASSET_S3_REGION"),
		Endpoint:      os.Getenv("MEDIA_ASSET_S3_ENDPOINT"),
		Prefix:        "input-media",
		PublicBaseURL: strings.TrimRight(os.Getenv("MEDIA_ASSET_PUBLIC_BASE_URL"), "/"),
		SigningKey:    os.Getenv("MEDIA_ASSET_SIGNING_KEY"),
		ImageMaxBytes: 50 << 20,
		VideoMaxBytes: 500 << 20,
		UserMaxBytes:  5 << 30,
	}
	for name, target := range map[string]*int64{
		"MEDIA_ASSET_MAX_IMAGE_BYTES": &c.ImageMaxBytes,
		"MEDIA_ASSET_MAX_VIDEO_BYTES": &c.VideoMaxBytes,
		"MEDIA_ASSET_USER_MAX_BYTES":  &c.UserMaxBytes,
	} {
		if value := os.Getenv(name); value != "" {
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil || parsed < 1 || parsed > 10<<30 {
				return c, errors.New("media asset byte limits must be between 1 and 10737418240")
			}
			*target = parsed
		}
	}
	if !c.Enabled {
		return c, nil
	}
	if !taskArtifactStoreBucketPattern.MatchString(c.Bucket) || strings.Contains(c.Bucket, "..") || !taskArtifactStoreRegionPattern.MatchString(c.Region) {
		return c, errors.New("media asset bucket and region are required")
	}
	if len(c.SigningKey) < 32 || len(c.SigningKey) > 1024 || strings.ContainsAny(c.SigningKey, "\r\n") {
		return c, errors.New("MEDIA_ASSET_SIGNING_KEY must contain at least 32 bytes")
	}
	public, err := url.Parse(c.PublicBaseURL)
	if err != nil || public.Scheme != "https" || public.Host == "" || public.User != nil || public.RawQuery != "" || public.Fragment != "" || (public.Path != "" && public.Path != "/") {
		return c, errors.New("MEDIA_ASSET_PUBLIC_BASE_URL must be an HTTPS URL without credentials, query or fragment")
	}
	if c.Endpoint != "" {
		endpoint, err := url.Parse(c.Endpoint)
		if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || (endpoint.Path != "" && endpoint.Path != "/") {
			return c, errors.New("invalid media asset S3 endpoint")
		}
	}
	if c.ImageMaxBytes > c.UserMaxBytes || c.VideoMaxBytes > c.UserMaxBytes {
		return c, errors.New("media asset user limit must cover the individual file limits")
	}
	return c, nil
}
