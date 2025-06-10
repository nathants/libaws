package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
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
	fmt.Println("account-id:", accountID)
	fmt.Println("setup root user login for account via password reset at https://console.aws.amazon.com/ with email:", args.Email)
}
