package lib

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
)

var s3Client *s3.S3
var s3ClientLock sync.RWMutex
var s3ClientsRegional = make(map[string]*s3.S3)

func S3Client() *s3.S3 {
	s3ClientLock.Lock()
	defer s3ClientLock.Unlock()
	if s3Client == nil {
		s3Client = s3.New(Session())
	}
	return s3Client
}

func S3ClientRegion(region string) (*s3.S3, error) {
	s3ClientLock.Lock()
	defer s3ClientLock.Unlock()
	s3Client, ok := s3ClientsRegional[region]
	if !ok {
		sess, err := SessionRegion(region)
		if err != nil {
			return nil, err
		}
		s3Client = s3.New(sess)
		s3ClientsRegional[region] = s3Client
	}
	return s3Client, nil
}

func S3ClientRegionMust(region string) *s3.S3 {
	client, err := S3ClientRegion(region)
	if err != nil {
		panic(err)
	}
	return client
}

var s3BucketRegionLock sync.RWMutex
var s3BucketRegion = make(map[string]string)

func S3BucketRegion(bucket string) (string, error) {
	s3BucketRegionLock.Lock()
	defer s3BucketRegionLock.Unlock()
	region, ok := s3BucketRegion[bucket]
	if !ok {
		resp, err := http.Head(fmt.Sprintf("https://%s.s3.amazonaws.com", bucket))
		if err != nil {
			return "", err
		}
		err = resp.Body.Close()
		if err != nil {
			return "", err
		}
		switch resp.StatusCode {
		case 200:
		case 404:
			err := awserr.New(s3.ErrCodeNoSuchBucket, bucket, nil)
			Logger.Println("error:", err)
			return "", err
		default:
			err := fmt.Errorf("http %d for %s", resp.StatusCode, bucket)
			Logger.Println("error:", err)
			return "", err
		}
		region = resp.Header.Get("x-amz-bucket-region")
		if region == "" {
			return "", fmt.Errorf("empty x-amz-bucket-region for bucket: %s", bucket)
		}
		s3BucketRegion[bucket] = region
	}
	return region, nil
}

