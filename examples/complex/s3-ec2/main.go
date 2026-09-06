package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/nathants/libaws/lib"
)

var (
	uid         = os.Getenv("uid")
	keypairName = "test-keypair-" + uid
	profileName = "test-profile-" + uid
	ec2Name     = "test-ec2-" + uid
	vpcName     = "test-vpc-" + uid
	sgName      = "test-sg-" + uid
	inBucket    = "in-bucket-" + uid
	outBucket   = "out-bucket-" + uid
)

const initTemplate = `#!/bin/bash
(
    set -xeou pipefail
    trap 'sudo poweroff' EXIT
    python3 - <<'PY'
import base64
import hashlib
import pathlib
import subprocess
import urllib.request
import zipfile

aws_cli_url = "https://awscli.amazonaws.com/awscli-exe-linux-x86_64-2.36.28.zip"
aws_cli_sha256 = "1e050540227bc4dca8c2e9d503e358758dc4edd647f68e7f1a3899be6fc74bf6"
archive = pathlib.Path("/tmp/awscliv2.zip")
digest = hashlib.sha256()
with urllib.request.urlopen(aws_cli_url, timeout=120) as response, archive.open("wb") as output:
    while chunk := response.read(1024 * 1024):
        digest.update(chunk)
        output.write(chunk)
if digest.hexdigest() != aws_cli_sha256:
    raise RuntimeError("AWS CLI archive digest mismatch")
with zipfile.ZipFile(archive) as package:
    for member in package.infolist():
        extracted = pathlib.Path(package.extract(member, "/tmp/awscliv2"))
        mode = member.external_attr >> 16
        if mode:
            extracted.chmod(mode & 0o777)

aws = "/tmp/awscliv2/aws/dist/aws"
in_bucket, in_key, out_bucket, out_key = [
    base64.b64decode(value, validate=True).decode("utf-8")
    for value in ("%s", "%s", "%s", "%s")
]
input_path = pathlib.Path("/tmp/input")
output_path = pathlib.Path("/tmp/output")
subprocess.run([aws, "s3", "cp", f"s3://{in_bucket}/{in_key}", input_path], check=True)
output_path.write_bytes(input_path.read_bytes().rstrip(b"\n") + b" from ec2\n")
subprocess.run([aws, "s3", "cp", output_path, f"s3://{out_bucket}/{out_key}"], check=True)
PY
) &> /tmp/log.txt
`

func formatInitTemplate(inBucket, inKey, outBucket, outKey string) string {
	encode := func(value string) string {
		return base64.StdEncoding.EncodeToString([]byte(value))
	}
	return fmt.Sprintf(initTemplate,
		encode(inBucket),
		encode(inKey),
		encode(outBucket),
		encode(outKey),
	)
}

func handleRequest(ctx context.Context, event events.S3Event) (events.APIGatewayProxyResponse, error) {
	sgID, err := lib.EC2SgID(ctx, vpcName, sgName)
	if err != nil {
		panic(err)
	}
	amiID, user, err := lib.EC2AmiBase(ctx, lib.EC2AmiDebianTrixie, lib.EC2ArchAmd64)
	if err != nil {
		panic(err)
	}
	spotSubnetIDs, err := lib.EC2SubnetsFromVpc(ctx, vpcName, ec2types.InstanceTypeT3Small, true)
	if err != nil {
		panic(err)
	}
	for _, record := range event.Records {
		key := record.S3.Object.URLDecodedKey
		_, err := lib.EC2RequestSpotFleet(ctx, ec2types.AllocationStrategyLowestPrice, &lib.EC2Config{
			NumInstances:   1,
			SgID:           sgID,
			SubnetIds:      spotSubnetIDs,
			Name:           ec2Name + ":" + key,
			AmiID:          amiID,
			Key:            keypairName,
			Profile:        profileName,
			UserName:       user,
			InstanceType:   ec2types.InstanceTypeT3Small,
			SecondsTimeout: 900,
			Gigs:           8,
			Init:           formatInitTemplate(inBucket, key, outBucket, key),
		})
		if err != nil {
			panic(err)
		}
	}
	return events.APIGatewayProxyResponse{StatusCode: 200}, nil
}

func main() {
	lambda.Start(handleRequest)
}
