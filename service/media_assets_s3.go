package service

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type s3MediaAssetStorage struct {
	client *s3.Client
	bucket string
}

func newS3MediaAssetStorage(ctx context.Context, settings system_setting.MediaAssetConfig) (MediaAssetStorage, error) {
	// The standard AWS chain supports EC2/ECS workload roles and refreshes their
	// temporary credentials. It also supports environment credentials for local
	// S3-compatible testing, without putting credentials in channel options.
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(settings.Region), config.WithHTTPClient(&http.Client{Timeout: 5 * time.Minute}))
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		if settings.Endpoint != "" {
			options.BaseEndpoint = aws.String(settings.Endpoint)
			options.UsePathStyle = true
		}
	})
	return &s3MediaAssetStorage{client: client, bucket: settings.Bucket}, nil
}

func (s *s3MediaAssetStorage) Put(ctx context.Context, key string, body io.ReadSeeker, size int64, mimeType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), Body: body, ContentLength: aws.Int64(size), ContentType: aws.String(mimeType), ServerSideEncryption: types.ServerSideEncryptionAes256})
	return err
}

func (s *s3MediaAssetStorage) Get(ctx context.Context, key, byteRange string) (*MediaAssetObject, error) {
	input := &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}
	if byteRange != "" {
		input.Range = aws.String(byteRange)
	}
	output, err := s.client.GetObject(ctx, input)
	if err != nil {
		return nil, err
	}
	return &MediaAssetObject{Body: output.Body, Size: aws.ToInt64(output.ContentLength), ContentRange: aws.ToString(output.ContentRange)}, nil
}

func (s *s3MediaAssetStorage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}
