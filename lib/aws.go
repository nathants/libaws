package lib

import (
	"os"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
)

var Commands = make(map[string]func())

var sess *session.Session
var sessLock sync.RWMutex

func Session() *session.Session {
	sessLock.Lock()
	defer sessLock.Unlock()
	if sess == nil {
		err := os.Setenv("AWS_SDK_LOAD_CONFIG", "true")
		if err != nil {
			panic(err)
		}
		sess = session.Must(session.NewSession(&aws.Config{}))
	}
	return sess
}

func Region() string {
	sess := Session()
	return *sess.Config.Region
}
