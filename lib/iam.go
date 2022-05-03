package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
)

var iamClient *iam.IAM
var iamClientLock sync.RWMutex

func IamClientExplicit(accessKeyID, accessKeySecret, region string) *iam.IAM {
	return iam.New(SessionExplicit(accessKeyID, accessKeySecret, region))
}

func IamClient() *iam.IAM {
	iamClientLock.Lock()
	defer iamClientLock.Unlock()
	if iamClient == nil {
		iamClient = iam.New(Session())
	}
	return iamClient
}

type IamAllow struct {
	Action   string
	Resource string
}

func (a *IamAllow) String() string {
	return fmt.Sprintf("%s %s", a.Action, a.Resource)
}

func (a *IamAllow) policyDocument() string {
	return `{"Version": "2012-10-17",
             "Statement": [{"Effect": "Allow",
                            "Action": "` + a.Action + `",
                            "Resource": "` + a.Resource + `"}]}`
}

func (allow *IamAllow) policyName() string {
	action := strings.ReplaceAll(allow.Action, "*", "ALL")
	resource := strings.ReplaceAll(allow.Resource, "*", "ALL")
	var parts []string
	for _, part := range strings.Split(resource, ":") { // arn:aws:service:account:region:target
		if !Contains([]string{"arn", "aws", "s3", "dynamodb", "sqs", "sns"}, part) {
			parts = append(parts, strings.ReplaceAll(part, "/", "__"))
		}
	}
	resource = strings.Join(parts, ":")
	name := action + "__" + resource
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.TrimRight(name, "_")
	return name
}

func iamAllowFromPolicyDocument(policyDocument string) (*IamAllow, error) {
	policy := IamPolicyDocument{}
	err := json.Unmarshal([]byte(policyDocument), &policy)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	if len(policy.Statement) != 1 {
		err := fmt.Errorf("expected 1 statement, got 0: %s", policyDocument)
		Logger.Println("error:", err)
		return nil, err
	}
	if policy.Statement[0].Effect != "Allow" {
		err := fmt.Errorf("expected 1 Allow statement, got: %s", policyDocument)
		Logger.Println("error:", err)
		return nil, err
	}
	allow := &IamAllow{
		Action:   policy.Statement[0].Action.(string),
		Resource: policy.Statement[0].Resource.(string),
	}
	return allow, nil
}

type IamPolicy struct {
	Arn                           *string            `json:",omitempty"`
	AttachmentCount               *int64             `json:",omitempty"`
	CreateDate                    *time.Time         `json:",omitempty"`
	DefaultVersionId              *string            `json:",omitempty"`
	Description                   *string            `json:",omitempty"`
	IsAttachable                  *bool              `json:",omitempty"`
	Path                          *string            `json:",omitempty"`
	PermissionsBoundaryUsageCount *int64             `json:",omitempty"`
	PolicyId                      *string            `json:",omitempty"`
	PolicyName                    *string            `json:",omitempty"`
	Tags                          []*iam.Tag         `json:",omitempty"`
	UpdateDate                    *time.Time         `json:",omitempty"`
	PolicyDocument                *IamPolicyDocument `json:",omitempty"`
}

