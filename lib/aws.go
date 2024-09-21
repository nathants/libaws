package lib

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

var sess *session.Session
var sessLock sync.RWMutex
var sessRegional = make(map[string]*session.Session)

func SessionExplicit(accessKeyID, accessKeySecret, region string) *session.Session {
	sess, err := session.NewSession(&aws.Config{
		Region:              aws.String(region),
		STSRegionalEndpoint: endpoints.RegionalSTSEndpoint,
		MaxRetries:          aws.Int(5),
		Credentials:         credentials.NewStaticCredentials(accessKeyID, accessKeySecret, ""),
	})
	if err != nil {
		panic(err)
	}
	return sess
}

func Session() *session.Session {
	sessLock.Lock()
	defer sessLock.Unlock()
	if sess == nil {
		err := os.Setenv("AWS_SDK_LOAD_CONFIG", "true")
		if err != nil {
			panic(err)
		}
		sess = session.Must(session.NewSession(&aws.Config{
			STSRegionalEndpoint: endpoints.RegionalSTSEndpoint,
			MaxRetries:          aws.Int(5),
		}))
	}
	return sess
}

func SessionRegion(region string) (*session.Session, error) {
	sessLock.Lock()
	defer sessLock.Unlock()
	sess, ok := sessRegional[region]
	if !ok {
		err := os.Setenv("AWS_SDK_LOAD_CONFIG", "true")
		if err != nil {
			return nil, err
		}
		sess, err = session.NewSession(&aws.Config{
			Region:              aws.String(region),
			STSRegionalEndpoint: endpoints.RegionalSTSEndpoint,
			MaxRetries:          aws.Int(5),
		})
		if err != nil {
			return nil, err
		}
		sessRegional[region] = sess
	}
	return sess, nil
}

func Region() string {
	sess := Session()
	return *sess.Config.Region
}

func Regions() ([]string, error) {
	out, err := EC2Client().DescribeRegions(&ec2.DescribeRegionsInput{
        AllRegions: aws.Bool(true), // Set to true to include all regions
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

func Zones(ctx context.Context) ([]*ec2.AvailabilityZone, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "Zones"}
		defer d.Log()
	}
	out, err := EC2Client().DescribeAvailabilityZonesWithContext(ctx, &ec2.DescribeAvailabilityZonesInput{})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return out.AvailabilityZones, nil
}