func S3ClientBucketRegion(bucket string) (*s3.S3, error) {
	var s3Client *s3.S3
	err := Retry(context.Background(), func() error {
		var region string
		var err error
		region, err = S3BucketRegion(bucket)
		if err != nil {
			return err
		}
		s3Client, err = S3ClientRegion(region)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s3Client, nil
}

func S3ClientBucketRegionMust(bucket string) *s3.S3 {
	client, err := S3ClientBucketRegion(bucket)
	if err != nil {
		panic(err)
	}
	return client
}

type s3EnsureInput struct {
	name       string
	region     string
	acl        string
	versioning bool
	encryption bool
}

func S3EnsureInput(name string, attrs []string) (*s3EnsureInput, error) {
	input := &s3EnsureInput{
		name:       name,
		region:     Region(),
		acl:        "private",
		versioning: false,
		encryption: true,
	}
	for _, line := range attrs {
		line = strings.ToLower(line)
		attr, value, err := splitOnce(line, "=")
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		switch attr {
		case "acl":
			switch value {
			case "public", "private":
				input.acl = value
			default:
				err := fmt.Errorf("unknown s3 attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
		case "versioning":
			switch value {
			case "true", "false":
				input.versioning = value == "true"
			default:
				err := fmt.Errorf("unknown s3 attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
		case "encryption":
			switch value {
			case "true", "false":
				input.encryption = value == "true"
			default:
				err := fmt.Errorf("unknown s3 attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
		default:
			err := fmt.Errorf("unknown s3 attr: %s", line)
			Logger.Println("error:", err)
			return nil, err
		}
	}
	return input, nil
}

func S3Ensure(ctx context.Context, input *s3EnsureInput, preview bool) error {
	region, err := S3BucketRegion(input.name)
	exists := false
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != s3.ErrCodeNoSuchBucket {
			Logger.Println("error:", err)
			return err
		}
		if !preview {
			_, err := S3Client().CreateBucketWithContext(ctx, &s3.CreateBucketInput{
				Bucket: aws.String(input.name),
				CreateBucketConfiguration: &s3.CreateBucketConfiguration{
					LocationConstraint: aws.String(input.region),
				},
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			exists = true
		}
		Logger.Println(PreviewString(preview)+"s3 created bucket:", input.name, input.region)
	} else {
		if region != input.region {
			err := fmt.Errorf("s3 region can only be set at creation time for %s: %s != %s", input.name, region, input.region)
			Logger.Println("error:", err)
			return err
		}
		exists = true
	}
	//
	if exists {
		//
		account, err := StsAccount(ctx)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		//
		pabOut, err := S3Client().GetPublicAccessBlockWithContext(ctx, &s3.GetPublicAccessBlockInput{
			Bucket:              aws.String(input.name),
			ExpectedBucketOwner: aws.String(account),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		needsUpdate := false
		conf := pabOut.PublicAccessBlockConfiguration
		if input.acl == "private" {
			if !(*conf.BlockPublicAcls && *conf.IgnorePublicAcls && *conf.BlockPublicPolicy && *conf.RestrictPublicBuckets) {
				needsUpdate = true
			}
		} else {
			if *conf.BlockPublicAcls || *conf.IgnorePublicAcls || *conf.BlockPublicPolicy || *conf.RestrictPublicBuckets {
				needsUpdate = true
			}
		}
		if needsUpdate {
			if !preview {
				_, err := S3Client().PutPublicAccessBlockWithContext(ctx, &s3.PutPublicAccessBlockInput{
					Bucket:              aws.String(input.name),
					ExpectedBucketOwner: aws.String(account),
					PublicAccessBlockConfiguration: &s3.PublicAccessBlockConfiguration{
						BlockPublicAcls:       aws.Bool(input.acl == "private"),
						IgnorePublicAcls:      aws.Bool(input.acl == "private"),
						BlockPublicPolicy:     aws.Bool(input.acl == "private"),
						RestrictPublicBuckets: aws.Bool(input.acl == "private"),
					},
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"s3 updated public access block for %s: %s", input.name, input.acl)
		}
		//
		needsUpdate = false
		versionOut, err := S3Client().GetBucketVersioningWithContext(ctx, &s3.GetBucketVersioningInput{
			Bucket:              aws.String(input.name),
			ExpectedBucketOwner: aws.String(account),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		if (input.versioning && *versionOut.Status != s3.BucketVersioningStatusEnabled) ||
			(!input.versioning && *versionOut.Status != s3.BucketVersioningStatusSuspended) {
			needsUpdate = true
		}
		if needsUpdate {
			if !preview {
				status := s3.BucketVersioningStatusSuspended
				if input.versioning {
					status = s3.BucketVersioningStatusEnabled
				}
				_, err := S3Client().PutBucketVersioningWithContext(ctx, &s3.PutBucketVersioningInput{
					Bucket:              aws.String(input.name),
					ExpectedBucketOwner: aws.String(account),
					VersioningConfiguration: &s3.VersioningConfiguration{
						Status: aws.String(status),
					},
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"s3 updated versioning for %s: %v", input.name, input.versioning)
		}
		//
		needsUpdate = false
		encryptedConfig := &s3.ServerSideEncryptionConfiguration{
			Rules: []*s3.ServerSideEncryptionRule{{
				ApplyServerSideEncryptionByDefault: &s3.ServerSideEncryptionByDefault{
					SSEAlgorithm: aws.String(s3.ServerSideEncryptionAes256),
				},
			}},
		}
		encOut, err := S3Client().GetBucketEncryptionWithContext(ctx, &s3.GetBucketEncryptionInput{
			Bucket:              aws.String(input.name),
			ExpectedBucketOwner: aws.String(account),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		if (input.encryption && !reflect.DeepEqual(encOut.ServerSideEncryptionConfiguration, encryptedConfig)) ||
			(!input.encryption && len(encOut.ServerSideEncryptionConfiguration.Rules) != 0) {
			needsUpdate = true
		}
		if needsUpdate {
			if !preview {
				if input.encryption {
					_, err := S3Client().PutBucketEncryptionWithContext(ctx, &s3.PutBucketEncryptionInput{
						Bucket:                            aws.String(input.name),
						ExpectedBucketOwner:               aws.String(account),
						ServerSideEncryptionConfiguration: encryptedConfig,
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				} else {
					_, err := S3Client().DeleteBucketEncryptionWithContext(ctx, &s3.DeleteBucketEncryptionInput{
						Bucket:              aws.String(input.name),
						ExpectedBucketOwner: aws.String(account),
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
			}
			Logger.Println(PreviewString(preview)+"s3 updated encryption for %s: %v", input.name, input.encryption)
		}
	}
	//
	return nil
}
