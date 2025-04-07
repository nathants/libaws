package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ecr-ensure"] = ecrEnsure
	lib.Args["ecr-ensure"] = ecrEnsureArgs{}
}

type ecrEnsureArgs struct {
	Name    string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (ecrEnsureArgs) Description() string {
	return "\nensure ecr image\n"
}

func ecrEnsure() {
	var args ecrEnsureArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.EcrEnsure(ctx, args.Name, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
