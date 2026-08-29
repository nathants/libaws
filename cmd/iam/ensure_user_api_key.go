package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["iam-ensure-user-api-key"] = iamEnsureUserApiKey
	lib.Args["iam-ensure-user-api-key"] = iamEnsureUserApiKeyArgs{}
}

type iamEnsureUserApiKeyArgs struct {
	Name    string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (iamEnsureUserApiKeyArgs) Description() string {
	return "\nensure an existing IAM user has one API access key\n"
}

func iamEnsureUserApiKey() {
	var args iamEnsureUserApiKeyArgs
	arg.MustParse(&args)
	out, err := lib.IamEnsureUserApiKey(context.Background(), args.Name, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if out.AccessKeyId != nil {
		fmt.Println("access key id:", *out.AccessKeyId)
		fmt.Println("access key secret:", *out.SecretAccessKey)
	}
}
