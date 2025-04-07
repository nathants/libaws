package libaws

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ecr-login"] = ecrLogin
	lib.Args["ecr-login"] = ecrLoginArgs{}
}

type ecrLoginArgs struct {
}

func (ecrLoginArgs) Description() string {
	return "\nlogin to docker\n"
}

func ecrLogin() {
	var args ecrLoginArgs
	arg.MustParse(&args)
	ctx := context.Background()
	token, err := lib.EcrClient().GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	bytes, err := base64.StdEncoding.DecodeString(*token.AuthorizationData[0].AuthorizationToken)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	parts := strings.Split(string(bytes), ":")
	if len(parts) != 2 {
		err := fmt.Errorf("expected two parts")
		lib.Logger.Fatal("error: ", err)
	}
	password := parts[1]
	endpoint := *token.AuthorizationData[0].ProxyEndpoint
	cmd := exec.Command("docker", "login", "--username", "AWS", "--password", password, endpoint)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
