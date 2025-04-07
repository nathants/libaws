package libaws

import (
	"context"
	"fmt"
	"sort"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["cloudwatch-ls-namespaces"] = cloudwatchLsNamespaces
	lib.Args["cloudwatch-ls-namespaces"] = cloudwatchLsNamespacesArgs{}
}

type cloudwatchLsNamespacesArgs struct {
}

func (cloudwatchLsNamespacesArgs) Description() string {
	return "\nlist cloudwatch namespaces\n"
}

func cloudwatchLsNamespaces() {
	var args cloudwatchLsNamespacesArgs
	arg.MustParse(&args)
	ctx := context.Background()
	metrics, err := lib.CloudwatchListMetrics(ctx, nil, nil)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	namespacesMap := make(map[string]any)
	for _, m := range metrics {
		namespacesMap[*m.Namespace] = nil
	}
	var namespaces []string
	for n := range namespacesMap {
		namespaces = append(namespaces, n)
	}
	sort.Strings(namespaces)
	for _, n := range namespaces {
		fmt.Println(n)
	}
}
