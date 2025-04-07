package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	orgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["organizations-ls"] = organizationsLs
	lib.Args["organizations-ls"] = organizationsLsArgs{}
}

type organizationsLsArgs struct {
}

func (organizationsLsArgs) Description() string {
	return "\nlist sub accounts\n"
}

func organizationsLs() {
	var args organizationsLsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	var token *string
	var accounts []orgtypes.Account
	for {
		out, err := lib.OrganizationsClient().ListAccounts(ctx, &organizations.ListAccountsInput{
			NextToken: token,
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		accounts = append(accounts, out.Accounts...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}

	for _, account := range accounts {
		fmt.Println(lib.Pformat(account))
	}
}
