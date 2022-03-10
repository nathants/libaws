package lib

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go/service/codecommit"
)

var codeCommitClient *codecommit.CodeCommit
var codeCommitClientLock sync.RWMutex

func CodeCommitClientClear() {
	codeCommitClientLock.Lock()
	defer codeCommitClientLock.Unlock()
	codeCommitClient = nil
	sessionClear()
}

func CodeCommitClient() *codecommit.CodeCommit {
	codeCommitClientLock.Lock()
	defer codeCommitClientLock.Unlock()
	if codeCommitClient == nil {
		codeCommitClient = codecommit.New(Session())
	}
	return codeCommitClient
}

func CodeCommitListRepos(ctx context.Context) ([]*codecommit.RepositoryNameIdPair, error) {
	var token *string
	var repos []*codecommit.RepositoryNameIdPair
	for {
		out, err := CodeCommitClient().ListRepositoriesWithContext(ctx, &codecommit.ListRepositoriesInput{
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		repos = append(repos, out.Repositories...)
		if out.NextToken == nil {
			break
		}
	}
	return repos, nil
}
