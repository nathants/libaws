package lib

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"strings"
	"sync"
)

var iamClient *iam.IAM
var iamClientLock sync.RWMutex

func IamClient() *iam.IAM {
	iamClientLock.Lock()
	defer iamClientLock.Unlock()
	if iamClient == nil {
		iamClient = iam.New(Session())
	}
	return iamClient
}

func iamAllowPolicyDocument(action, resource string) *string {
	return aws.String(`{"Version": "2012-10-17",
                        "Statement": [{"Effect": "Allow",
                                       "Action": "` + action + `",
                                       "Resource": "` + resource + `"}]}`)
}

func iamAllowPolicyName(action, resource string) *string {
	action = strings.ReplaceAll(action, "*", "ALL")
	resource = strings.ReplaceAll(resource, "*", "")
	var parts []string
	for _, part := range strings.Split(resource, ":")[3:] { // arn:aws:service:account:region:target
		parts = append(parts, last(strings.Split(part, "/")))
	}
	resource = strings.Join(parts, ":")
	name := action + "_" + resource
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.TrimRight(name, "_")
	return aws.String(name)
}

func IamListPolicies(ctx context.Context) ([]*iam.Policy, error) {
	var policies []*iam.Policy
	var marker *string
	for {
		out, err := IamClient().ListPoliciesWithContext(ctx, &iam.ListPoliciesInput{
			Marker: marker,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		policies = append(policies, out.Policies...)
		if !*out.IsTruncated {
			break
		}
		marker = out.Marker
	}
	return policies, nil
}

func IamListRoles(ctx context.Context, pathPrefix *string) ([]*iam.Role, error) {
	var roles []*iam.Role
	var marker *string
	for {
		out, err := IamClient().ListRolesWithContext(ctx, &iam.ListRolesInput{
			Marker:     marker,
			PathPrefix: pathPrefix,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		roles = append(roles, out.Roles...)
		if !*out.IsTruncated {
			break
		}
		marker = out.Marker
	}
	return roles, nil
}

func IamEnsureRoleAllows(ctx context.Context, roleName string, allows []string, preview bool) error {
	if len(allows) == 0 {
		return nil
	}
	for _, allow := range allows {
		parts := strings.SplitN(allow, " ", 2)
		if len(parts) != 2 {
			err := fmt.Errorf("allow format should be: 'SERVICE:ACTION RESOURCE', got: %s", allow)
			Logger.Println("error:", err)
			return err
		}
		action := parts[0]
		resource := parts[1]
		if preview {
			Logger.Println("preview: ensure role allow:", roleName, allow)
		} else {
			_, err := IamClient().PutRolePolicyWithContext(ctx, &iam.PutRolePolicyInput{
				RoleName:       aws.String(roleName),
				PolicyName:     iamAllowPolicyName(action, resource),
				PolicyDocument: iamAllowPolicyDocument(action, resource),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			Logger.Println("ensure role allow:", roleName, allow)
		}
	}
	return nil
}

func IamEnsureRolePolicies(ctx context.Context, roleName string, policyNames []string, preview bool) error {
	if len(policyNames) == 0 {
		return nil
	}
	for _, policyName := range policyNames {
		if preview {
			Logger.Println("preview: ensure role policy:", roleName, policyName)
		} else {
			policies, err := IamListPolicies(ctx)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			var matchedPolicies []*iam.Policy
			for _, policy := range policies {
				if last(strings.Split(*policy.Arn, "/")) == policyName {
					matchedPolicies = append(matchedPolicies, policy)
				}
			}
			switch len(matchedPolicies) {
			case 0:
				err := fmt.Errorf("didn't find policy for name: %s", policyName)
				Logger.Println("error:", err)
				return err
			case 1:
				_, err := IamClient().AttachRolePolicyWithContext(ctx, &iam.AttachRolePolicyInput{
					PolicyArn: matchedPolicies[0].Arn,
					RoleName:  aws.String(roleName),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			default:
				err := fmt.Errorf("found more than 1 policy for name: %s", policyName)
				Logger.Println("error:", err)
				for _, policy := range matchedPolicies {
					Logger.Println("error:", policy.Arn)
				}
				return err
			}
			Logger.Println("ensure role policy:", roleName, policyName)
		}
	}
	return nil
}

func iamAssumePolicyDocument(principalName string) *string {
	return aws.String(`{"Version": "2012-10-17",
                        "Statement": [{"Effect": "Allow",
                                       "Principal": {"Service": "` + principalName + `.amazonaws.com"},
                                       "Action": "sts:AssumeRole"}]}`)
}

func IamEnsureRole(ctx context.Context, roleName, principalName string, preview bool) error {
	if preview {
		Logger.Println("preview: ensure role:", roleName, principalName)
	} else {
		rolePath := fmt.Sprintf("/%s/%s-path/", principalName, roleName)
		roles, err := IamListRoles(ctx, aws.String(rolePath))
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		switch len(roles) {
		case 0:
			_, err := IamClient().CreateRoleWithContext(ctx, &iam.CreateRoleInput{
				Path:                     aws.String(rolePath),
				AssumeRolePolicyDocument: iamAssumePolicyDocument(principalName),
				RoleName:                 aws.String(roleName),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		case 1:
			if *roles[0].Path != rolePath {
				err := fmt.Errorf("role path mismatch: %s %s != %s", roleName, *roles[0].Path, rolePath)
				Logger.Println("error:", err)
				return err
			}
			if *roles[0].AssumeRolePolicyDocument != *iamAssumePolicyDocument(principalName) {
				err := fmt.Errorf("role policy mismatch: %s %s != %s", roleName, *roles[0].AssumeRolePolicyDocument, *iamAssumePolicyDocument(principalName))
				Logger.Println("error:", err)
				return err
			}
			if *roles[0].RoleName != roleName {
				err := fmt.Errorf("role name mismatch: %s != %s", *roles[0].RoleName, roleName)
				Logger.Println("error:", err)
				return err
			}
		default:
			err := fmt.Errorf("found more than 1 role under path: %s", rolePath)
			Logger.Println("error:", err)
			for _, role := range roles {
				Logger.Println("error:", role.Arn)
			}
			return err
		}
		Logger.Println("ensure role:", roleName, principalName)
	}
	return nil
}

func IamEnsureInstanceProfileRole(ctx context.Context, profileName, roleName string, preview bool) {
}
