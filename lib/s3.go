package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	s3MetricsId                                     = "S3MetricsEntireBucket"
	s3ErrCodeNotFound                               = "NotFound"
	s3ErrCodeNoSuchBucket                           = "NoSuchBucket"
	s3ErrCodeBucketAlreadyOwnedByYou                = "BucketAlreadyOwnedByYou"
	s3ErrCodeNoSuchBucketPolicy                     = "NoSuchBucketPolicy"
	s3ErrCodeNoSuchConfiguration                    = "NoSuchConfiguration"
	s3ErrCodeNoSuchPublicAccessBlockConfiguration   = "NoSuchPublicAccessBlockConfiguration"
	s3ErrCodeServerSideEncryptionConfigurationError = "ServerSideEncryptionConfigurationNotFoundError"
	s3ErrCodeReplicationConfigurationNotFoundError  = "ReplicationConfigurationNotFoundError"
	s3ErrCodeNoSuchCORSConfiguration                = "NoSuchCORSConfiguration"
	s3ErrCodeNoSuchLifecycleConfiguration           = "NoSuchLifecycleConfiguration"
	s3ErrCodeNoSuchTagSet                           = "NoSuchTagSet"
)

var s3Client *s3.Client
var s3ClientLock sync.Mutex
var s3ClientsRegional = map[string]*s3.Client{}

