package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["events-ls-buses"] = eventsLsBuses
	lib.Args["events-ls-buses"] = eventsLsBusesArgs{}
}

type eventsLsBusesArgs struct {
}

func (eventsLsBusesArgs) Description() string {
	return "\nlist event buses\n"
}

func eventsLsBuses() {
	var args eventsLsBusesArgs
	arg.MustParse(&args)
	ctx := context.Background()
	buses, err := lib.EventsListBuses(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, bus := range buses {
		fmt.Println(*bus.Name)
	}
}
