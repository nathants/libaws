package cliaws

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/buger/goterm"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-speedtest"] = ec2Speedtest
	lib.Args["ec2-speedtest"] = ec2SpeedtestArgs{}
}

type ec2SpeedtestArgs struct {
	Type string `arg:"-t,--type" default:"c6g.xlarge"`
	Key  string `arg:"-k,--key,required"`
	Sg   string `arg:"--sg,required" help:"security group name or id"`
	Vpc  string `arg:"-v,--vpc,required" help:"vpc name or id. specify instead of --subnet-ids"`
}

func (ec2SpeedtestArgs) Description() string {
	return "\nssh speedtest to an ec2 instance\n"
}

func ec2Speedtest() {
	var args ec2SpeedtestArgs
	arg.MustParse(&args)
	ctx := context.Background()
	if !strings.HasPrefix(args.Sg, "sg-") {
		sgID, err := lib.EC2SgID(ctx, args.Vpc, args.Sg)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		args.Sg = sgID
	}
	if !strings.HasPrefix("vpc-", args.Vpc) {
		vpcID, err := lib.VpcID(ctx, args.Vpc)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		args.Vpc = vpcID
	}
	subnets, err := lib.VpcSubnets(ctx, args.Vpc)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	var subnetIDs []string
	for _, subnet := range subnets {
		subnetIDs = append(subnetIDs, *subnet.SubnetId)
	}
	arch := lib.EC2ArchAmd64
	if strings.Contains(strings.Split(args.Type, ".")[0][1:], "g") { // slice first char, since arm64 g is never first char
		arch = lib.EC2ArchArm64
	}
	amiID, sshUser, err := lib.EC2AmiBase(ctx, lib.EC2AmiAlpine, arch)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	instances, err := lib.EC2RequestSpotFleet(ctx, ec2.AllocationStrategyLowestPrice, &lib.EC2Config{
		NumInstances:   1,
		Name:           "speedtest",
		SgID:           args.Sg,
		SubnetIds:      subnetIDs,
		AmiID:          amiID,
		UserName:       sshUser,
		Key:            args.Key,
		InstanceType:   args.Type,
		Gigs:           1,
		SecondsTimeout: 300,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	var selectors []string
	for _, instance := range instances {
		selectors = append(selectors, *instance.InstanceId)
	}
	_, err = lib.EC2WaitSsh(ctx, &lib.EC2WaitSshInput{
		Selectors:      selectors,
		MaxWaitSeconds: 300,
		User:           sshUser,
		MaxConcurrency: 1,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	instances, err = lib.EC2ListInstances(ctx, selectors, ec2.InstanceStateNameRunning)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	//
	cmd := exec.Command(
		"ssh",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "StrictHostKeyChecking=no",
		sshUser+"@"+*instances[0].PublicDnsName,
		"cat /dev/zero",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	go func() {
		// defer func() {}()
		err := cmd.Run()
		if err != nil && err.Error() != "signal: killed" {
			lib.Logger.Println("error: ", err)
		}
	}()
	buf := make([]byte, 1000*1000*5)
	start := time.Now()
	last := time.Now()
	count := 0
	var values []float64
	for {
		n, err := stdout.Read(buf)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		count += n
		if time.Since(last) >= 1*time.Second {
			value := float64(count/1000/1000) / time.Since(last).Seconds()
			values = append(values, value)
			goterm.MoveCursorUp(1)
			_, _ = goterm.Println(goterm.RESET_LINE + fmt.Sprintf("%.2f MB/s download", value))
			goterm.Flush()
			last = time.Now()
			count = 0
		}
		if time.Since(start) > 10*time.Second {
			_ = cmd.Process.Kill()
			value := float64(0)
			for _, v := range values {
				value += v
			}
			value = value / float64(len(values))
			goterm.MoveCursorUp(1)
			_, _ = goterm.Println(goterm.RESET_LINE + fmt.Sprintf("%.2f MB/s download", value))
			goterm.Flush()
			break
		}
	}
	//
	cmd = exec.Command(
		"ssh",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "StrictHostKeyChecking=no",
		sshUser+"@"+*instances[0].PublicDnsName,
		"cat > /dev/null",
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	go func() {
		// defer func() {}()
		err := cmd.Run()
		if err != nil && err.Error() != "signal: killed" {
			lib.Logger.Println("error: ", err)
		}
	}()
	buf = make([]byte, 1000*1000*5)
	start = time.Now()
	last = time.Now()
	count = 0
	values = []float64{}
	_, _ = goterm.Println()
	for {
		n, err := stdin.Write(buf)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		count += n
		if time.Since(last) >= 1*time.Second {
			value := float64(count/1000/1000) / time.Since(last).Seconds()
			values = append(values, value)
			goterm.MoveCursorUp(1)
			_, _ = goterm.Println(goterm.RESET_LINE + fmt.Sprintf("%.2f MB/s upload", value))
			goterm.Flush()
			last = time.Now()
			count = 0
		}
		if time.Since(start) > 10*time.Second {
			_ = cmd.Process.Kill()
			goterm.MoveCursorUp(1)
			value := float64(0)
			for _, v := range values {
				value += v
			}
			value = value / float64(len(values))
			_, _ = goterm.Println(goterm.RESET_LINE + fmt.Sprintf("%.2f MB/s upload", value))
			goterm.Flush()
			break
		}
	}
	_, err = lib.EC2Client().TerminateInstancesWithContext(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice([]string{*instances[0].InstanceId}),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