func S3ClientExplicit(accessKeyID, accessKeySecret, region string) *s3.Client {
	return s3.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func S3Client() *s3.Client {
	s3ClientLock.Lock()
	defer s3ClientLock.Unlock()
	if s3Client == nil {
		s3Client = s3.NewFromConfig(*Session())
	}
	return s3Client
}

func S3ClientRegion(region string) (*s3.Client, error) {
	s3ClientLock.Lock()
	defer s3ClientLock.Unlock()
	s3Client, ok := s3ClientsRegional[region]
	if !ok {
		sess, err := SessionRegion(region)
		if err != nil {
			return nil, err
		}
		s3Client = s3.NewFromConfig(*sess)
		s3ClientsRegional[region] = s3Client
	}
	return s3Client, nil
}

func S3ClientRegionMust(region string) *s3.Client {
	client, err := S3ClientRegion(region)
	if err != nil {
		panic(err)
	}
	return client
}

var s3BucketRegionLock sync.Mutex
var s3BucketRegion = map[string]string{}

func S3BucketRegion(bucket string) (string, error) {
	s3BucketRegionLock.Lock()
	defer s3BucketRegionLock.Unlock()
	region, ok := s3BucketRegion[bucket]
	if !ok {
		cacheFile := "/tmp/aws.s3.bucket.region=" + bucket
		data, err := os.ReadFile(cacheFile)
		if err == nil {
			region = string(data)
		} else {
			if doDebug {
				d := &Debug{start: time.Now(), name: "S3BucketRegion"}
				d.Start()
				defer d.End()
			}
			resp, err := http.Head(fmt.Sprintf("https://%s.s3.amazonaws.com", bucket))
			if err != nil {
				return "", err
			}
			defer func() { _ = resp.Body.Close() }()
			switch resp.StatusCode {
			case 200:
			case 400:
			case 403:
			case 404:
				return "", fmt.Errorf("no such bucket: %s", bucket)
			default:
				err := fmt.Errorf("http %d for %s", resp.StatusCode, bucket)
				Logger.Println("error:", err)
				return "", err
			}
			region = resp.Header.Get("x-amz-bucket-region")
			if region == "" {
				return "", fmt.Errorf("empty x-amz-bucket-region for bucket: %s", bucket)
			}
			err = os.WriteFile(cacheFile, []byte(region), os.ModePerm)
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
		}
		s3BucketRegion[bucket] = region
	}
	return region, nil
}

func S3ClientBucketRegion(bucket string) (*s3.Client, error) {
	var s3Client *s3.Client
	var expectedErr error
	err := Retry(context.Background(), func() error {
		var region string
		var err error
		region, err = S3BucketRegion(bucket)
		if err != nil {
			if strings.Contains(err.Error(), "no such bucket") {
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

func S3ClientBucketRegionMust(bucket string) *s3.Client {
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
	cors         *bool
	corsOrigins  []string
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
		metrics:    false,
		cors:       nil,
		ttlDays:    0,
	}
}

func S3EnsureInput(infraSetName, bucketName string, attrs []string) (*s3EnsureInput, error) {
	input := s3EnsureInputDefault()
	input.infraSetName = infraSetName
	input.name = bucketName
	policy := IamPolicyDocument{
		Version:   "2012-10-17",
		Statement: []IamStatementEntry{},
	}
	for _, line := range attrs {
		line = strings.ToLower(line)
		attr, value, err := SplitOnce(line, "=")
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		switch attr {
		case "allow_put":
			policy.Statement = append(policy.Statement, IamStatementEntry{
				Sid:       "allow put from " + value,
				Effect:    "Allow",
				Action:    "s3:PutObject",
				Resource:  "arn:aws:s3:::" + bucketName + "/*",
				Principal: map[string]string{"Service": value},
			})
		case "ttldays":
			input.ttlDays = Atoi(value)
		case "corsorigin":
			if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
				err := fmt.Errorf("corsorigin must begin with http or https: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
			input.corsOrigins = append(input.corsOrigins, value)
		case "cors":
			switch value {
			case "true", "false":
				input.cors = aws.Bool(value == "true")
			default:
				err := fmt.Errorf("unknown attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
		case "acl":
			switch value {
			case "public", "private":
				input.acl = value
			default:
				err := fmt.Errorf("unknown attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
		case "versioning":
			switch value {
			case "true", "false":
				input.versioning = value == "true"
			default:
				err := fmt.Errorf("unknown attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
		case "metrics":
			switch value {
			case "true", "false":
				input.metrics = value == "true"
			default:
				err := fmt.Errorf("unknown attr: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
		default:
			err := fmt.Errorf("unknown attr: %s", line)
			Logger.Println("error:", err)
			return nil, err
		}
	}
	if len(policy.Statement) > 0 {
		data, err := json.Marshal(policy)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		input.CustomPolicy = aws.String(string(data))
	}
	if input.cors != nil && !*input.cors && len(input.corsOrigins) > 0 {
		err := fmt.Errorf("cannot specify cors=false and corsorigin=VALUE: %s", Pformat(input.corsOrigins))
		Logger.Println("error:", err)
		return nil, err
	}
	if len(input.corsOrigins) > 0 {
		input.cors = aws.Bool(true)
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

func s3Cors(allowedOrigins []string) []s3types.CORSRule {
	if len(allowedOrigins) == 0 {
		allowedOrigins = append(allowedOrigins, "*")
	}
	return []s3types.CORSRule{{
		AllowedHeaders: []string{"Authorization", "Range"},
		AllowedMethods: []string{"GET", "PUT", "POST", "HEAD"},
		AllowedOrigins: allowedOrigins,
		ExposeHeaders:  []string{"Content-Length", "Content-Type", "ETag"},
		MaxAgeSeconds:  aws.Int32(int32(3000)),
	}}
}

var s3EncryptionConfig = &s3types.ServerSideEncryptionConfiguration{
	Rules: []s3types.ServerSideEncryptionRule{{
		ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
			SSEAlgorithm: s3types.ServerSideEncryptionAes256,
		},
		BucketKeyEnabled: aws.Bool(false),
	}},
}

func S3Ensure(ctx context.Context, input *s3EnsureInput, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "S3Ensure"}
		d.Start()
		defer d.End()
	}
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	_, err = S3Client().HeadBucket(ctx, &s3.HeadBucketInput{
		ExpectedBucketOwner: aws.String(account),
		Bucket:              aws.String(input.name),
	})
	if err != nil {
		if !strings.Contains(err.Error(), s3ErrCodeNotFound) {
			Logger.Println("error:", err)
			return err
		}
		if !preview {
			_, err := S3Client().CreateBucket(ctx, &s3.CreateBucketInput{
				Bucket: aws.String(input.name),
				CreateBucketConfiguration: &s3types.CreateBucketConfiguration{
					LocationConstraint: s3types.BucketLocationConstraint(Region()),
				},
			})
			if err != nil {
				if !strings.Contains(err.Error(), s3ErrCodeBucketAlreadyOwnedByYou) {
					Logger.Println("error:", err)
					return err
				}
			}
		}
		Logger.Println(PreviewString(preview)+"created bucket:", input.name)
	}
	exists := false
	getTagOut, err := S3Client().GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
		ExpectedBucketOwner: aws.String(account),
		Bucket:              aws.String(input.name),
	})
	if err != nil {
		if !strings.Contains(err.Error(), s3ErrCodeNoSuchBucket) && !strings.Contains(err.Error(), s3ErrCodeNoSuchTagSet) {
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
			_, err := S3Client().PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
				ExpectedBucketOwner: aws.String(account),
				Bucket:              aws.String(input.name),
				Tagging: &s3types.Tagging{
					TagSet: []s3types.Tag{{
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
	pabOut, err := S3Client().GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{
		ExpectedBucketOwner: aws.String(account),
		Bucket:              aws.String(input.name),
	})
	if err != nil {
		if !strings.Contains(err.Error(), s3ErrCodeNoSuchPublicAccessBlockConfiguration) && !strings.Contains(err.Error(), s3ErrCodeNoSuchBucket) {
			Logger.Println("error:", err)
			return err
		}
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
			_, err := S3Client().PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{
				ExpectedBucketOwner: aws.String(account),
				Bucket:              aws.String(input.name),
				PublicAccessBlockConfiguration: &s3types.PublicAccessBlockConfiguration{
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
	policyOut, err := S3Client().GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{
		ExpectedBucketOwner: aws.String(account),
		Bucket:              aws.String(input.name),
	})
	if err != nil {
		if !strings.Contains(err.Error(), s3ErrCodeNoSuchBucketPolicy) && !strings.Contains(err.Error(), s3ErrCodeNoSuchBucket) {
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
				_, err = S3Client().PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
					ExpectedBucketOwner: aws.String(account),
					Bucket:              aws.String(input.name),
					Policy:              aws.String(string(policyBytes)),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"put acl:", input.name, aclName, string(policyBytes))
		}
	} else if input.acl == "private" {
		policy := IamPolicyDocument{}
		err = json.Unmarshal([]byte(*policyOut.Policy), &policy)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		if input.CustomPolicy != nil {
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
			if !preview {
				_, err := S3Client().DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{
					Bucket: aws.String(input.name),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"remove bucket policy:", input.name, *policyOut.Policy)
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
	corsOut, err := S3Client().GetBucketCors(ctx, &s3.GetBucketCorsInput{
		ExpectedBucketOwner: aws.String(account),
		Bucket:              aws.String(input.name),
	})
	if err != nil {
		if !strings.Contains(err.Error(), s3ErrCodeNoSuchCORSConfiguration) && !strings.Contains(err.Error(), s3ErrCodeNoSuchBucket) {
			Logger.Println("error:", err)
			return err
		}
		if input.cors != nil && *input.cors {
			if !preview {
				_, err := S3Client().PutBucketCors(ctx, &s3.PutBucketCorsInput{
					ExpectedBucketOwner: aws.String(account),
					Bucket:              aws.String(input.name),
					CORSConfiguration: &s3types.CORSConfiguration{
						CORSRules: s3Cors(input.corsOrigins),
					},
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"put cors:", input.name)
		}
	} else if input.cors == nil || !*input.cors {
		if !preview {
			_, err := S3Client().DeleteBucketCors(ctx, &s3.DeleteBucketCorsInput{
				ExpectedBucketOwner: aws.String(account),
				Bucket:              aws.String(input.name),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"delete cors:", input.name)
	} else if len(corsOut.CORSRules) != 1 {
		err := fmt.Errorf("bucket cors config is misconfigured for bucket: %s", input.name)
		Logger.Println("error:", err)
		return err
	} else if !reflect.DeepEqual(corsOut.CORSRules, s3Cors(input.corsOrigins)) {
		if !preview {
			_, err := S3Client().PutBucketCors(ctx, &s3.PutBucketCorsInput{
				ExpectedBucketOwner: aws.String(account),
				Bucket:              aws.String(input.name),
				CORSConfiguration: &s3types.CORSConfiguration{
					CORSRules: s3Cors(input.corsOrigins),
				},
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		have := corsOut.CORSRules[0]
		want := s3Cors(input.corsOrigins)[0]
		if !reflect.DeepEqual(have.AllowedHeaders, want.AllowedHeaders) {
			Logger.Printf(PreviewString(preview)+"updated cors allowed headers for %s: %s -> %s\n",
				input.name, Json(have.AllowedHeaders), Json(want.AllowedHeaders))
		}
		if !reflect.DeepEqual(have.AllowedMethods, want.AllowedMethods) {
			Logger.Printf(PreviewString(preview)+"updated cors allowed methods: %s -> %s\n",
				input.name, Json(have.AllowedMethods), Json(want.AllowedMethods))
		}
		if !reflect.DeepEqual(have.AllowedOrigins, want.AllowedOrigins) {
			Logger.Printf(PreviewString(preview)+"updated cors allowed origins for %s: %s -> %s\n",
				input.name, Json(have.AllowedOrigins), Json(want.AllowedOrigins))
		}
		if !reflect.DeepEqual(have.ExposeHeaders, want.ExposeHeaders) {
			Logger.Printf(PreviewString(preview)+"updated cors expose headers for %s: %s -> %s\n",
				input.name, Json(have.ExposeHeaders), Json(want.ExposeHeaders))
		}
		if !reflect.DeepEqual(have.MaxAgeSeconds, want.MaxAgeSeconds) {
			haveMax := int32(0)
			if have.MaxAgeSeconds != nil {
				haveMax = *have.MaxAgeSeconds
			}
			wantMax := int32(0)
			if want.MaxAgeSeconds != nil {
				wantMax = *want.MaxAgeSeconds
			}
			Logger.Printf(PreviewString(preview)+"updated cors max age seconds for %s: %d -> %d\n", input.name, haveMax, wantMax)
		}
	}
	needsUpdate := false
	versionOut, err := S3Client().GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		ExpectedBucketOwner: aws.String(account),
		Bucket:              aws.String(input.name),
	})
	if err != nil {
		if !strings.Contains(err.Error(), s3ErrCodeNoSuchBucket) {
			Logger.Println("error:", err)
			return err
		}
		if preview {
			versionOut = &s3.GetBucketVersioningOutput{}
		}
	}
	if (input.versioning && (versionOut.Status == "" || versionOut.Status != s3types.BucketVersioningStatusEnabled)) ||
		(!input.versioning && versionOut.Status != "" && versionOut.Status != s3types.BucketVersioningStatusSuspended) {
		needsUpdate = true
	}
	if needsUpdate {
		if !preview {
			status := s3types.BucketVersioningStatusSuspended
			if input.versioning {
				status = s3types.BucketVersioningStatusEnabled
			}
			_, err := S3Client().PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
				ExpectedBucketOwner: aws.String(account),
				Bucket:              aws.String(input.name),
				VersioningConfiguration: &s3types.VersioningConfiguration{
					Status: status,
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
	encOut, err := S3Client().GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{
		Bucket:              aws.String(input.name),
		ExpectedBucketOwner: aws.String(account),
	})
	exists = true
	if err != nil {
		if !strings.Contains(err.Error(), s3ErrCodeServerSideEncryptionConfigurationError) && !strings.Contains(err.Error(), s3ErrCodeNoSuchBucket) {
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
				_, err := S3Client().PutBucketEncryption(ctx, &s3.PutBucketEncryptionInput{
					ExpectedBucketOwner:               aws.String(account),
					Bucket:                            aws.String(input.name),
					ServerSideEncryptionConfiguration: s3EncryptionConfig,
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			} else {
				_, err := S3Client().DeleteBucketEncryption(ctx, &s3.DeleteBucketEncryptionInput{
					ExpectedBucketOwner: aws.String(account),
					Bucket:              aws.String(input.name),
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
	metrics, err := S3Client().GetBucketMetricsConfiguration(ctx, &s3.GetBucketMetricsConfigurationInput{
		ExpectedBucketOwner: aws.String(account),
		Bucket:              aws.String(input.name),
		Id:                  aws.String(s3MetricsId),
	})
	if err != nil {
		if !strings.Contains(err.Error(), s3ErrCodeNoSuchConfiguration) && !strings.Contains(err.Error(), s3ErrCodeNoSuchBucket) {
			Logger.Println("error:", err)
			return err
		}
		if input.metrics {
			if !preview {
				_, err := S3Client().PutBucketMetricsConfiguration(ctx, &s3.PutBucketMetricsConfigurationInput{
					ExpectedBucketOwner: aws.String(account),
					Bucket:              aws.String(input.name),
					Id:                  aws.String(s3MetricsId),
					MetricsConfiguration: &s3types.MetricsConfiguration{
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
				_, err := S3Client().DeleteBucketMetricsConfiguration(ctx, &s3.DeleteBucketMetricsConfigurationInput{
					ExpectedBucketOwner: aws.String(account),
					Bucket:              aws.String(input.name),
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
	ttlOut, err := S3Client().GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{
		ExpectedBucketOwner: aws.String(account),
		Bucket:              aws.String(input.name),
	})
	if err != nil {
		if !strings.Contains(err.Error(), s3ErrCodeNoSuchLifecycleConfiguration) && !strings.Contains(err.Error(), s3ErrCodeNoSuchBucket) {
			Logger.Println("error:", err)
			return err
		}
		if input.ttlDays != 0 {
			if !preview {
				_, err := S3Client().PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
					ExpectedBucketOwner: aws.String(account),
					Bucket:              aws.String(input.name),
					LifecycleConfiguration: &s3types.BucketLifecycleConfiguration{
						Rules: []s3types.LifecycleRule{{
							Expiration: &s3types.LifecycleExpiration{
								Days: aws.Int32(int32(input.ttlDays)),
							},
							ID:     aws.String(fmt.Sprintf("ttlDays=%d", input.ttlDays)),
							Prefix: aws.String(""),
							Status: s3types.ExpirationStatusEnabled,
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
				_, err := S3Client().DeleteBucketLifecycle(ctx, &s3.DeleteBucketLifecycleInput{
					ExpectedBucketOwner: aws.String(account),
					Bucket:              aws.String(input.name),
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
			if ttlDays == nil || *ttlDays != int32(input.ttlDays) {
				if !preview {
					_, err := S3Client().PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
						ExpectedBucketOwner: aws.String(account),
						Bucket:              aws.String(input.name),
						LifecycleConfiguration: &s3types.BucketLifecycleConfiguration{
							Rules: []s3types.LifecycleRule{
								{
									Prefix: aws.String(""),
									Expiration: &s3types.LifecycleExpiration{
										Days: aws.Int32(int32(input.ttlDays)),
									},
									Status: s3types.ExpirationStatusEnabled,
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
					ttlDays = aws.Int32(0)
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
		d.Start()
		defer d.End()
	}
	resp, err := http.Head(fmt.Sprintf("https://%s.s3.amazonaws.com", bucket))
	if err == nil {
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode == 404 { // already deleted
			return nil
		}
	}
	s3Client, err := S3ClientBucketRegion(bucket)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	// rm objects
	var token *string
	for {
		out, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			ContinuationToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		var objects []s3types.ObjectIdentifier
		for _, obj := range out.Contents {
			objects = append(objects, s3types.ObjectIdentifier{
				Key: obj.Key,
			})
		}
		if len(objects) != 0 {
			var errs []string
			if !preview {
				deleteOut, err := s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
					Bucket: aws.String(bucket),
					Delete: &s3types.Delete{Objects: objects},
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				for _, err := range deleteOut.Errors {
					Logger.Println("error:", *err.Key, err.Code, *err.Message)
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
		out, err := s3Client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
			Bucket:          aws.String(bucket),
			KeyMarker:       keyMarker,
			VersionIdMarker: versionMarker,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		var objects []s3types.ObjectIdentifier
		for _, obj := range out.Versions {
			if obj.VersionId != nil && *obj.VersionId == "null" {
				continue // "null" means unversioned
			}
			objects = append(objects, s3types.ObjectIdentifier{
				Key:       obj.Key,
				VersionId: obj.VersionId,
			})
		}
		for _, obj := range out.DeleteMarkers {
			objects = append(objects, s3types.ObjectIdentifier{
				Key:       obj.Key,
				VersionId: obj.VersionId,
			})
		}
		if !preview && len(objects) != 0 {
			deleteOut, err := s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(bucket),
				Delete: &s3types.Delete{Objects: objects},
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			var keys []string
			for _, err := range deleteOut.Errors {
				var version string
				if err.VersionId == nil {
					version = "-"
				} else {
					version = *err.VersionId
					if version == "" {
						version = "-"
					}
				}
				Logger.Println("error:", *err.Key, version, err.Code, *err.Message)
				keys = append(keys, *err.Key)
			}
			if len(deleteOut.Errors) != 0 {
				return fmt.Errorf("errors while deleting objects in bucket: %s %v", bucket, keys)
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
		_, err = s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
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
	Metrics       *s3types.MetricsConfiguration
	Versioning    bool
	Acl           *s3.GetBucketAclOutput
	Cors          []s3types.CORSRule
	Encryption    *s3types.ServerSideEncryptionConfiguration
	Lifecycle     []s3types.LifecycleRule
	Region        string
	Logging       *s3types.LoggingEnabled
	Notifications *s3.GetBucketNotificationConfigurationOutput
	Policy        *IamPolicyDocument
	Replication   *s3types.ReplicationConfiguration
}

func S3GetBucketDescription(ctx context.Context, bucket string) (*S3BucketDescription, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "S3GetBucketDescription"}
		d.Start()
		defer d.End()
	}
	var descr S3BucketDescription
	s3Client, err := S3ClientBucketRegion(bucket)
	if err != nil {
		return nil, err
	}
	version, err := s3Client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	if version.Status == s3types.BucketVersioningStatusEnabled {
		descr.Versioning = true
	}
	acl, err := s3Client.GetBucketAcl(ctx, &s3.GetBucketAclInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	descr.Acl = acl
	cors, err := s3Client.GetBucketCors(ctx, &s3.GetBucketCorsInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if !strings.Contains(err.Error(), s3ErrCodeNoSuchCORSConfiguration) {
			Logger.Println("error:", err)
			return nil, err
		}
	} else {
		descr.Cors = cors.CORSRules
	}
	encryption, err := s3Client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if !strings.Contains(err.Error(), s3ErrCodeServerSideEncryptionConfigurationError) {
			Logger.Println("error:", err)
			return nil, err
		}
	} else {
		descr.Encryption = encryption.ServerSideEncryptionConfiguration
	}
	lifecycle, err := s3Client.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if !strings.Contains(err.Error(), s3ErrCodeNoSuchLifecycleConfiguration) {
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
	logging, err := s3Client.GetBucketLogging(ctx, &s3.GetBucketLoggingInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	descr.Logging = logging.LoggingEnabled
	notif, err := s3Client.GetBucketNotificationConfiguration(ctx, &s3.GetBucketNotificationConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	if len(notif.LambdaFunctionConfigurations) != 0 || len(notif.QueueConfigurations) != 0 || len(notif.TopicConfigurations) != 0 {
		descr.Notifications = notif
	}
	policy, err := s3Client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if !strings.Contains(err.Error(), s3ErrCodeNoSuchBucketPolicy) {
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
	replication, err := s3Client.GetBucketReplication(ctx, &s3.GetBucketReplicationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if !strings.Contains(err.Error(), s3ErrCodeReplicationConfigurationNotFoundError) {
			Logger.Println("error:", err)
			return nil, err
		}
	} else {
		descr.Replication = replication.ReplicationConfiguration
	}
	metrics, err := s3Client.GetBucketMetricsConfiguration(ctx, &s3.GetBucketMetricsConfigurationInput{
		Bucket: aws.String(bucket),
		Id:     aws.String(s3MetricsId),
	})
	if err != nil {
		if !strings.Contains(err.Error(), s3ErrCodeNoSuchConfiguration) {
			Logger.Println("error:", err)
			return nil, err
		}
	} else {
		descr.Metrics = metrics.MetricsConfiguration
	}
	return &descr, nil
}

func S3PresignPut(bucket, key string, expire time.Duration) string {
	presignClient := s3.NewPresignClient(S3Client())
	input := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	req, err := presignClient.PresignPutObject(context.TODO(), input, func(po *s3.PresignOptions) {
		po.Expires = expire
	})
	if err != nil {
		panic(err)
	}
	return req.URL
}

func S3PresignGet(bucket, key, byterange string, expire time.Duration) string {
	presignClient := s3.NewPresignClient(S3Client())
	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if byterange != "" {
		input.Range = aws.String(byterange)
	}
	req, err := presignClient.PresignGetObject(context.TODO(), input, func(po *s3.PresignOptions) {
		po.Expires = expire
	})
	if err != nil {
		panic(err)
	}
	return req.URL
}

type S3DeleteInput struct {
	Bucket    string
	Prefix    string
	Recursive bool
	Preview   bool
}

func S3Delete(ctx context.Context, input *S3DeleteInput) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "S3Delete"}
		d.Start()
		defer d.End()
	}

	s3Client, err := S3ClientBucketRegion(input.Bucket)
	if err != nil {
		return err
	}

	var delimiter *string
	if !input.Recursive {
		delimiter = aws.String("/")
	}

	var token *string
	for {
		out, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(input.Bucket),
			Prefix:            aws.String(input.Prefix),
			Delimiter:         delimiter,
			ContinuationToken: token,
		})
		if err != nil {
			return err
		}

		var objects []s3types.ObjectIdentifier

		for _, obj := range out.Contents {
			objects = append(objects, s3types.ObjectIdentifier{
				Key: obj.Key,
			})
		}

		if len(objects) != 0 {

			if !input.Preview {

				deleteOut, err := s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
					Bucket: aws.String(input.Bucket),
					Delete: &s3types.Delete{Objects: objects},
				})
				if err != nil {
					return err
				}

				for _, err := range deleteOut.Errors {
					Logger.Println("error:", *err.Key, *err.Code, *err.Message)
				}
				if len(deleteOut.Errors) != 0 {
					return fmt.Errorf("errors deleting objects")
				}

			}

			for _, object := range objects {
				Logger.Println(PreviewString(input.Preview)+"s3 deleted:", *object.Key)
			}

		}
		if !*out.IsTruncated {
			break
		}

		token = out.NextContinuationToken
	}

	return nil
}
