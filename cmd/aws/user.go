package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["aws-user"] = user
	lib.Args["aws-user"] = userArgs{}
}

type userArgs struct {
}

func (userArgs) Description() string {
	return "\ncurrent iam user name\n"
}

func user() {
	var args userArgs
	arg.MustParse(&args)
	ctx := context.Background()
	user, err := lib.StsUser(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println(user)
}
