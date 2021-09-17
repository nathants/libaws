package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["organizations-ensure"] = organizationsEnsure
	lib.Args["organizations-ensure"] = organizationsEnsureArgs{}
}

type organizationsEnsureArgs struct {
	Name    string `arg:"positional,required"`
	Email   string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (organizationsEnsureArgs) Description() string {
	return "\nensure a sub account\n"
}

func organizationsEnsure() {
	var args organizationsEnsureArgs
	arg.MustParse(&args)
	ctx := context.Background()
	accountID, err := lib.OrganizationsEnsure(ctx, args.Name, args.Email, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(accountID)
}
