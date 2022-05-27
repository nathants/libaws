package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
)

const (
	s3MetricsId = "S3MetricsEntireBucket"
)

var s3Client *s3.S3
var s3ClientLock sync.RWMutex
var s3ClientsRegional = make(map[string]*s3.S3)

func S3ClientExplicit(accessKeyID, accessKeySecret, region string) *s3.S3 {
	return s3.New(SessionExplicit(accessKeyID, accessKeySecret, region))
}

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
		cacheFile := "/tmp/aws.s3.bucket.region=" + bucket
		data, err := ioutil.ReadFile(cacheFile)
		if err == nil {
			region = string(data)
		} else {
			if doDebug {
				d := &Debug{start: time.Now(), name: "S3BucketRegion"}
				defer d.Log()
			}
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
			case 400:
			case 403:
			case 404:
				err := awserr.New(s3.ErrCodeNoSuchBucket, bucket, nil)
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
			err = ioutil.WriteFile(cacheFile, []byte(region), os.ModePerm)
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
		}
		s3BucketRegion[bucket] = region
	}
	return region, nil
}

func S3ClientBucketRegion(bucket string) (*s3.S3, error) {
	var s3Client *s3.S3
	var expectedErr error
	err := Retry(context.Background(), func() error {
		var region string
		var err error
		region, err = S3BucketRegion(bucket)
		if err != nil {
			aerr, ok := err.(awserr.Error)
			if ok && aerr.Code() == s3.ErrCodeNoSuchBucket {
				expectedErr = err
				return nil
			}
			Logger.Println("error:", err)
			return err
		}
		s3Client, err = S3ClientRegion(region)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		return nil
	})
	if expectedErr != nil {
		return nil, expectedErr
	}
	if err != nil {
		Logger.Println("error:", err)
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
	infraSetName string
	name         string
	acl          string
	versioning   bool
	encryption   bool
	metrics      bool
	cors         bool
	ttlDays      int

	// NOTE: you almost never want to use this, danger close.
	// currently used in cmd/vpc/ensure_flowlogs.go
	//
	CustomPolicy *string // use this when you need to specify a custom bucket policy
}

func s3EnsureInputDefault() *s3EnsureInput {
	return &s3EnsureInput{
		acl:        "private",
		versioning: false,
		encryption: true,
		metrics:    true,
		cors:       false,
		ttlDays:    0,
	}
}

func S3EnsureInput(infraSetName, bucketName string, attrs []string) (*s3EnsureInput, error) {
	input := s3EnsureInputDefault()
	input.infraSetName = infraSetName
	input.name = bucketName
	for _, line := range attrs {
		line = strings.ToLower(line)
		attr, value, err := SplitOnce(line, "=")
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		switch attr {
		case "ttldays":
			input.ttlDays = Atoi(value)
		case "cors":
			switch value {
			case "true", "false":
				input.cors = value == "true"
			default:
				err := fmt.Errorf("s3 unknown attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
		case "acl":
			switch value {
			case "public", "private":
				input.acl = value
			default:
				err := fmt.Errorf("s3 unknown attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
		case "versioning":
			switch value {
			case "true", "false":
				input.versioning = value == "true"
			default:
				err := fmt.Errorf("s3 unknown attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
		case "metrics":
			switch value {
			case "true", "false":
				input.metrics = value == "true"
			default:
				err := fmt.Errorf("s3 unknown attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
		default:
			err := fmt.Errorf("s3 unknown attr: %s", line)
			Logger.Println("error:", err)
			return nil, err
		}
	}
	return input, nil
}

func s3PublicPolicy(bucket string) IamPolicyDocument {
	return IamPolicyDocument{
		Version: "2012-10-17",
		Id:      "S3PublicPolicy",
		Statement: []IamStatementEntry{{
			Sid:       "S3PublicPolicy",
			Effect:    "Allow",
			Principal: "*",
			Action:    "s3:GetObject",
			Resource:  fmt.Sprintf("arn:aws:s3:::%s/*", bucket),
		}},
	}
}

var s3Cors = []*s3.CORSRule{{
	AllowedHeaders: []*string{aws.String("Authorization"), aws.String("Range")},
	AllowedMethods: []*string{aws.String("GET"), aws.String("PUT"), aws.String("POST"), aws.String("HEAD")},
	AllowedOrigins: []*string{aws.String("*")},
	ExposeHeaders:  []*string{aws.String("Content-Length"), aws.String("Content-Type"), aws.String("ETag")},
	MaxAgeSeconds:  aws.Int64(int64(3000)),
}}

var s3EncryptionConfig = &s3.ServerSideEncryptionConfiguration{
	Rules: []*s3.ServerSideEncryptionRule{{
		ApplyServerSideEncryptionByDefault: &s3.ServerSideEncryptionByDefault{
			SSEAlgorithm: aws.String(s3.ServerSideEncryptionAes256),
		},
		BucketKeyEnabled: aws.Bool(false),
	}},
}

func S3Ensure(ctx context.Context, input *s3EnsureInput, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "S3Ensure"}
		defer d.Log()
	}
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	_, err = S3Client().HeadBucketWithContext(ctx, &s3.HeadBucketInput{
		Bucket:              aws.String(input.name),
		ExpectedBucketOwner: aws.String(account),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "NotFound" {
			Logger.Println("error:", err)
			return err
		}
		if !preview {
			_, err := S3Client().CreateBucketWithContext(ctx, &s3.CreateBucketInput{
				Bucket: aws.String(input.name),
				CreateBucketConfiguration: &s3.CreateBucketConfiguration{
					LocationConstraint: aws.String(Region()),
				},
			})
			if err != nil {
				aerr, ok := err.(awserr.Error)
				if !ok || aerr.Code() != s3.ErrCodeBucketAlreadyOwnedByYou {
					Logger.Println("error:", err)
					return err
				}
			}
		}
		Logger.Println(PreviewString(preview)+"created bucket:", input.name)
	}
	exists := false
	getTagOut, err := S3Client().GetBucketTaggingWithContext(ctx, &s3.GetBucketTaggingInput{
		Bucket:              aws.String(input.name),
		ExpectedBucketOwner: aws.String(account),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || !Contains([]string{"NoSuchBucket", "NoSuchTagSet"}, aerr.Code()) {
			Logger.Println("error:", err)
			return err
		}
	} else {
		for _, tag := range getTagOut.TagSet {
			if *tag.Key == infraSetTagName && *tag.Value == input.infraSetName {
				exists = true
				break
			}
		}
	}
	if !exists {
		if !preview {
			_, err := S3Client().PutBucketTaggingWithContext(ctx, &s3.PutBucketTaggingInput{
				Bucket:              aws.String(input.name),
				ExpectedBucketOwner: aws.String(account),
				Tagging: &s3.Tagging{
					TagSet: []*s3.Tag{{
						Key:   aws.String(infraSetTagName),
						Value: aws.String(input.infraSetName),
					}},
				},
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created bucket tags for:", input.name)
	}
	exists = true
	pabOut, err := S3Client().GetPublicAccessBlockWithContext(ctx, &s3.GetPublicAccessBlockInput{
		Bucket:              aws.String(input.name),
		ExpectedBucketOwner: aws.String(account),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || !Contains([]string{"NoSuchPublicAccessBlockConfiguration", "NoSuchBucket"}, aerr.Code()) {
			Logger.Println("error:", err)
			return err
		}
		exists = false
	}
	if exists {
		conf := pabOut.PublicAccessBlockConfiguration
		if input.acl == "private" {
			if !(*conf.BlockPublicAcls && *conf.IgnorePublicAcls && *conf.BlockPublicPolicy && *conf.RestrictPublicBuckets) {
				err := fmt.Errorf("acl public/private can only be set at bucket creation")
				Logger.Println("error:", err)
				return err
			}
		} else {
			if *conf.BlockPublicAcls || *conf.IgnorePublicAcls || *conf.BlockPublicPolicy || *conf.RestrictPublicBuckets {
				err := fmt.Errorf("acl public/private can only be set at bucket creation")
				Logger.Println("error:", err)
				return err
			}
		}
	}
	if !exists {
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
		Logger.Printf(PreviewString(preview)+"created public access block for %s: %s\n", input.name, input.acl)
	}
	policyOut, err := S3Client().GetBucketPolicyWithContext(ctx, &s3.GetBucketPolicyInput{
		Bucket:              aws.String(input.name),
		ExpectedBucketOwner: aws.String(account),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || !Contains([]string{"NoSuchBucketPolicy", "NoSuchBucket"}, aerr.Code()) {
			Logger.Println("error:", err)
			return err
		}
		if input.acl == "public" || input.CustomPolicy != nil {
			var aclName string
			var policyBytes []byte
			if input.CustomPolicy != nil {
				aclName = "custom"
				policyBytes = []byte(*input.CustomPolicy)
			} else {
				aclName = "public"
				policyBytes, err = json.Marshal(s3PublicPolicy(input.name))
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			if !preview {
				_, err = S3Client().PutBucketPolicyWithContext(ctx, &s3.PutBucketPolicyInput{
					Bucket:              aws.String(input.name),
					ExpectedBucketOwner: aws.String(account),
					Policy:              aws.String(string(policyBytes)),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"put acl:", input.name, aclName)
		}
	} else if input.acl == "private" {
		if input.CustomPolicy != nil {
			policy := IamPolicyDocument{}
			err = json.Unmarshal([]byte(*policyOut.Policy), &policy)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			expectedPolicy := IamPolicyDocument{}
			err = json.Unmarshal([]byte(*input.CustomPolicy), &expectedPolicy)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			if !reflect.DeepEqual(expectedPolicy, policy) {
				err := fmt.Errorf("private bucket policy is misconfigured for bucket: %s\n%s != %s", input.name, Pformat(policy), Pformat(expectedPolicy))
				Logger.Println("error:", err)
				return err
			}
		} else {
			err := fmt.Errorf("no bucket policy should exist for private bucket: %s", input.name)
			Logger.Println("error:", err)
			return err
		}
	} else {
		policy := IamPolicyDocument{}
		err = json.Unmarshal([]byte(*policyOut.Policy), &policy)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		expectedPolicy := IamPolicyDocument{}
		if input.CustomPolicy == nil {
			expectedPolicy = s3PublicPolicy(input.name)
		} else {
			err = json.Unmarshal([]byte(*input.CustomPolicy), &expectedPolicy)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		if !reflect.DeepEqual(expectedPolicy, policy) {
			err := fmt.Errorf("public bucket policy is misconfigured for bucket: %s\n%s != %s", input.name, Pformat(policy), Pformat(expectedPolicy))
			Logger.Println("error:", err)
			return err
		}
	}
	corsOut, err := S3Client().GetBucketCorsWithContext(ctx, &s3.GetBucketCorsInput{
		Bucket:              aws.String(input.name),
		ExpectedBucketOwner: aws.String(account),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || !Contains([]string{"NoSuchCORSConfiguration", "NoSuchBucket"}, aerr.Code()) {
			Logger.Println("error:", err)
			return err
		}
		if input.cors {
			if !preview {
				_, err := S3Client().PutBucketCorsWithContext(ctx, &s3.PutBucketCorsInput{
					Bucket:              aws.String(input.name),
					ExpectedBucketOwner: aws.String(account),
					CORSConfiguration:   &s3.CORSConfiguration{CORSRules: s3Cors},
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"put cors:", input.name)
		}
	} else if !input.cors {
		if !preview {
			_, err := S3Client().DeleteBucketCorsWithContext(ctx, &s3.DeleteBucketCorsInput{
				Bucket:              aws.String(input.name),
				ExpectedBucketOwner: aws.String(account),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"delete cors:", input.name)
	} else if !reflect.DeepEqual(corsOut.CORSRules, s3Cors) {
		err := fmt.Errorf("bucket cors config is misconfigured for bucket: %s", input.name)
		Logger.Println("error:", err)
		return err
	}
	needsUpdate := false
	versionOut, err := S3Client().GetBucketVersioningWithContext(ctx, &s3.GetBucketVersioningInput{
		Bucket:              aws.String(input.name),
		ExpectedBucketOwner: aws.String(account),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "NoSuchBucket" {
			Logger.Println("error:", err)
			return err
		}
	}
	if (input.versioning && (versionOut.Status == nil || *versionOut.Status != s3.BucketVersioningStatusEnabled)) ||
		(!input.versioning && versionOut.Status != nil && *versionOut.Status != s3.BucketVersioningStatusSuspended) {
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
		Logger.Printf(PreviewString(preview)+"updated versioning for %s: %v\n", input.name, input.versioning)
	}
	needsUpdate = false
	encOut, err := S3Client().GetBucketEncryptionWithContext(ctx, &s3.GetBucketEncryptionInput{
		Bucket:              aws.String(input.name),
		ExpectedBucketOwner: aws.String(account),
	})
	exists = true
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || !Contains([]string{"ServerSideEncryptionConfigurationNotFoundError", "NoSuchBucket"}, aerr.Code()) {
			Logger.Println("error:", err)
			return err
		}
		exists = false
	}
	if (input.encryption && (!exists || !reflect.DeepEqual(encOut.ServerSideEncryptionConfiguration, s3EncryptionConfig))) ||
		(!input.encryption && exists && len(encOut.ServerSideEncryptionConfiguration.Rules) != 0) {
		needsUpdate = true
	}
	if needsUpdate {
		if !preview {
			if input.encryption {
				_, err := S3Client().PutBucketEncryptionWithContext(ctx, &s3.PutBucketEncryptionInput{
					Bucket:                            aws.String(input.name),
					ExpectedBucketOwner:               aws.String(account),
					ServerSideEncryptionConfiguration: s3EncryptionConfig,
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
		if !exists {
			Logger.Printf(PreviewString(preview)+"created encryption for %s: %v\n", input.name, input.encryption)
		} else {
			Logger.Printf(PreviewString(preview)+"updated encryption for %s: %v\n", input.name, input.encryption)
		}
	}
	metrics, err := S3Client().GetBucketMetricsConfigurationWithContext(ctx, &s3.GetBucketMetricsConfigurationInput{
		Bucket:              aws.String(input.name),
		ExpectedBucketOwner: aws.String(account),
		Id:                  aws.String(s3MetricsId),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || !Contains([]string{"NoSuchConfiguration", "NoSuchBucket"}, aerr.Code()) {
			Logger.Println("error:", err)
			return err
		}
		if input.metrics {
			if !preview {
				_, err := S3Client().PutBucketMetricsConfigurationWithContext(ctx, &s3.PutBucketMetricsConfigurationInput{
					Bucket:              aws.String(input.name),
					ExpectedBucketOwner: aws.String(account),
					Id:                  aws.String(s3MetricsId),
					MetricsConfiguration: &s3.MetricsConfiguration{
						Id: aws.String(s3MetricsId),
					},
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"put bucket metrics for:", input.name)
		}
	} else {
		if input.metrics {
			if metrics.MetricsConfiguration.Filter != nil {
				err := fmt.Errorf("bucket metrics misconfigured: %s %s", input.name, s3MetricsId)
				Logger.Println("error:", err)
				return err
			}
		} else {
			if !preview {
				_, err := S3Client().DeleteBucketMetricsConfigurationWithContext(ctx, &s3.DeleteBucketMetricsConfigurationInput{
					Bucket:              aws.String(input.name),
					ExpectedBucketOwner: aws.String(account),
					Id:                  aws.String(s3MetricsId),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"delete bucket metrics for:", input.name)
		}
	}
	ttlOut, err := S3Client().GetBucketLifecycleConfigurationWithContext(ctx, &s3.GetBucketLifecycleConfigurationInput{
		Bucket:              aws.String(input.name),
		ExpectedBucketOwner: aws.String(account),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || !Contains([]string{"NoSuchLifecycleConfiguration", "NoSuchBucket"}, aerr.Code()) {
			Logger.Println("error:", err)
			return err
		}
		if input.ttlDays != 0 {
			if !preview {
				_, err := S3Client().PutBucketLifecycleConfigurationWithContext(ctx, &s3.PutBucketLifecycleConfigurationInput{
					Bucket:              aws.String(input.name),
					ExpectedBucketOwner: aws.String(account),
					LifecycleConfiguration: &s3.BucketLifecycleConfiguration{
						Rules: []*s3.LifecycleRule{{
							Expiration: &s3.LifecycleExpiration{
								Days: aws.Int64(int64(input.ttlDays)),
							},
							ID:     aws.String(fmt.Sprintf("ttlDays=%d", input.ttlDays)),
							Prefix: aws.String(""),
							Status: aws.String("Enabled"),
						}},
					},
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"put bucket ttl days for:", input.name, input.ttlDays)
		}
	} else {
		if input.ttlDays == 0 {
			if !preview {
				_, err := S3Client().DeleteBucketLifecycleWithContext(ctx, &s3.DeleteBucketLifecycleInput{
					Bucket:              aws.String(input.name),
					ExpectedBucketOwner: aws.String(account),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"deleted bucket ttl for:", input.name)
		} else {
			if len(ttlOut.Rules) != 1 {
				err := fmt.Errorf("expected exactly 1 ttl rule: %s %s", input.name, Pformat(ttlOut.Rules))
				Logger.Println("error:", err)
				return err
			}
			ttlDays := ttlOut.Rules[0].Expiration.Days
			if ttlDays == nil || *ttlDays != int64(input.ttlDays) {
				if !preview {
					_, err := S3Client().PutBucketLifecycleConfigurationWithContext(ctx, &s3.PutBucketLifecycleConfigurationInput{
						Bucket:              aws.String(input.name),
						ExpectedBucketOwner: aws.String(account),
						LifecycleConfiguration: &s3.BucketLifecycleConfiguration{
							Rules: []*s3.LifecycleRule{
								{
									Prefix: aws.String(""),
									Expiration: &s3.LifecycleExpiration{
										Days: aws.Int64(int64(input.ttlDays)),
									},
									Status: aws.String("Enabled"),
								},
							},
						},
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				if ttlDays == nil {
					*ttlDays = 0
				}
				Logger.Printf(PreviewString(preview)+"updated bucket ttl for %s: %d => %d\n", input.name, *ttlDays, input.ttlDays)
			}
		}
	}
	return nil
}

func S3DeleteBucket(ctx context.Context, bucket string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "S3DeleteBucket"}
		defer d.Log()
	}
	resp, err := http.Head(fmt.Sprintf("https://%s.s3.amazonaws.com", bucket))
	if err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode == 404 { // already deleted
			return nil
		}
	}
	// rm objects
	var token *string
	for {
		out, err := S3Client().ListObjectsV2WithContext(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			ContinuationToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		var objects []*s3.ObjectIdentifier
		for _, obj := range out.Contents {
			objects = append(objects, &s3.ObjectIdentifier{
				Key: obj.Key,
			})
		}
		if len(objects) != 0 {
			var errs []string
			if !preview {
				deleteOut, err := S3Client().DeleteObjectsWithContext(ctx, &s3.DeleteObjectsInput{
					Bucket: aws.String(bucket),
					Delete: &s3.Delete{Objects: objects},
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				for _, err := range deleteOut.Errors {
					Logger.Println("error:", *err.Key, *err.Code, *err.Message)
					errs = append(errs, *err.Key)
				}
			}
			for _, obj := range objects {
				Logger.Println(PreviewString(preview)+"deleted object:", *obj.Key)
			}
			if len(errs) != 0 {
				return fmt.Errorf("errors while deleting objects in bucket: %s %v", bucket, errs)
			}
		}
		if !*out.IsTruncated {
			break
		}
		token = out.NextContinuationToken
	}
	// rm versions
	var keyMarker *string
	var versionMarker *string
	for {
		out, err := S3Client().ListObjectVersionsWithContext(ctx, &s3.ListObjectVersionsInput{
			Bucket:          aws.String(bucket),
			Prefix:          nil,
			KeyMarker:       keyMarker,
			VersionIdMarker: versionMarker,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		var objects []*s3.ObjectIdentifier
		for _, obj := range out.Versions {
			if obj.VersionId != nil && *obj.VersionId == "null" {
				continue // "null" means unversioned
			}
			objects = append(objects, &s3.ObjectIdentifier{
				Key:       obj.Key,
				VersionId: obj.VersionId,
			})
		}
		for _, obj := range out.DeleteMarkers {
			objects = append(objects, &s3.ObjectIdentifier{
				Key:       obj.Key,
				VersionId: obj.VersionId,
			})
		}
		if !preview {
			if len(objects) != 0 {
				deleteOut, err := S3Client().DeleteObjectsWithContext(ctx, &s3.DeleteObjectsInput{
					Bucket: aws.String(bucket),
					Delete: &s3.Delete{Objects: objects},
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				var keys []string
				for _, err := range deleteOut.Errors {
					version := *err.VersionId
					if version == "" {
						version = "-"
					}
					Logger.Println("error:", *err.Key, version, *err.Code, *err.Message)
					keys = append(keys, *err.Key)
				}
				if len(deleteOut.Errors) != 0 {
					return fmt.Errorf("errors while deleting objects in bucket: %s %v", bucket, keys)
				}
			}
		}
		for _, obj := range objects {
			var version string
			if obj.VersionId == nil || *obj.VersionId == "" {
				version = "-"
			} else {
				version = *obj.VersionId
			}
			Logger.Println(PreviewString(preview)+"deleted version:", *obj.Key, version)
		}
		if !*out.IsTruncated {
			break
		}
		keyMarker = out.NextKeyMarker
		versionMarker = out.NextVersionIdMarker
	}
	// rm bucket
	if !preview {
		_, err = S3Client().DeleteBucketWithContext(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"deleted bucket:", bucket)
	return nil
}

type S3BucketDescription struct {
	Metrics       *s3.MetricsConfiguration
	Versioning    bool
	Acl           *s3.GetBucketAclOutput
	Cors          []*s3.CORSRule
	Encryption    *s3.ServerSideEncryptionConfiguration
	Lifecycle     []*s3.LifecycleRule
	Region        string
	Logging       *s3.LoggingEnabled
	Notifications *s3.NotificationConfiguration
	Policy        *IamPolicyDocument
	Replication   *s3.ReplicationConfiguration
}

func S3GetBucketDescription(ctx context.Context, bucket string) (*S3BucketDescription, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "S3GetBucketDescription"}
		defer d.Log()
	}
	var descr S3BucketDescription
	s3Client, err := S3ClientBucketRegion(bucket)
	if err != nil {
		return nil, err
	}
	version, err := s3Client.GetBucketVersioningWithContext(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	if version.Status != nil {
		descr.Versioning = *version.Status == s3.BucketVersioningStatusEnabled
	}
	acl, err := s3Client.GetBucketAclWithContext(ctx, &s3.GetBucketAclInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	descr.Acl = acl
	cors, err := s3Client.GetBucketCorsWithContext(ctx, &s3.GetBucketCorsInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "NoSuchCORSConfiguration" {
			Logger.Println("error:", err)
			return nil, err
		}
	} else {
		descr.Cors = cors.CORSRules
	}
	encryption, err := s3Client.GetBucketEncryptionWithContext(ctx, &s3.GetBucketEncryptionInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "ServerSideEncryptionConfigurationNotFoundError" {
			Logger.Println("error:", err)
			return nil, err
		}
	} else {
		descr.Encryption = encryption.ServerSideEncryptionConfiguration
	}
	lifecycle, err := s3Client.GetBucketLifecycleConfigurationWithContext(ctx, &s3.GetBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "NoSuchLifecycleConfiguration" {
			Logger.Println("error:", err)
			return nil, err
		}
	} else {
		descr.Lifecycle = lifecycle.Rules
	}
	region, err := S3BucketRegion(bucket)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	descr.Region = region
	logging, err := s3Client.GetBucketLoggingWithContext(ctx, &s3.GetBucketLoggingInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	descr.Logging = logging.LoggingEnabled
	notif, err := s3Client.GetBucketNotificationConfigurationWithContext(ctx, &s3.GetBucketNotificationConfigurationRequest{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	if notif.LambdaFunctionConfigurations != nil || notif.QueueConfigurations != nil || notif.TopicConfigurations != nil {
		descr.Notifications = notif
	}
	policy, err := s3Client.GetBucketPolicyWithContext(ctx, &s3.GetBucketPolicyInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "NoSuchBucketPolicy" {
			Logger.Println("error:", err)
			return nil, err
		}
	} else {
		descr.Policy = &IamPolicyDocument{}
		err := json.Unmarshal([]byte(*policy.Policy), descr.Policy)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
	}
	replication, err := s3Client.GetBucketReplicationWithContext(ctx, &s3.GetBucketReplicationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "ReplicationConfigurationNotFoundError" {
			Logger.Println("error:", err)
			return nil, err
		}
	} else {
		descr.Replication = replication.ReplicationConfiguration
	}
	metrics, err := s3Client.GetBucketMetricsConfigurationWithContext(ctx, &s3.GetBucketMetricsConfigurationInput{
		Bucket: aws.String(bucket),
		Id:     aws.String(s3MetricsId),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "NoSuchConfiguration" {
			Logger.Println("error:", err)
			return nil, err
		}
	} else {
		descr.Metrics = metrics.MetricsConfiguration
	}
	return &descr, nil
}

func S3PresignPut(bucket, key string, expire time.Duration) string {
	req, _ := S3Client().PutObjectRequest(&s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	url, err := req.Presign(expire)
	if err != nil {
		panic(err)
	}
	return url
}

func S3PresignGet(bucket, key, byterange string, expire time.Duration) string {
	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if byterange != "" {
		input.Range = aws.String(byterange)
	}
	req, _ := S3Client().GetObjectRequest(input)
	url, err := req.Presign(expire)
	if err != nil {
		panic(err)
	}
	return url
}
