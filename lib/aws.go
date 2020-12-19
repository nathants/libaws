package lib

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"os"
	"sync"
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
