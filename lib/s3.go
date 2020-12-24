package lib

import (
	"sync"

	"github.com/aws/aws-sdk-go/service/s3"
)

var s3Client *s3.S3
var s3ClientLock sync.RWMutex

func S3Client() *s3.S3 {
	s3ClientLock.Lock()
	defer s3ClientLock.Unlock()
	if s3Client == nil {
		s3Client = s3.New(Session())
	}
	return s3Client
}
