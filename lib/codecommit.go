package lib

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/codecommit"
	codecommittypes "github.com/aws/aws-sdk-go-v2/service/codecommit/types"
)

var codeCommitClient *codecommit.Client
var codeCommitClientLock sync.Mutex

func CodeCommitClientExplicit(accessKeyID, accessKeySecret, region string) *codecommit.Client {
	return codecommit.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func CodeCommitClient() *codecommit.Client {
	codeCommitClientLock.Lock()
	defer codeCommitClientLock.Unlock()
	if codeCommitClient == nil {
		codeCommitClient = codecommit.NewFromConfig(*Session())
	}
	return codeCommitClient
}

func CodeCommitListRepos(ctx context.Context) ([]codecommittypes.RepositoryNameIdPair, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "CodeCommitListRepos"}
		d.Start()
		defer d.End()
	}
	var token *string
	var repos []codecommittypes.RepositoryNameIdPair
	for {
		out, err := CodeCommitClient().ListRepositories(ctx, &codecommit.ListRepositoriesInput{
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