func (p *IamPolicy) FromPolicy(ctx context.Context, policy *iam.Policy, resolveDocument bool) error {
	p.Arn = policy.Arn
	p.AttachmentCount = policy.AttachmentCount
	p.CreateDate = policy.CreateDate
	p.DefaultVersionId = policy.DefaultVersionId
	p.Description = policy.Description
	p.IsAttachable = policy.IsAttachable
	p.Path = policy.Path
	p.PermissionsBoundaryUsageCount = policy.PermissionsBoundaryUsageCount
	p.PolicyId = policy.PolicyId
	p.PolicyName = policy.PolicyName
	p.Tags = policy.Tags
	p.UpdateDate = policy.UpdateDate
	if resolveDocument {
		p.PolicyDocument = &IamPolicyDocument{}
		out, err := IamClient().GetPolicyVersionWithContext(ctx, &iam.GetPolicyVersionInput{
			PolicyArn: policy.Arn,
			VersionId: policy.DefaultVersionId,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		document, err := url.QueryUnescape(*out.PolicyVersion.Document)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = json.Unmarshal([]byte(document), p.PolicyDocument)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func IamDeleteUser(ctx context.Context, name string, preview bool) error {
	_, err := IamClient().GetUserWithContext(ctx, &iam.GetUserInput{
		UserName: aws.String(name),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
			Logger.Println("error:", err)
			return err
		}
		return nil
	}
	if !preview {
		out, err := IamClient().ListAccessKeysWithContext(ctx, &iam.ListAccessKeysInput{
			UserName: aws.String(name),
			MaxItems: aws.Int64(100),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		if len(out.AccessKeyMetadata) == 100 {
			err := fmt.Errorf("TODO paginate keys")
			Logger.Println("error:", err)
			return err
		}
		for _, key := range out.AccessKeyMetadata {
			_, err := IamClient().DeleteAccessKeyWithContext(ctx, &iam.DeleteAccessKeyInput{
				UserName:    aws.String(name),
				AccessKeyId: key.AccessKeyId,
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		err = IamEnsureUserAllows(ctx, name, []string{}, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = IamEnsureUserPolicies(ctx, name, []string{}, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		_, err = IamClient().DeleteUserWithContext(ctx, &iam.DeleteUserInput{
			UserName: aws.String(name),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"deleted User:", name)
	return nil
}

func IamDeleteRole(ctx context.Context, name string, preview bool) error {
	_, err := IamClient().GetRoleWithContext(ctx, &iam.GetRoleInput{
		RoleName: aws.String(name),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
			Logger.Println("error:", err)
			return err
		}
		return nil
	}
	if !preview {
		err := IamEnsureRoleAllows(ctx, name, []string{}, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = IamEnsureRolePolicies(ctx, name, []string{}, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		_, err = IamClient().DeleteRoleWithContext(ctx, &iam.DeleteRoleInput{
			RoleName: aws.String(name),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"deleted role:", name)
	return nil
}

func IamDeleteInstanceProfile(ctx context.Context, name string, preview bool) error {
	_, err := IamClient().GetInstanceProfileWithContext(ctx, &iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(name),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
			Logger.Println("error:", err)
			return err
		}
		return nil
	}
	if !preview {
		err := IamEnsureRoleAllows(ctx, name, []string{}, preview)
		if err != nil {
			aerr, ok := err.(awserr.Error)
			if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
				Logger.Println("error:", err)
				return err
			}
		}
		err = IamEnsureRolePolicies(ctx, name, []string{}, preview)
		if err != nil {
			aerr, ok := err.(awserr.Error)
			if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
				Logger.Println("error:", err)
				return err
			}
		}
		_, err = IamClient().RemoveRoleFromInstanceProfileWithContext(ctx, &iam.RemoveRoleFromInstanceProfileInput{
			InstanceProfileName: aws.String(name),
			RoleName:            aws.String(name),
		})
		if err != nil {
			aerr, ok := err.(awserr.Error)
			if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
				Logger.Println("error:", err)
				return err
			}
		}
		_, err = IamClient().DeleteRoleWithContext(ctx, &iam.DeleteRoleInput{
			RoleName: aws.String(name),
		})
		if err != nil {
			aerr, ok := err.(awserr.Error)
			if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
				Logger.Println("error:", err)
				return err
			}
		}
		_, err = IamClient().DeleteInstanceProfileWithContext(ctx, &iam.DeleteInstanceProfileInput{
			InstanceProfileName: aws.String(name),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"deleted instance profile:", name)
	return nil
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
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return policies, nil
}

func IamListRoles(ctx context.Context, pathPrefix *string) ([]*IamRole, error) {
	var roles []*IamRole
	var marker *string
	input := &iam.ListRolesInput{}
	if pathPrefix != nil {
		input.PathPrefix = pathPrefix
	}
	for {
		input.Marker = marker
		out, err := IamClient().ListRolesWithContext(ctx, input)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, role := range out.Roles {
			r := &IamRole{}
			err := r.FromRole(role)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			roles = append(roles, r)
		}
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return roles, nil
}

func IamEnsureUserAllows(ctx context.Context, username string, allows []string, preview bool) error {
	var allowNames []string
	for _, allowStr := range allows {
		parts := strings.SplitN(allowStr, " ", 2)
		if len(parts) != 2 {
			err := fmt.Errorf("allow format should be: 'SERVICE:ACTION RESOURCE', got: %s", allowStr)
			Logger.Println("error:", err)
			return err
		}
		allow := &IamAllow{
			Action:   parts[0],
			Resource: parts[1],
		}
		allowNames = append(allowNames, allow.policyName())
		out, err := IamClient().GetUserPolicyWithContext(ctx, &iam.GetUserPolicyInput{
			UserName:   aws.String(username),
			PolicyName: aws.String(allow.policyName()),
		})
		if err != nil {
			aerr, ok := err.(awserr.Error)
			if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
				Logger.Println("error:", err)
				return err
			}
		} else {
			document, err := url.QueryUnescape(*out.PolicyDocument)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			equal, err := iamPolicyEqual(document, allow.policyDocument())
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			if equal {
				continue
			}
		}
		if !preview {
			_, err := IamClient().PutUserPolicyWithContext(ctx, &iam.PutUserPolicyInput{
				UserName:       aws.String(username),
				PolicyName:     aws.String(allow.policyName()),
				PolicyDocument: aws.String(allow.policyDocument()),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"attached user allow:", username, allow)
	}
	attachedAllows, err := IamListUserAllows(ctx, username)
	if err != nil && !preview {
		Logger.Println("error:", err)
		return err
	}
	for _, allow := range attachedAllows {
		if !Contains(allowNames, allow.policyName()) {
			if !preview {
				_, err := IamClient().DeleteUserPolicyWithContext(ctx, &iam.DeleteUserPolicyInput{
					UserName:   aws.String(username),
					PolicyName: aws.String(allow.policyName()),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"detach user allow:", username, allow)
		}
	}
	return nil
}

func IamEnsureRoleAllows(ctx context.Context, roleName string, allows []string, preview bool) error {
	var allowNames []string
	for _, allowStr := range allows {
		_, allowStr, err := resolveEnvVars(allowStr, []string{}) // resolve again since lambdaEnvVarApiID and lambdaEnvVarWebsocketID are not set
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		parts := strings.SplitN(allowStr, " ", 2)
		if len(parts) != 2 {
			err := fmt.Errorf("allow format should be: 'SERVICE:ACTION RESOURCE', got: %s", allowStr)
			Logger.Println("error:", err)
			return err
		}
		allow := &IamAllow{
			Action:   parts[0],
			Resource: parts[1],
		}
		allowNames = append(allowNames, allow.policyName())
		out, err := IamClient().GetRolePolicyWithContext(ctx, &iam.GetRolePolicyInput{
			RoleName:   aws.String(roleName),
			PolicyName: aws.String(allow.policyName()),
		})
		if err != nil {
			aerr, ok := err.(awserr.Error)
			if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
				Logger.Println("error:", err)
				return err
			}
		} else {
			document, err := url.QueryUnescape(*out.PolicyDocument)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			equal, err := iamPolicyEqual(document, allow.policyDocument())
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			if equal {
				continue
			}
		}
		if !preview {
			_, err := IamClient().PutRolePolicyWithContext(ctx, &iam.PutRolePolicyInput{
				RoleName:       aws.String(roleName),
				PolicyName:     aws.String(allow.policyName()),
				PolicyDocument: aws.String(allow.policyDocument()),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"attached role allow:", roleName, allow)
	}
	attachedAllows, err := IamListRoleAllows(ctx, roleName)
	if err != nil && !preview {
		return err
	}
	for _, allow := range attachedAllows {
		if !Contains(allowNames, allow.policyName()) {
			if !preview {
				_, err := IamClient().DeleteRolePolicyWithContext(ctx, &iam.DeleteRolePolicyInput{
					RoleName:   aws.String(roleName),
					PolicyName: aws.String(allow.policyName()),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"detach role allow:", roleName, allow)
		}
	}
	return nil
}

func iamPolicyEqual(a, b string) (bool, error) {
	aData := make(map[string]interface{})
	bData := make(map[string]interface{})
	err := json.Unmarshal([]byte(a), &aData)
	if err != nil {
		return false, err
	}
	err = json.Unmarshal([]byte(b), &bData)
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(aData, bData), nil
}

func IamEnsureUserPolicies(ctx context.Context, username string, policyNames []string, preview bool) error {
	policies, err := IamListPolicies(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
outer:
	for _, policyName := range policyNames {
		var matchedPolicies []*iam.Policy
		for _, policy := range policies {
			if Last(strings.Split(*policy.Arn, "/")) == policyName {
				matchedPolicies = append(matchedPolicies, policy)
			}
		}
		switch len(matchedPolicies) {
		case 0:
			err := fmt.Errorf("didn't find policy for name: %s", policyName)
			Logger.Println("error:", err)
			return err
		case 1:
			userPolicies, err := IamListUserPolicies(ctx, username)
			if err != nil && !preview {
				Logger.Println("error:", err)
				return err
			}
			for _, policy := range userPolicies {
				if *policy.PolicyName == policyName {
					continue outer
				}
			}
			if !preview {
				_, err = IamClient().AttachUserPolicyWithContext(ctx, &iam.AttachUserPolicyInput{
					PolicyArn: matchedPolicies[0].Arn,
					UserName:  aws.String(username),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"attached user policy:", username, policyName)
		default:
			err := fmt.Errorf("found more than 1 policy for name: %s", policyName)
			Logger.Println("error:", err)
			for _, policy := range matchedPolicies {
				Logger.Println("error:", *policy.Arn)
			}
			return err
		}
	}
	attachedPolicies, err := IamListUserPolicies(ctx, username)
	if err != nil && !preview {
		Logger.Println("error:", err)
		return err
	}
	for _, policy := range attachedPolicies {
		if !Contains(policyNames, *policy.PolicyName) {
			if !preview {
				_, err := IamClient().DetachUserPolicyWithContext(ctx, &iam.DetachUserPolicyInput{
					UserName:  aws.String(username),
					PolicyArn: policy.Arn,
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"detached user policy:", username, *policy.PolicyName)
		}
	}
	return nil
}

func IamEnsureRolePolicies(ctx context.Context, roleName string, policyNames []string, preview bool) error {
	policies, err := IamListPolicies(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
outer:
	for _, policyName := range policyNames {
		var matchedPolicies []*iam.Policy
		for _, policy := range policies {
			if Last(strings.Split(*policy.Arn, "/")) == policyName {
				matchedPolicies = append(matchedPolicies, policy)
			}
		}
		switch len(matchedPolicies) {
		case 0:
			err := fmt.Errorf("didn't find policy for name: %s", policyName)
			Logger.Println("error:", err)
			return err
		case 1:
			rolePolicies, err := IamListRolePolicies(ctx, roleName)
			if err != nil && !preview {
				Logger.Println("error:", err)
				return err
			}
			for _, policy := range rolePolicies {
				if *policy.PolicyName == policyName {
					continue outer
				}
			}
			if !preview {
				_, err = IamClient().AttachRolePolicyWithContext(ctx, &iam.AttachRolePolicyInput{
					PolicyArn: matchedPolicies[0].Arn,
					RoleName:  aws.String(roleName),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"attached role policy:", roleName, policyName)
		default:
			err := fmt.Errorf("found more than 1 policy for name: %s", policyName)
			Logger.Println("error:", err)
			for _, policy := range matchedPolicies {
				Logger.Println("error:", *policy.Arn)
			}
			return err
		}
	}
	attachedPolicies, err := IamListRolePolicies(ctx, roleName)
	if err != nil && !preview {
		return err
	}
	for _, policy := range attachedPolicies {
		policyName := Last(strings.Split(*policy.PolicyArn, "/"))
		if !Contains(policyNames, policyName) {
			if !preview {
				_, err := IamClient().DetachRolePolicyWithContext(ctx, &iam.DetachRolePolicyInput{
					RoleName:  aws.String(roleName),
					PolicyArn: policy.PolicyArn,
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"detached role policy:", roleName, policyName)
		}
	}
	return nil
}

func iamAssumePolicyDocument(principalName string) (*string, error) {
	if strings.Contains(principalName, ".") {
		err := fmt.Errorf("principal should be '$name', not '$name.amazonaws.com', got: %s", principalName)
		Logger.Println("error:", err)
		return nil, err
	}
	return aws.String(`{"Version": "2012-10-17",
                        "Statement": [{"Effect": "Allow",
                                       "Principal": {"Service": "` + principalName + `.amazonaws.com"},
                                       "Action": "sts:AssumeRole"}]}`), nil
}

func IamEnsureRole(ctx context.Context, roleName, principalName string, preview bool) error {
	if !preview {
		rolePath := fmt.Sprintf("/%s/%s-path/", principalName, roleName)
		roles, err := IamListRoles(ctx, aws.String(rolePath))
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		switch len(roles) {
		case 0:
			policyDocument, err := iamAssumePolicyDocument(principalName)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			_, err = IamClient().CreateRoleWithContext(ctx, &iam.CreateRoleInput{
				Path:                     aws.String(rolePath),
				AssumeRolePolicyDocument: policyDocument,
				RoleName:                 aws.String(roleName),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			Logger.Println(PreviewString(preview)+"created role:", roleName, principalName)
		case 1:
			if *roles[0].Path != rolePath {
				err := fmt.Errorf("role path mismatch: %s %s != %s", roleName, *roles[0].Path, rolePath)
				Logger.Println("error:", err)
				return err
			}
			document, err := url.QueryUnescape(*roles[0].assumeRolePolicyDocumentString)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			policyDocument, err := iamAssumePolicyDocument(principalName)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			equal, err := iamPolicyEqual(document, *policyDocument)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			if !equal {
				err := fmt.Errorf("role policy mismatch: %s %s != %s", roleName, document, *policyDocument)
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
				Logger.Println("error:", *role.Arn)
			}
			return err
		}
	}
	return nil
}

func IamListInstanceProfiles(ctx context.Context, pathPrefix *string) ([]*iam.InstanceProfile, error) {
	var profiles []*iam.InstanceProfile
	var marker *string
	for {
		out, err := IamClient().ListInstanceProfilesWithContext(ctx, &iam.ListInstanceProfilesInput{
			Marker:     marker,
			PathPrefix: pathPrefix,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		profiles = append(profiles, out.InstanceProfiles...)
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return profiles, nil
}

func IamEnsureInstanceProfile(ctx context.Context, name string, policies, allows []string, preview bool) error {
	profilePath := fmt.Sprintf("/instance-profile/%s-path/", name)
	profiles, err := IamListInstanceProfiles(ctx, aws.String(profilePath))
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	switch len(profiles) {
	case 0:
		if !preview {
			out, err := IamClient().CreateInstanceProfileWithContext(ctx, &iam.CreateInstanceProfileInput{
				InstanceProfileName: aws.String(name),
				Path:                aws.String(profilePath),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			profiles = append(profiles, out.InstanceProfile)
		}
		Logger.Println(PreviewString(preview)+"created instance profile:", name)
	case 1:
		if *profiles[0].InstanceProfileName != name {
			err := fmt.Errorf("profile name mismatch: %s != %s", *profiles[0].InstanceProfileName, name)
			Logger.Println("error:", err)
			return err
		}
		if *profiles[0].Path != profilePath {
			err := fmt.Errorf("profile path mismatch: %s %s != %s", *profiles[0].InstanceProfileName, *profiles[0].Path, profilePath)
			Logger.Println("error:", err)
			return err
		}
	default:
		err := fmt.Errorf("found more than 1 instance profile under path: %s", profilePath)
		Logger.Println("error:", err)
		for _, profile := range profiles {
			Logger.Println("error:", *profile.Arn)
		}
		return err
	}
	var roleNames []string
	if len(profiles) == 1 {
		for _, role := range profiles[0].Roles {
			roleNames = append(roleNames, *role.RoleName)
		}
	}
	err = IamEnsureRole(ctx, name, "ec2", preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = IamEnsureRoleAllows(ctx, name, allows, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = IamEnsureRolePolicies(ctx, name, policies, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	switch len(roleNames) {
	case 0:
		if !preview {
			_, err := IamClient().AddRoleToInstanceProfileWithContext(ctx, &iam.AddRoleToInstanceProfileInput{
				InstanceProfileName: aws.String(name),
				RoleName:            aws.String(name),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"added role:", name, "to instance profile:", name)
	case 1:
		if roleNames[0] != name {
			err := fmt.Errorf("role name mismatch: %s != %s", roleNames[0], name)
			Logger.Println("error:", err)
			return err
		}
	default:
		err := fmt.Errorf("more than 1 role found for instance profile: %s %s", name, Pformat(roleNames))
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func IamRoleArn(ctx context.Context, principalName, roleName string) (string, error) {
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	return fmt.Sprintf("arn:aws:iam::%s:role/%s/%s-path/%s", account, principalName, roleName, roleName), nil
}

func IamInstanceProfileArn(ctx context.Context, profileName string) (string, error) {
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	return fmt.Sprintf("arn:aws:iam::%s:instance-profile/%s", account, profileName), nil
}

func IamListSSHPublicKeys(ctx context.Context) ([]*iam.SSHPublicKeyMetadata, error) {
	user, err := StsUser(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	var marker *string
	var keys []*iam.SSHPublicKeyMetadata
	for {
		out, err := IamClient().ListSSHPublicKeysWithContext(ctx, &iam.ListSSHPublicKeysInput{
			UserName: aws.String(user),
			Marker:   marker,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		keys = append(keys, out.SSHPublicKeys...)
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return keys, nil
}

func IamGetSSHPublicKey(ctx context.Context, keyID string) (*iam.SSHPublicKey, error) {
	user, err := StsUser(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	out, err := IamClient().GetSSHPublicKeyWithContext(ctx, &iam.GetSSHPublicKeyInput{
		Encoding:       aws.String("SSH"),
		SSHPublicKeyId: aws.String(keyID),
		UserName:       aws.String(user),
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return out.SSHPublicKey, nil
}

type IamRole struct {
	Arn                            *string
	AssumeRolePolicyDocument       *IamPolicyDocument
	assumeRolePolicyDocumentString *string
	CreateDate                     *time.Time
	Description                    *string
	MaxSessionDuration             *int64
	Path                           *string
	PermissionsBoundary            *iam.AttachedPermissionsBoundary
	RoleId                         *string
	RoleLastUsed                   *iam.RoleLastUsed
	RoleName                       *string
	Tags                           []*iam.Tag
}

func (r *IamRole) FromRole(role *iam.Role) error {
	r.assumeRolePolicyDocumentString = role.AssumeRolePolicyDocument
	r.AssumeRolePolicyDocument = &IamPolicyDocument{}
	document, err := url.QueryUnescape(*role.AssumeRolePolicyDocument)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = json.Unmarshal([]byte(document), r.AssumeRolePolicyDocument)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	r.Arn = role.Arn
	r.CreateDate = role.CreateDate
	r.Description = role.Description
	r.MaxSessionDuration = role.MaxSessionDuration
	r.Path = role.Path
	r.PermissionsBoundary = role.PermissionsBoundary
	r.RoleId = role.RoleId
	r.RoleLastUsed = role.RoleLastUsed
	r.RoleName = role.RoleName
	r.Tags = role.Tags
	return nil
}

type IamStatementEntry struct {
	Sid       string      `json:",omitempty"`
	Effect    string      `json:",omitempty"`
	Resource  interface{} `json:",omitempty"`
	Principal interface{} `json:",omitempty"`
	Action    interface{} `json:",omitempty"`
}

type IamPolicyDocument struct {
	Version   string              `json:",omitempty"`
	Id        string              `json:",omitempty"`
	Statement []IamStatementEntry `json:",omitempty"`
}

type IamStatementEntryCondition struct {
	Sid       string      `json:",omitempty"`
	Effect    string      `json:",omitempty"`
	Resource  interface{} `json:",omitempty"`
	Principal interface{} `json:",omitempty"`
	Action    interface{} `json:",omitempty"`
	Condition interface{} `json:",omitempty"`
}

type IamPolicyDocumentCondition struct {
	Version   string
	Id        string
	Statement []IamStatementEntryCondition
}

func IamPolicyArn(ctx context.Context, policyName string) (string, error) {
	policies, err := IamListPolicies(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	count := 0
	var policy *iam.Policy
	for _, p := range policies {
		if policyName == *p.PolicyName {
			policy = p
			count++
		}
	}
	switch count {
	case 0:
		err := fmt.Errorf("iam no policy found with name: %s", policyName)
		Logger.Println("error:", err)
		return "", err
	case 1:
		return *policy.Arn, nil
	default:
		err := fmt.Errorf("iam more than 1 (%d) policy found with name: %s", count, policyName)
		Logger.Println("error:", err)
		return "", err
	}
}

func IamListUserPolicies(ctx context.Context, username string) ([]*iam.Policy, error) {
	var marker *string
	var policies []*iam.Policy
	for {
		out, err := IamClient().ListAttachedUserPoliciesWithContext(ctx, &iam.ListAttachedUserPoliciesInput{
			Marker:   marker,
			UserName: aws.String(username),
		})
		if err != nil {
			return nil, err
		}
		for _, policy := range out.AttachedPolicies {
			policyArn, err := IamPolicyArn(ctx, *policy.PolicyName)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			out, err := IamClient().GetPolicyWithContext(ctx, &iam.GetPolicyInput{
				PolicyArn: aws.String(policyArn),
			})
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			policies = append(policies, out.Policy)
		}
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return policies, nil
}

func IamResetUserLoginTempPassword(ctx context.Context, username, password string) error {
	_, err := IamClient().UpdateLoginProfileWithContext(ctx, &iam.UpdateLoginProfileInput{
		Password:              aws.String(password),
		UserName:              aws.String(username),
		PasswordResetRequired: aws.Bool(true),
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func IamEnsureUserApi(ctx context.Context, username string, preview bool) (*iam.AccessKey, error) {
	_, err := IamClient().GetUserWithContext(ctx, &iam.GetUserInput{
		UserName: aws.String(username),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
			Logger.Println("error:", err)
			return nil, err
		}
		if !preview {
			_, err := IamClient().CreateUserWithContext(ctx, &iam.CreateUserInput{
				UserName: aws.String(username),
			})
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
		}
		Logger.Println(PreviewString(preview)+"iam created user:", username)
	}
	out, err := IamClient().ListAccessKeysWithContext(ctx, &iam.ListAccessKeysInput{
		MaxItems: aws.Int64(100),
		UserName: aws.String(username),
	})
	if err != nil && !preview {
		Logger.Println("error:", err)
		return nil, err
	}
	if !preview {
		switch len(out.AccessKeyMetadata) {
		case 0:
			out, err := IamClient().CreateAccessKeyWithContext(ctx, &iam.CreateAccessKeyInput{
				UserName: aws.String(username),
			})
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			Logger.Println(PreviewString(preview)+"created access key for username:", username)
			return out.AccessKey, nil
		case 1:
			return &iam.AccessKey{}, nil
		default:
			err := fmt.Errorf("more than 1 access key exists for username: %s %d", username, len(out.AccessKeyMetadata))
			return nil, err
		}
	}
	Logger.Println(PreviewString(preview)+"created access key for username:", username)
	return &iam.AccessKey{}, nil
}

func IamEnsureUserLogin(ctx context.Context, username, password string, preview bool) error {
	_, err := IamClient().GetUserWithContext(ctx, &iam.GetUserInput{
		UserName: aws.String(username),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
			Logger.Println("error:", err)
			return err
		}
		if !preview {
			_, err := IamClient().CreateUserWithContext(ctx, &iam.CreateUserInput{
				UserName: aws.String(username),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"iam created user:", username)
	}
	_, err = IamClient().GetLoginProfileWithContext(ctx, &iam.GetLoginProfileInput{
		UserName: aws.String(username),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
			Logger.Println("error:", err)
			return err
		}
		_, err = IamClient().CreateLoginProfileWithContext(ctx, &iam.CreateLoginProfileInput{
			Password:              aws.String(password),
			UserName:              aws.String(username),
			PasswordResetRequired: aws.Bool(true),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func IamListRolePolicies(ctx context.Context, roleName string) ([]*iam.AttachedPolicy, error) {
	var policies []*iam.AttachedPolicy
	var marker *string
	for {
		out, err := IamClient().ListAttachedRolePoliciesWithContext(ctx, &iam.ListAttachedRolePoliciesInput{
			RoleName: aws.String(roleName),
			Marker:   marker,
		})
		if err != nil {
			return nil, err
		}
		policies = append(policies, out.AttachedPolicies...)
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return policies, nil
}

func IamListUserAllows(ctx context.Context, username string) ([]*IamAllow, error) {
	var iamAllows []*IamAllow
	var marker *string
	for {
		out, err := IamClient().ListUserPoliciesWithContext(ctx, &iam.ListUserPoliciesInput{
			UserName: aws.String(username),
			Marker:   marker,
		})
		if err != nil {
			return nil, err
		}
		for _, policyName := range out.PolicyNames {
			policy, err := IamClient().GetUserPolicyWithContext(ctx, &iam.GetUserPolicyInput{
				UserName:   aws.String(username),
				PolicyName: policyName,
			})
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			policyDocument, err := url.QueryUnescape(*policy.PolicyDocument)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			allow, err := iamAllowFromPolicyDocument(policyDocument)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			iamAllows = append(iamAllows, allow)
		}
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return iamAllows, nil
}

func IamListRoleAllows(ctx context.Context, roleName string) ([]*IamAllow, error) {
	var iamAllows []*IamAllow
	var marker *string
	for {
		out, err := IamClient().ListRolePoliciesWithContext(ctx, &iam.ListRolePoliciesInput{
			RoleName: aws.String(roleName),
			Marker:   marker,
		})
		if err != nil {
			return nil, err
		}
		for _, policyName := range out.PolicyNames {
			policy, err := IamClient().GetRolePolicyWithContext(ctx, &iam.GetRolePolicyInput{
				RoleName:   aws.String(roleName),
				PolicyName: policyName,
			})
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			policyDocument, err := url.QueryUnescape(*policy.PolicyDocument)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			allow, err := iamAllowFromPolicyDocument(policyDocument)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			iamAllows = append(iamAllows, allow)
		}
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return iamAllows, nil
}

type IamUser struct {
	Arn                 *string                          `json:",omitempty"`
	CreateDate          *time.Time                       `json:",omitempty"`
	PasswordLastUsed    *time.Time                       `json:",omitempty"`
	Path                *string                          `json:",omitempty"`
	PermissionsBoundary *iam.AttachedPermissionsBoundary `json:",omitempty"`
	Tags                []*iam.Tag                       `json:",omitempty"`
	UserId              *string                          `json:",omitempty"`
	UserName            *string                          `json:",omitempty"`
	Allows              []*IamAllow                      `json:",omitempty"`
	Policies            []string                         `json:",omitempty"`
}

func (u *IamUser) FromUser(ctx context.Context, user *iam.User) error {
	u.Arn = user.Arn
	u.CreateDate = user.CreateDate
	u.PasswordLastUsed = user.PasswordLastUsed
	u.Path = user.Path
	u.PermissionsBoundary = user.PermissionsBoundary
	u.Tags = user.Tags
	u.UserId = user.UserId
	u.UserName = user.UserName
	var err error
	u.Allows, err = IamListUserAllows(ctx, *user.UserName)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	policies, err := IamListUserPolicies(ctx, *user.UserName)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	for _, policy := range policies {
		u.Policies = append(u.Policies, *policy.PolicyName)
	}
	return nil
}

func IamListUsers(ctx context.Context) ([]*IamUser, error) {
	var marker *string
	var result []*IamUser
	for {
		out, err := IamClient().ListUsersWithContext(ctx, &iam.ListUsersInput{
			Marker: marker,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, user := range out.Users {
			iamUser := &IamUser{}
			err := iamUser.FromUser(ctx, user)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			result = append(result, iamUser)
		}
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return result, nil
}

func IamDeleteRolePolicies(ctx context.Context, name string, preview bool) error {
	rolePolicies, err := IamListRolePolicies(ctx, name)
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
			Logger.Println("error:", err)
			return err
		}
		return nil
	}
	for _, policy := range rolePolicies {
		if !preview {
			_, err := IamClient().DetachRolePolicyWithContext(ctx, &iam.DetachRolePolicyInput{
				RoleName:  aws.String(name),
				PolicyArn: policy.PolicyArn,
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"deleted role policy:", name, *policy.PolicyName)
	}
	return nil
}

func IamDeleteRoleAllows(ctx context.Context, name string, preview bool) error {
	roleAllows, err := IamListRoleAllows(ctx, name)
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
			Logger.Println("error:", err)
			return err
		}
		return nil
	}
	for _, allow := range roleAllows {
		if !preview {
			_, err := IamClient().DeleteRolePolicyWithContext(ctx, &iam.DeleteRolePolicyInput{
				RoleName:   aws.String(name),
				PolicyName: aws.String(allow.policyName()),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"deleted role allow:", name, allow.policyName())
	}
	return nil
}

func IamEnsureEC2SpotRoles(ctx context.Context) error {
	roleName := "aws-ec2-spot-fleet-tagging-role"
	doc := IamPolicyDocument{
		Version: "2012-10-17",
		Id:      roleName,
		Statement: []IamStatementEntry{{
			Effect:    "Allow",
			Principal: map[string]string{"Service": "spotfleet.amazonaws.com"},
			Action:    "sts:AssumeRole",
		}},
	}
	bytes, err := json.Marshal(doc)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	_, err = IamClient().CreateRoleWithContext(ctx, &iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		AssumeRolePolicyDocument: aws.String(string(bytes)),
	})
	policyArn := "arn:aws:iam::aws:policy/service-role/AmazonEC2SpotFleetTaggingRole"
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != iam.ErrCodeEntityAlreadyExistsException {
			Logger.Println("error:", err)
			return err
		}
		out, err := IamClient().ListAttachedRolePoliciesWithContext(ctx, &iam.ListAttachedRolePoliciesInput{
			RoleName: aws.String(roleName),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		if len(out.AttachedPolicies) != 1 {
			err := fmt.Errorf("%s is misconfigured: %s", roleName, Pformat(out.AttachedPolicies))
			Logger.Println("error:", err)
			return err
		}
		if *out.AttachedPolicies[0].PolicyArn != policyArn {
			err := fmt.Errorf("%s is misconfigured, %s != %s", roleName, *out.AttachedPolicies[0].PolicyArn, policyArn)
			Logger.Println("error:", err)
			return err
		}
	} else {
		_, err = IamClient().AttachRolePolicyWithContext(ctx, &iam.AttachRolePolicyInput{
			RoleName:  aws.String(roleName),
			PolicyArn: aws.String(policyArn),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	_, err = IamClient().CreateServiceLinkedRoleWithContext(ctx, &iam.CreateServiceLinkedRoleInput{
		AWSServiceName: aws.String("spot.amazonaws.com"),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "InvalidInput" { // already exists
			Logger.Println("error:", err)
			return err
		}
	}
	_, err = IamClient().CreateServiceLinkedRoleWithContext(ctx, &iam.CreateServiceLinkedRoleInput{
		AWSServiceName: aws.String("spotfleet.amazonaws.com"),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "InvalidInput" { // already exists
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}
