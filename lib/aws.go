package lib

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

var sess *aws.Config
var sessLock sync.Mutex
var sessRegional = map[string]*aws.Config{}

func SessionExplicit(accessKeyID, accessKeySecret, region string) *aws.Config {
	err := os.Setenv("AWS_STS_REGIONAL_ENDPOINTS", "regional")
	if err != nil {
		panic(err)
	}
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, accessKeySecret, "")),
		config.WithRetryMaxAttempts(5),
	)
	if err != nil {
		panic(err)
	}
	return &cfg
}

func Session() *aws.Config {
	sessLock.Lock()
	defer sessLock.Unlock()
	if sess == nil {
		err := os.Setenv("AWS_SDK_LOAD_CONFIG", "true")
		if err != nil {
			panic(err)
		}
		err = os.Setenv("AWS_STS_REGIONAL_ENDPOINTS", "regional")
		if err != nil {
			panic(err)
		}
		cfg, err := config.LoadDefaultConfig(context.Background(),
			config.WithRetryMaxAttempts(5),
		)
		if err != nil {
			panic(err)
		}
		sess = &cfg
	}
	return sess
}

func SessionRegion(region string) (*aws.Config, error) {
	sessLock.Lock()
	defer sessLock.Unlock()
	sess, ok := sessRegional[region]
	if !ok {
		err := os.Setenv("AWS_SDK_LOAD_CONFIG", "true")
		if err != nil {
			return nil, err
		}
		err = os.Setenv("AWS_STS_REGIONAL_ENDPOINTS", "regional")
		if err != nil {
			return nil, err
		}
		cfg, err := config.LoadDefaultConfig(context.Background(),
			config.WithRegion(region),
			config.WithRetryMaxAttempts(5),
		)
		if err != nil {
			return nil, err
		}
		sess = &cfg
		sessRegional[region] = sess
	}
	return sess, nil
}

func Region() string {
	sess := Session()
	return sess.Region
}

func Regions() ([]string, error) {
	out, err := EC2Client().DescribeRegions(context.Background(), &ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(true),
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	var regions []string
	for _, region := range out.Regions {
		regions = append(regions, *region.RegionName)
	}
	return regions, nil
}

func Zones(ctx context.Context) ([]ec2types.AvailabilityZone, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "Zones"}
		d.Start()
		defer d.End()
	}
	out, err := EC2Client().DescribeAvailabilityZones(ctx, &ec2.DescribeAvailabilityZonesInput{})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return out.AvailabilityZones, nil
}
