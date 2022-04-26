package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"

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

type iamAllow struct {
	action   string
	resource string
}

func (a *iamAllow) String() string {
	return fmt.Sprintf("%s %s", a.action, a.resource)
}

func (a *iamAllow) policyDocument() string {
	return `{"Version": "2012-10-17",
             "Statement": [{"Effect": "Allow",
                            "Action": "` + a.action + `",
                            "Resource": "` + a.resource + `"}]}`
}

func (allow *iamAllow) policyName() string {
	action := strings.ReplaceAll(allow.action, "*", "ALL")
	resource := strings.ReplaceAll(allow.resource, "*", "ALL")
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

func iamAllowFromPolicyDocument(policyDocument string) (*iamAllow, error) {
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
	allow := &iamAllow{
		action:   policy.Statement[0].Action.(string),
		resource: policy.Statement[0].Resource.(string),
	}
	return allow, nil
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

func IamListRoles(ctx context.Context, pathPrefix string) ([]*iam.Role, error) {
	var roles []*iam.Role
	var marker *string
	input := &iam.ListRolesInput{}
	if pathPrefix != "" {
		input.PathPrefix = aws.String(pathPrefix)
	}
	for {
		input.Marker = marker
		out, err := IamClient().ListRolesWithContext(ctx, input)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		roles = append(roles, out.Roles...)
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return roles, nil
}

func IamEnsureRoleAllows(ctx context.Context, roleName string, allows []string, preview bool) error {
	var allowNames []string
	for _, allowStr := range allows {
		parts := strings.SplitN(allowStr, " ", 2)
		if len(parts) != 2 {
			err := fmt.Errorf("allow format should be: 'SERVICE:ACTION RESOURCE', got: %s", allowStr)
			Logger.Println("error:", err)
			return err
		}
		allow := &iamAllow{
			action:   parts[0],
			resource: parts[1],
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
		Logger.Println(PreviewString(preview)+"attach role allow:", roleName, allow)
	}
	attachedAllows, err := IamListRoleAllows(ctx, roleName)
	if err != nil && !preview {
		Logger.Println("error:", err)
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
		Logger.Println("error:", err)
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

func iamAssumePolicyDocument(principalName string) *string {
	return aws.String(`{"Version": "2012-10-17",
                        "Statement": [{"Effect": "Allow",
                                       "Principal": {"Service": "` + principalName + `.amazonaws.com"},
                                       "Action": "sts:AssumeRole"}]}`)
}

func IamEnsureRole(ctx context.Context, roleName, principalName string, preview bool) error {
	if !preview {
		rolePath := fmt.Sprintf("/%s/%s-path/", principalName, roleName)
		roles, err := IamListRoles(ctx, rolePath)
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
			Logger.Println(PreviewString(preview)+"created role:", roleName, principalName)
		case 1:
			if *roles[0].Path != rolePath {
				err := fmt.Errorf("role path mismatch: %s %s != %s", roleName, *roles[0].Path, rolePath)
				Logger.Println("error:", err)
				return err
			}
			document, err := url.QueryUnescape(*roles[0].AssumeRolePolicyDocument)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			equal, err := iamPolicyEqual(document, *iamAssumePolicyDocument(principalName))
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			if !equal {
				err := fmt.Errorf("role policy mismatch: %s %s != %s", roleName, document, *iamAssumePolicyDocument(principalName))
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

func IamEnsureInstanceProfileRole(ctx context.Context, profileName, roleName string, preview bool) error {
	profilePath := fmt.Sprintf("/instance-profile/%s-path/", profileName)
	profiles, err := IamListInstanceProfiles(ctx, aws.String(profilePath))
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	switch len(profiles) {
	case 0:
		if !preview {
			out, err := IamClient().CreateInstanceProfileWithContext(ctx, &iam.CreateInstanceProfileInput{
				InstanceProfileName: aws.String(profileName),
				Path:                aws.String(profilePath),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			profiles = append(profiles, out.InstanceProfile)
		}
		Logger.Println(PreviewString(preview)+"created instance profile:", profileName)
	case 1:
		if *profiles[0].InstanceProfileName != profileName {
			err := fmt.Errorf("profile name mismatch: %s != %s", *profiles[0].InstanceProfileName, profileName)
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
	var roles []string
	for _, role := range profiles[0].Roles {
		roles = append(roles, *role.RoleName)
	}
	if !Contains(roles, roleName) {
		if !preview {
			_, err := IamClient().AddRoleToInstanceProfileWithContext(ctx, &iam.AddRoleToInstanceProfileInput{
				InstanceProfileName: aws.String(profileName),
				RoleName:            aws.String(roleName),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}

		}
		Logger.Println(PreviewString(preview)+"added role:", roleName, "to instance profile:", profileName)
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

type IamStatementEntry struct {
	Sid       string
	Effect    string
	Resource  interface{} `json:",omitempty"`
	Principal interface{}
	Action    interface{}
}

type IamPolicyDocument struct {
	Version   string
	Id        string
	Statement []IamStatementEntry
}

type IamStatementEntryCondition struct {
	Sid       string
	Effect    string
	Resource  interface{}
	Principal interface{}
	Action    interface{}
	Condition interface{}
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

func IamListUserPolicies(ctx context.Context, username string) ([]string, error) {
	var marker *string
	var policies []string
	for {
		out, err := IamClient().ListUserPoliciesWithContext(ctx, &iam.ListUserPoliciesInput{
			Marker:   marker,
			UserName: aws.String(username),
		})
		if err != nil {
			return nil, err
		}
		policies = append(policies, StringSlice(out.PolicyNames)...)
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

func IamEnsureUser(ctx context.Context, username, password, policyName string, preview bool) error {
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

	policies, err := IamListUserPolicies(ctx, username)
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
			Logger.Println("error:", err)
			return err
		}
		if preview {
			return nil
		}
		Logger.Println("error:", err)
		return err
	}
	if !Contains(policies, policyName) {
		policyArn, err := IamPolicyArn(ctx, policyName)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		_, err = IamClient().AttachUserPolicyWithContext(ctx, &iam.AttachUserPolicyInput{
			PolicyArn: aws.String(policyArn),
			UserName:  aws.String(username),
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

func IamListRoleAllows(ctx context.Context, roleName string) ([]*iamAllow, error) {
	var iamAllows []*iamAllow
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

func IamListUsers(ctx context.Context) ([]*iam.User, error) {
	var marker *string
	var result []*iam.User
	for {
		out, err := IamClient().ListUsersWithContext(ctx, &iam.ListUsersInput{
			Marker: marker,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		result = append(result, out.Users...)
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

func IamDeleteRole(ctx context.Context, name string, preview bool) error {
	if !preview {
		_, err := IamClient().DeleteRoleWithContext(ctx, &iam.DeleteRoleInput{
			RoleName: aws.String(name),
		})
		if err != nil {
			aerr, ok := err.(awserr.Error)
			if !ok || aerr.Code() != iam.ErrCodeNoSuchEntityException {
				Logger.Println("error:", err)
				return err
			}
		}
	}
	Logger.Println(PreviewString(preview)+"deleted role:", name)
	return nil
}

func IamEnsureEC2Roles(ctx context.Context) error {
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
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != iam.ErrCodeEntityAlreadyExistsException {
			Logger.Println("error:", err)
			return err
		}
	}
	_, err = IamClient().AttachRolePolicyWithContext(ctx, &iam.AttachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String("arn:aws:iam::aws:policy/service-role/AmazonEC2SpotFleetTaggingRole"),
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
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
