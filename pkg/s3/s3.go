package s3

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/sirupsen/logrus"
)

const (
	bucketName = "charon-clusters-config"
)

type S3Client struct {
	client *s3.Client
}

func NewS3Client() (*S3Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %v", err)
	}

	client := s3.NewFromConfig(cfg)
	return &S3Client{client: client}, nil
}

func (s *S3Client) UploadDirectory(localPath, s3Path string) error {
	return filepath.Walk(localPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(localPath, path)
		if err != nil {
			return err
		}

		s3Key := filepath.Join(s3Path, relPath)
		return s.uploadFile(path, s3Key)
	})
}

func (s *S3Client) uploadFile(localPath, s3Key string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %v", localPath, err)
	}
	defer file.Close()

	_, err = s.client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(s3Key),
		Body:   file,
	})

	if err != nil {
		return fmt.Errorf("failed to upload file %s to S3: %v", localPath, err)
	}

	logrus.Infof("Uploaded %s to s3://%s/%s", localPath, bucketName, s3Key)
	return nil
}

func (s *S3Client) UploadFile(localPath, s3Key string) error {
	return s.uploadFile(localPath, s3Key)
}
