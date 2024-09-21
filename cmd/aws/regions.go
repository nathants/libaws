package cliaws

import (
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["aws-regions"] = regions
	lib.Args["aws-regions"] = regionsArgs{}
}

type regionsArgs struct {
}

func (regionsArgs) Description() string {
	return "\nregion ids\n"
}

func regions() {
	var args regionArgs
	arg.MustParse(&args)
	awsRegions, err := lib.Regions()
	if err != nil {
	    lib.Logger.Fatal("error: ", err)
	}
	for _, region := range awsRegions {
		fmt.Println(region)
	}
}
