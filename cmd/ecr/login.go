package libaws

import (
	"context"
	"encoding/base64"
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
	if token == nil || len(token.AuthorizationData) != 1 {
		lib.Logger.Fatal("error: ECR returned invalid authorization data")
	}
	authorization := token.AuthorizationData[0]
	if authorization.AuthorizationToken == nil || authorization.ProxyEndpoint == nil {
		lib.Logger.Fatal("error: ECR returned incomplete authorization data")
	}
	credentials, err := base64.StdEncoding.DecodeString(*authorization.AuthorizationToken)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	username, password, ok := strings.Cut(string(credentials), ":")
	if !ok || username == "" || password == "" {
		lib.Logger.Fatal("error: invalid ECR authorization token")
	}
	endpoint := *authorization.ProxyEndpoint
	cmd := exec.Command("docker", "login", "--username", username, "--password-stdin", endpoint)
	cmd.Stdin = strings.NewReader(password)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
