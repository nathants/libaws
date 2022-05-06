package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["iam-ls-instance-profiles"] = iamLsInstanceProfiles
	lib.Args["iam-ls-instance-profiles"] = iamLsInstanceProfilesArgs{}
}

type iamLsInstanceProfilesArgs struct {
}

func (iamLsInstanceProfilesArgs) Description() string {
	return "\nlist iam instance profiles\n"
}

func iamLsInstanceProfiles() {
	var args iamLsInstanceProfilesArgs
	arg.MustParse(&args)
	ctx := context.Background()
	profiles, err := lib.IamListInstanceProfiles(ctx, nil)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, profile := range profiles {
		fmt.Println(lib.Pformat(profile))
	}
}
