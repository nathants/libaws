package lib

import (
	"os"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
)

var sess *session.Session
var sessLock sync.RWMutex
var sessRegional = make(map[string]*session.Session)

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
