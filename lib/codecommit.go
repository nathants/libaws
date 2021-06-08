package lib

import (
	"sync"

	"github.com/aws/aws-sdk-go/service/codecommit"
)

var codeCommitClient *codecommit.CodeCommit
var codeCommitClientLock sync.RWMutex

func CodeCommitClient() *codecommit.CodeCommit {
	codeCommitClientLock.Lock()
	defer codeCommitClientLock.Unlock()
	if codeCommitClient == nil {
		codeCommitClient = codecommit.New(Session())
	}
	return codeCommitClient
}
