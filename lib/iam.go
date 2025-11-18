package lib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

const (
	EC2SpotFleetTaggingRole = "aws-ec2-spot-fleet-tagging-role"
)

var iamClient *iam.Client
var iamClientLock sync.Mutex

func IamClientExplicit(accessKeyID, accessKeySecret, region string) *iam.Client {
	return iam.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func IamClient() *iam.Client {
	iamClientLock.Lock()
	defer iamClientLock.Unlock()
	if iamClient == nil {
		iamClient = iam.NewFromConfig(*Session())
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
		if !slices.Contains([]string{"arn", "aws", "s3", "dynamodb", "sqs", "sns"}, part) {
			parts = append(parts, strings.ReplaceAll(part, "/", "__"))
		}
	}
	resource = strings.Join(parts, ":")
	name := action + "__" + resource
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.TrimRight(name, "_")
	return name
}

func iamAllowsFromPolicyDocument(policyDocument string) ([]*IamAllow, error) {
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
	var allows []*IamAllow
	resource, ok := policy.Statement[0].Resource.(string)
	if !ok {
		resources, ok := policy.Statement[0].Resource.([]any)
		if len(resources) != 1 || !ok {
			panic(fmt.Sprintf("%#v", policy.Statement[0]))
		}
		resource = resources[0].(string)
	}
	action, ok := policy.Statement[0].Action.(string)
	if ok {
		allows = append(allows, &IamAllow{
			Action:   action,
			Resource: resource,
		})
	} else {
		actions, ok := policy.Statement[0].Action.([]any)
		if !ok {
			panic(fmt.Sprintf("%#v", policy.Statement[0]))
		}
		for _, action := range actions {
			allows = append(allows, &IamAllow{
				Action:   action.(string),
				Resource: resource,
			})
		}
	}
	return allows, nil
}

type IamPolicy struct {
	Arn                           *string            `json:",omitempty"`
	AttachmentCount               *int32             `json:",omitempty"`
	CreateDate                    *time.Time         `json:",omitempty"`
	DefaultVersionId              *string            `json:",omitempty"`
	Description                   *string            `json:",omitempty"`
	IsAttachable                  *bool              `json:",omitempty"`
	Path                          *string            `json:",omitempty"`
	PermissionsBoundaryUsageCount *int32             `json:",omitempty"`
	PolicyId                      *string            `json:",omitempty"`
	PolicyName                    *string            `json:",omitempty"`
	Tags                          []iamtypes.Tag     `json:",omitempty"`
	UpdateDate                    *time.Time         `json:",omitempty"`
	PolicyDocument                *IamPolicyDocument `json:",omitempty"`
}

func (p *IamPolicy) FromPolicy(ctx context.Context, policy iamtypes.Policy, resolveDocument bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamPolicy.FromPolicy"}
		d.Start()
		defer d.End()
	}
	p.Arn = policy.Arn
	p.AttachmentCount = policy.AttachmentCount
	p.CreateDate = policy.CreateDate
	p.DefaultVersionId = policy.DefaultVersionId
	p.Description = policy.Description
	p.IsAttachable = &policy.IsAttachable
	p.Path = policy.Path
	p.PermissionsBoundaryUsageCount = policy.PermissionsBoundaryUsageCount
	p.PolicyId = policy.PolicyId
	p.PolicyName = policy.PolicyName
	p.Tags = policy.Tags
	p.UpdateDate = policy.UpdateDate
	if resolveDocument {
		p.PolicyDocument = &IamPolicyDocument{}
		out, err := IamClient().GetPolicyVersion(ctx, &iam.GetPolicyVersionInput{
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamDeleteUser"}
		d.Start()
		defer d.End()
	}
	_, err := IamClient().GetUser(ctx, &iam.GetUserInput{
		UserName: aws.String(name),
	})
	if err != nil {
		var nse *iamtypes.NoSuchEntityException
		if errors.As(err, &nse) {
			return nil
		}
		Logger.Println("error:", err)
		return err
	}
	if !preview {
		out, err := IamClient().ListAccessKeys(ctx, &iam.ListAccessKeysInput{
			UserName: aws.String(name),
			MaxItems: aws.Int32(100),
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
			_, err := IamClient().DeleteAccessKey(ctx, &iam.DeleteAccessKeyInput{
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
		_, err = IamClient().DeleteUser(ctx, &iam.DeleteUserInput{
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

func IamDeleteRole(ctx context.Context, roleName string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamDeleteRole"}
		d.Start()
		defer d.End()
	}
	_, err := IamClient().GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		var nse *iamtypes.NoSuchEntityException
		if errors.As(err, &nse) {
			return nil
		}
		Logger.Println("error:", err)
		return err
	}
	err = IamEnsureRoleAllows(ctx, roleName, []string{}, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = IamEnsureRolePolicies(ctx, roleName, []string{}, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if !preview {
		_, err = IamClient().DeleteRole(ctx, &iam.DeleteRoleInput{
			RoleName: aws.String(roleName),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"deleted role:", roleName)
	return nil
}

func IamDeleteInstanceProfile(ctx context.Context, profileName string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamDeleteInstanceProfile"}
		d.Start()
		defer d.End()
	}
	_, err := IamClient().GetInstanceProfile(ctx, &iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(profileName),
	})
	if err != nil {
		var nse *iamtypes.NoSuchEntityException
		if errors.As(err, &nse) {
			return nil
		}
		Logger.Println("error:", err)
		return err
	}
	err = IamEnsureRoleAllows(ctx, profileName, []string{}, preview)
	if err != nil {
		var nse *iamtypes.NoSuchEntityException
		if errors.As(err, &nse) {
		} else {
			Logger.Println("error:", err)
			return err
		}
	}
	err = IamEnsureRolePolicies(ctx, profileName, []string{}, preview)
	if err != nil {
		var nse *iamtypes.NoSuchEntityException
		if errors.As(err, &nse) {
		} else {
			Logger.Println("error:", err)
			return err
		}
	}
	if !preview {
		_, err = IamClient().RemoveRoleFromInstanceProfile(ctx, &iam.RemoveRoleFromInstanceProfileInput{
			InstanceProfileName: aws.String(profileName),
			RoleName:            aws.String(profileName),
		})
		if err != nil {
			var nse *iamtypes.NoSuchEntityException
			if errors.As(err, &nse) {
			} else {
				Logger.Println("error:", err)
				return err
			}
		}
		_, err = IamClient().DeleteRole(ctx, &iam.DeleteRoleInput{
			RoleName: aws.String(profileName),
		})
		if err != nil {
			var nse *iamtypes.NoSuchEntityException
			if errors.As(err, &nse) {
			} else {
				Logger.Println("error:", err)
				return err
			}
		}
		_, err = IamClient().DeleteInstanceProfile(ctx, &iam.DeleteInstanceProfileInput{
			InstanceProfileName: aws.String(profileName),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"deleted instance profile:", profileName)
	return nil
}

func IamListPolicies(ctx context.Context) ([]iamtypes.Policy, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamListPolicies"}
		d.Start()
		defer d.End()
	}
	var policies []iamtypes.Policy
	var marker *string
	for {
		out, err := IamClient().ListPolicies(ctx, &iam.ListPoliciesInput{
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamListRoles"}
		d.Start()
		defer d.End()
	}
	var roles []*IamRole
	var marker *string
	input := &iam.ListRolesInput{}
	if pathPrefix != nil {
		input.PathPrefix = pathPrefix
	}
	for {
		input.Marker = marker
		out, err := IamClient().ListRoles(ctx, input)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, role := range out.Roles {
			r := &IamRole{}
			err := r.FromRole(ctx, &role)
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamEnsureUserAllows"}
		d.Start()
		defer d.End()
	}
	var allowNames []string
	for _, allowStr := range allows {
		parts := SplitWhiteSpaceN(allowStr, 2)
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
		out, err := IamClient().GetUserPolicy(ctx, &iam.GetUserPolicyInput{
			UserName:   aws.String(username),
			PolicyName: aws.String(allow.policyName()),
		})
		if err != nil {
			var nse *iamtypes.NoSuchEntityException
			if errors.As(err, &nse) {
			} else {
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
			_, err := IamClient().PutUserPolicy(ctx, &iam.PutUserPolicyInput{
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
		if !slices.Contains(allowNames, allow.policyName()) {
			if !preview {
				_, err := IamClient().DeleteUserPolicy(ctx, &iam.DeleteUserPolicyInput{
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

func iamFetchPolicyDocument(ctx context.Context, arn string, versionID string) (string, error) {
	out, err := IamClient().GetPolicyVersion(ctx, &iam.GetPolicyVersionInput{
		PolicyArn: aws.String(arn),
		VersionId: aws.String(versionID),
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	document, err := url.QueryUnescape(*out.PolicyVersion.Document)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	return document, nil
}

func IamEnsureManagedPolicy(ctx context.Context, policyName, description, policyDocument string, preview bool) (*iamtypes.Policy, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamEnsureManagedPolicy"}
		d.Start()
		defer d.End()
	}
	trimmedDoc := strings.TrimSpace(policyDocument)
	policies, err := IamListPolicies(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	var matches []iamtypes.Policy
	for _, policy := range policies {
		if policy.PolicyName != nil && *policy.PolicyName == policyName {
			matches = append(matches, policy)
		}
	}
	switch len(matches) {
	case 0:
		if !preview {
			out, err := IamClient().CreatePolicy(ctx, &iam.CreatePolicyInput{
				PolicyName:     aws.String(policyName),
				Description:    aws.String(description),
				PolicyDocument: aws.String(trimmedDoc),
			})
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			Logger.Println(PreviewString(preview)+"created managed policy:", policyName)
			return out.Policy, nil
		}
		Logger.Println(PreviewString(preview)+"create managed policy:", policyName)
		return nil, nil
	case 1:
		policy := matches[0]
		changed := false
		if policy.Description == nil || *policy.Description != description {
			Logger.Println("warning: managed policy description mismatch:", policyName, "have=",
				aws.ToString(policy.Description), "want=", description)
		}
		document, err := iamFetchPolicyDocument(ctx, *policy.Arn, *policy.DefaultVersionId)
		if err != nil {
			return nil, err
		}
		equal, err := iamPolicyEqual(strings.TrimSpace(document), trimmedDoc)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		if !equal {
			if !preview {
				// Before creating a new version, ensure we are under the 5 version limit
				versionsOut, err := IamClient().ListPolicyVersions(ctx, &iam.ListPolicyVersionsInput{
					PolicyArn: policy.Arn,
				})
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
				// Build a list of non-default versions, oldest first
				var toDelete []iamtypes.PolicyVersion
				for _, version := range versionsOut.Versions {
					if !version.IsDefaultVersion {
						toDelete = append(toDelete, version)
					}
				}
				sort.Slice(toDelete, func(i, j int) bool {
					return toDelete[i].CreateDate.Before(*toDelete[j].CreateDate)
				})
				// Delete oldest non-default versions until we are below the limit
				for len(versionsOut.Versions) >= 5 && len(toDelete) > 0 {
					old := toDelete[0]
					toDelete = toDelete[1:]
					_, err := IamClient().DeletePolicyVersion(ctx, &iam.DeletePolicyVersionInput{
						PolicyArn: policy.Arn,
						VersionId: old.VersionId,
					})
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
					Logger.Println("deleted old policy version:", policyName, aws.ToString(old.VersionId))
					// Reflect the deletion locally to avoid an extra List call
					versionsOut.Versions = versionsOut.Versions[:len(versionsOut.Versions)-1]
				}
				// Create a new default policy version with the desired document
				input := iam.CreatePolicyVersionInput{
					PolicyArn:      policy.Arn,
					PolicyDocument: aws.String(trimmedDoc),
					SetAsDefault:   true,
				}
				_, err = IamClient().CreatePolicyVersion(ctx, &input)
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
				Logger.Println(PreviewString(preview)+"updated policy document:", policyName)
			}
			changed = true
		}
		if changed {
			if !preview {
				// Refresh policy to reflect new DefaultVersionId after update
				getOut, err := IamClient().GetPolicy(ctx, &iam.GetPolicyInput{PolicyArn: policy.Arn})
				if err != nil {
					Logger.Println("error:", err)
				} else if getOut.Policy != nil {
					policy = *getOut.Policy
				}
			}
			return &policy, nil
		}
		Logger.Println(PreviewString(preview)+"managed policy already up to date:", policyName)
		return &policy, nil
	default:
		err := fmt.Errorf("found multiple policies with name: %s", policyName)
		for _, p := range matches {
			Logger.Println("error:", policyName, aws.ToString(p.Arn))
		}
		return nil, err
	}
}

func IamEnsureRoleAllows(ctx context.Context, roleName string, allows []string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamEnsureRoleAllows"}
		d.Start()
		defer d.End()
	}
	var allowNames []string
	for _, allowStr := range allows {
		if preview {
			for _, name := range []string{lambdaEnvVarApiID, lambdaEnvVarWebsocketID} {
				if os.Getenv(name) == "" {
					err := os.Setenv(name, name)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
			}
		}
		allowStr, err := resolveEnvVars(allowStr, []string{}) // resolve again since lambdaEnvVarApiID and lambdaEnvVarWebsocketID are not set
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		parts := SplitWhiteSpaceN(allowStr, 2)
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
		out, err := IamClient().GetRolePolicy(ctx, &iam.GetRolePolicyInput{
			RoleName:   aws.String(roleName),
			PolicyName: aws.String(allow.policyName()),
		})
		if err != nil {
			var nse *iamtypes.NoSuchEntityException
			if errors.As(err, &nse) {
			} else {
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
			_, err := IamClient().PutRolePolicy(ctx, &iam.PutRolePolicyInput{
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
		if !slices.Contains(allowNames, allow.policyName()) {
			if !preview {
				_, err := IamClient().DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
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
	aData := map[string]any{}
	bData := map[string]any{}
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamEnsureUserPolicies"}
		d.Start()
		defer d.End()
	}
	// Fetch all policies once and index by name for O(1) lookups
	policies, err := IamListPolicies(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	byName := map[string][]iamtypes.Policy{}
	for _, p := range policies {
		name := Last(strings.Split(*p.Arn, "/"))
		byName[name] = append(byName[name], p)
	}
	// Fetch currently attached user policies once and build a set
	attachedPolicies, err := IamListUserPolicies(ctx, username)
	if err != nil && !preview {
		Logger.Println("error:", err)
		return err
	}
	attached := map[string]struct{}{}
	for _, ap := range attachedPolicies {
		if ap.PolicyName != nil {
			attached[*ap.PolicyName] = struct{}{}
		}
	}
	// Ensure desired policies are attached
	for _, policyName := range policyNames {
		matches := byName[policyName]
		switch len(matches) {
		case 0:
			if !preview {
				err := fmt.Errorf("didn't find policy for name: %s", policyName)
				Logger.Println("error:", err)
				return err
			}
		case 1:
			if _, ok := attached[policyName]; ok {
				continue
			}
			if !preview {
				_, err = IamClient().AttachUserPolicy(ctx, &iam.AttachUserPolicyInput{
					PolicyArn: matches[0].Arn,
					UserName:  aws.String(username),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				// reflect the new attachment locally
				attached[policyName] = struct{}{}
			}
			Logger.Println(PreviewString(preview)+"attached user policy:", username, policyName)
		default:
			err := fmt.Errorf("found more than 1 policy for name: %s", policyName)
			Logger.Println("error:", err)
			for _, p := range matches {
				Logger.Println("error:", *p.Arn)
			}
			return err
		}
	}
	// Detach any extra policies not requested
	attachedPolicies, err = IamListUserPolicies(ctx, username)
	if err != nil && !preview {
		Logger.Println("error:", err)
		return err
	}
	for _, policy := range attachedPolicies {
		if !slices.Contains(policyNames, *policy.PolicyName) {
			if !preview {
				_, err := IamClient().DetachUserPolicy(ctx, &iam.DetachUserPolicyInput{
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamEnsureRolePolicies"}
		d.Start()
		defer d.End()
	}
	policies, err := IamListPolicies(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
outer:
	for _, policyName := range policyNames {
		var matchedPolicies []iamtypes.Policy
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
				_, err = IamClient().AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
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
		if !slices.Contains(policyNames, policyName) {
			if !preview {
				_, err := IamClient().DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
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

func IamEnsureRole(ctx context.Context, infrasetName, roleName, principalName string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamEnsureRole"}
		d.Start()
		defer d.End()
	}
	rolePath := fmt.Sprintf("/%s/%s-path/", principalName, roleName)
	roles, err := IamListRoles(ctx, aws.String(rolePath))
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	switch len(roles) {
	case 0:
		if !preview {
			policyDocument, err := iamAssumePolicyDocument(principalName)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			_, err = IamClient().CreateRole(ctx, &iam.CreateRoleInput{
				Path:                     aws.String(rolePath),
				AssumeRolePolicyDocument: policyDocument,
				RoleName:                 aws.String(roleName),
				Tags: []iamtypes.Tag{{
					Key:   aws.String(infraSetTagName),
					Value: aws.String(infrasetName),
				}},
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created role:", roleName, principalName)
	case 1:
		if *roles[0].path != rolePath {
			err := fmt.Errorf("role path mismatch: %s %s != %s", roleName, *roles[0].path, rolePath)
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
			Logger.Println("error:", *role.arn)
		}
		return err
	}
	return nil
}

type IamInstanceProfile struct {
	arn        *string
	createDate *time.Time
	id         *string
	path       *string
	tags       []iamtypes.Tag

	Name  *string    `json:",omitempty" yaml:",omitempty"`
	Roles []*IamRole `json:",omitempty" yaml:",omitempty"`
}

func (p *IamInstanceProfile) FromProfile(ctx context.Context, profile *iamtypes.InstanceProfile) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamInstanceProfile.FromProfile"}
		d.Start()
		defer d.End()
	}
	p.arn = profile.Arn
	p.createDate = profile.CreateDate
	p.id = profile.InstanceProfileId
	p.Name = profile.InstanceProfileName
	p.path = profile.Path
	p.tags = profile.Tags
	for _, role := range profile.Roles {
		r := &IamRole{}
		err := r.FromRole(ctx, &role)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		p.Roles = append(p.Roles, r)
	}
	return nil
}

func IamListInstanceProfiles(ctx context.Context, pathPrefix *string) ([]*IamInstanceProfile, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamListInstanceProfiles"}
		d.Start()
		defer d.End()
	}
	var profiles []*IamInstanceProfile
	var marker *string
	for {
		out, err := IamClient().ListInstanceProfiles(ctx, &iam.ListInstanceProfilesInput{
			Marker:     marker,
			PathPrefix: pathPrefix,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, profile := range out.InstanceProfiles {
			out, err := IamClient().ListInstanceProfileTags(ctx, &iam.ListInstanceProfileTagsInput{
				InstanceProfileName: profile.InstanceProfileName,
				MaxItems:            aws.Int32(100),
			})
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			if len(out.Tags) == 100 {
				panic("out overflow")
			}
			profile.Tags = out.Tags
			p := &IamInstanceProfile{}
			err = p.FromProfile(ctx, &profile)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			profiles = append(profiles, p)
		}
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return profiles, nil
}

func IamEnsureInstanceProfile(ctx context.Context, infrasetName, profileName string, policies, allows []string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamEnsureInstanceProfile"}
		d.Start()
		defer d.End()
	}
	profilePath := fmt.Sprintf("/instance-profile/%s-path/", profileName)
	profiles, err := IamListInstanceProfiles(ctx, aws.String(profilePath))
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	switch len(profiles) {
	case 0:
		if !preview {
			out, err := IamClient().CreateInstanceProfile(ctx, &iam.CreateInstanceProfileInput{
				InstanceProfileName: aws.String(profileName),
				Path:                aws.String(profilePath),
				Tags: []iamtypes.Tag{{
					Key:   aws.String(infraSetTagName),
					Value: aws.String(infrasetName),
				}},
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			p := &IamInstanceProfile{}
			err = p.FromProfile(ctx, out.InstanceProfile)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			profiles = append(profiles, p)
		}
		Logger.Println(PreviewString(preview)+"created instance profile:", profileName)
	case 1:
		if *profiles[0].Name != profileName {
			err := fmt.Errorf("profile name mismatch: %s != %s", *profiles[0].Name, profileName)
			Logger.Println("error:", err)
			return err
		}
		if *profiles[0].path != profilePath {
			err := fmt.Errorf("profile path mismatch: %s %s != %s", *profiles[0].Name, *profiles[0].path, profilePath)
			Logger.Println("error:", err)
			return err
		}
	default:
		err := fmt.Errorf("found more than 1 instance profile under path: %s", profilePath)
		Logger.Println("error:", err)
		for _, profile := range profiles {
			Logger.Println("error:", *profile.arn)
		}
		return err
	}
	err = IamEnsureRole(ctx, infrasetName, profileName, "ec2", preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = IamEnsureRoleAllows(ctx, profileName, allows, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = IamEnsureRolePolicies(ctx, profileName, policies, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	var roleNames []string
	if len(profiles) == 1 {
		for _, role := range profiles[0].Roles {
			roleNames = append(roleNames, *role.RoleName)
		}
	}
	switch len(roleNames) {
	case 0:
		if !preview {
			_, err := IamClient().AddRoleToInstanceProfile(ctx, &iam.AddRoleToInstanceProfileInput{
				InstanceProfileName: aws.String(profileName),
				RoleName:            aws.String(profileName),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"added role:", profileName, "to instance profile:", profileName)
	case 1:
		if roleNames[0] != profileName {
			err := fmt.Errorf("role name mismatch: %s != %s", roleNames[0], profileName)
			Logger.Println("error:", err)
			return err
		}
	default:
		err := fmt.Errorf("more than 1 role found for instance profile: %s %s", profileName, Pformat(roleNames))
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

func IamListSSHPublicKeys(ctx context.Context) ([]iamtypes.SSHPublicKeyMetadata, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamListSSHPublicKeys"}
		d.Start()
		defer d.End()
	}
	user, err := StsUser(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	var marker *string
	var keys []iamtypes.SSHPublicKeyMetadata
	for {
		out, err := IamClient().ListSSHPublicKeys(ctx, &iam.ListSSHPublicKeysInput{
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

func IamGetSSHPublicKey(ctx context.Context, keyID string) (*iamtypes.SSHPublicKey, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamGetSSHPublicKey"}
		d.Start()
		defer d.End()
	}
	user, err := StsUser(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	out, err := IamClient().GetSSHPublicKey(ctx, &iam.GetSSHPublicKeyInput{
		Encoding:       iamtypes.EncodingTypeSsh,
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
	assumeRolePolicyDocumentString *string
	description                    *string
	maxSessionDuration             *int32
	path                           *string
	roleId                         *string
	createDate                     *time.Time
	arn                            *string

	AssumeRolePolicyDocument *IamPolicyDocument                    `json:",omitempty" yaml:",omitempty"`
	PermissionsBoundary      *iamtypes.AttachedPermissionsBoundary `json:",omitempty" yaml:",omitempty"`
	RoleLastUsed             *iamtypes.RoleLastUsed                `json:",omitempty" yaml:",omitempty"`
	RoleName                 *string                               `json:",omitempty" yaml:",omitempty"`
	Tags                     []iamtypes.Tag                        `json:",omitempty" yaml:",omitempty"`
	Allow                    []string                              `json:",omitempty" yaml:",omitempty"`
	Policy                   []string                              `json:",omitempty" yaml:",omitempty"`
}

func (r *IamRole) FromRole(ctx context.Context, role *iamtypes.Role) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamRole.FromRole"}
		d.Start()
		defer d.End()
	}
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
	r.arn = role.Arn
	r.createDate = role.CreateDate
	r.description = role.Description
	r.maxSessionDuration = role.MaxSessionDuration
	r.path = role.Path
	r.PermissionsBoundary = role.PermissionsBoundary
	r.roleId = role.RoleId
	r.RoleLastUsed = role.RoleLastUsed
	r.RoleName = role.RoleName
	r.Tags = role.Tags
	allows, err := IamListRoleAllows(ctx, *r.RoleName)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	for _, allow := range allows {
		r.Allow = append(r.Allow, allow.String())
	}
	policies, err := IamListRolePolicies(ctx, *r.RoleName)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	for _, policy := range policies {
		r.Policy = append(r.Policy, *policy.PolicyName)
	}
	return nil
}

type IamStatementEntry struct {
	Sid       string `json:",omitempty" yaml:",omitempty"`
	Effect    string `json:",omitempty" yaml:",omitempty"`
	Resource  any    `json:",omitempty" yaml:",omitempty"`
	Principal any    `json:",omitempty" yaml:",omitempty"`
	Action    any    `json:",omitempty" yaml:",omitempty"`
}

type IamPolicyDocument struct {
	Version   string              `json:",omitempty" yaml:",omitempty"`
	Id        string              `json:",omitempty" yaml:",omitempty"`
	Statement []IamStatementEntry `json:",omitempty" yaml:",omitempty"`
}

type IamStatementEntryCondition struct {
	Sid       string
	Effect    string `json:",omitempty" yaml:",omitempty"`
	Resource  any    `json:",omitempty" yaml:",omitempty"`
	Principal any    `json:",omitempty" yaml:",omitempty"`
	Action    any    `json:",omitempty" yaml:",omitempty"`
	Condition any    `json:",omitempty" yaml:",omitempty"`
}

type IamPolicyDocumentCondition struct {
	Version   string
	Id        string
	Statement []IamStatementEntryCondition
}

func IamPolicyArn(ctx context.Context, policyName string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamPolicyArn"}
		d.Start()
		defer d.End()
	}
	policies, err := IamListPolicies(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	count := 0
	var policy iamtypes.Policy
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

func IamListUserPolicies(ctx context.Context, username string) ([]*iamtypes.Policy, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamListUserPolicies"}
		d.Start()
		defer d.End()
	}
	var marker *string
	var policies []*iamtypes.Policy
	for {
		out, err := IamClient().ListAttachedUserPolicies(ctx, &iam.ListAttachedUserPoliciesInput{
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
			get, err := IamClient().GetPolicy(ctx, &iam.GetPolicyInput{
				PolicyArn: aws.String(policyArn),
			})
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			policies = append(policies, get.Policy)
		}
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return policies, nil
}

func IamResetUserLoginTempPassword(ctx context.Context, username, password string) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamResetUserLoginTempPassword"}
		d.Start()
		defer d.End()
	}
	_, err := IamClient().UpdateLoginProfile(ctx, &iam.UpdateLoginProfileInput{
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

func IamEnsureUserApi(ctx context.Context, username string, preview bool) (*iamtypes.AccessKey, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamEnsureUserApi"}
		d.Start()
		defer d.End()
	}
	_, err := IamClient().GetUser(ctx, &iam.GetUserInput{
		UserName: aws.String(username),
	})
	if err != nil {
		var nse *iamtypes.NoSuchEntityException
		if errors.As(err, &nse) {
			if !preview {
				_, err := IamClient().CreateUser(ctx, &iam.CreateUserInput{
					UserName: aws.String(username),
				})
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
			}
			Logger.Println(PreviewString(preview)+"iam created user:", username)
		} else {
			Logger.Println("error:", err)
			return nil, err
		}
	}
	out, err := IamClient().ListAccessKeys(ctx, &iam.ListAccessKeysInput{
		MaxItems: aws.Int32(100),
		UserName: aws.String(username),
	})
	if err != nil {
		if !preview {
			Logger.Println("error:", err)
			return nil, err
		}
		out = &iam.ListAccessKeysOutput{}
	}
	if !preview {
		switch len(out.AccessKeyMetadata) {
		case 0:
			out, err := IamClient().CreateAccessKey(ctx, &iam.CreateAccessKeyInput{
				UserName: aws.String(username),
			})
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			Logger.Println(PreviewString(preview)+"created access key for username:", username)
			return out.AccessKey, nil
		case 1:
			return &iamtypes.AccessKey{}, nil // access key is only returned on creation
		default:
			err := fmt.Errorf("more than 1 access key exists for username: %s %d", username, len(out.AccessKeyMetadata))
			return nil, err
		}
	}
	Logger.Println(PreviewString(preview)+"created access key for username:", username)
	return &iamtypes.AccessKey{}, nil
}

func IamEnsureUserLogin(ctx context.Context, username, password string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamEnsureUserLogin"}
		d.Start()
		defer d.End()
	}
	_, err := IamClient().GetUser(ctx, &iam.GetUserInput{
		UserName: aws.String(username),
	})
	if err != nil {
		var nse *iamtypes.NoSuchEntityException
		if errors.As(err, &nse) {
			if !preview {
				_, err := IamClient().CreateUser(ctx, &iam.CreateUserInput{
					UserName: aws.String(username),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"iam created user:", username)
		} else {
			Logger.Println("error:", err)
			return err
		}
	}
	_, err = IamClient().GetLoginProfile(ctx, &iam.GetLoginProfileInput{
		UserName: aws.String(username),
	})
	if err != nil {
		var nse *iamtypes.NoSuchEntityException
		if errors.As(err, &nse) {
			_, err = IamClient().CreateLoginProfile(ctx, &iam.CreateLoginProfileInput{
				Password:              aws.String(password),
				UserName:              aws.String(username),
				PasswordResetRequired: true,
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		} else {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func IamListRolePolicies(ctx context.Context, roleName string) ([]iamtypes.AttachedPolicy, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamListRolePolicies"}
		d.Start()
		defer d.End()
	}
	var policies []iamtypes.AttachedPolicy
	var marker *string
	for {
		out, err := IamClient().ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamListUserAllows"}
		d.Start()
		defer d.End()
	}
	var iamAllows []*IamAllow
	var marker *string
	for {
		out, err := IamClient().ListUserPolicies(ctx, &iam.ListUserPoliciesInput{
			UserName: aws.String(username),
			Marker:   marker,
		})
		if err != nil {
			return nil, err
		}
		for _, policyName := range out.PolicyNames {
			policy, err := IamClient().GetUserPolicy(ctx, &iam.GetUserPolicyInput{
				UserName:   aws.String(username),
				PolicyName: aws.String(policyName),
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
			allows, err := iamAllowsFromPolicyDocument(policyDocument)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			iamAllows = append(iamAllows, allows...)
		}
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return iamAllows, nil
}

func IamListRoleAllows(ctx context.Context, roleName string) ([]*IamAllow, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamListRoleAllows"}
		d.Start()
		defer d.End()
	}
	var iamAllows []*IamAllow
	var marker *string
	for {
		out, err := IamClient().ListRolePolicies(ctx, &iam.ListRolePoliciesInput{
			RoleName: aws.String(roleName),
			Marker:   marker,
		})
		if err != nil {
			return nil, err
		}
		for _, policyName := range out.PolicyNames {
			policy, err := IamClient().GetRolePolicy(ctx, &iam.GetRolePolicyInput{
				RoleName:   aws.String(roleName),
				PolicyName: aws.String(policyName),
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
			allows, err := iamAllowsFromPolicyDocument(policyDocument)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			iamAllows = append(iamAllows, allows...)
		}
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return iamAllows, nil
}

type IamUser struct {
	createDate          *time.Time
	passwordLastUsed    *time.Time
	path                *string
	arn                 *string
	tags                []iamtypes.Tag
	userId              *string
	permissionsBoundary *iamtypes.AttachedPermissionsBoundary

	UserName *string  `json:",omitempty" yaml:",omitempty"`
	Allows   []string `json:",omitempty" yaml:",omitempty"`
	Policies []string `json:",omitempty" yaml:",omitempty"`
}

func (u *IamUser) FromUser(ctx context.Context, user *iamtypes.User) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamUser.FromUser"}
		d.Start()
		defer d.End()
	}
	u.arn = user.Arn
	u.createDate = user.CreateDate
	u.passwordLastUsed = user.PasswordLastUsed
	u.path = user.Path
	u.permissionsBoundary = user.PermissionsBoundary
	u.tags = user.Tags
	u.userId = user.UserId
	u.UserName = user.UserName
	allows, err := IamListUserAllows(ctx, *user.UserName)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	for _, allow := range allows {
		u.Allows = append(u.Allows, allow.String())
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamListUsers"}
		d.Start()
		defer d.End()
	}
	var marker *string
	var result []*IamUser
	for {
		out, err := IamClient().ListUsers(ctx, &iam.ListUsersInput{
			Marker: marker,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, user := range out.Users {
			iamUser := &IamUser{}
			err := iamUser.FromUser(ctx, &user)
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

func IamEnsureEC2SpotRoles(ctx context.Context, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "IamEnsureEC2SpotRoles"}
		d.Start()
		defer d.End()
	}
	doc := IamPolicyDocument{
		Version: "2012-10-17",
		Id:      EC2SpotFleetTaggingRole,
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
	out, err := IamClient().ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
		RoleName: aws.String(EC2SpotFleetTaggingRole),
	})
	policyArn := "arn:aws:iam::aws:policy/service-role/AmazonEC2SpotFleetTaggingRole"
	if err == nil {
		if len(out.AttachedPolicies) != 1 {
			err := fmt.Errorf("%s is misconfigured: %s", EC2SpotFleetTaggingRole, Pformat(out.AttachedPolicies))
			Logger.Println("error:", err)
			return err
		}
		if *out.AttachedPolicies[0].PolicyArn != policyArn {
			err := fmt.Errorf("%s is misconfigured, %s != %s", EC2SpotFleetTaggingRole, *out.AttachedPolicies[0].PolicyArn, policyArn)
			Logger.Println("error:", err)
			return err
		}
	} else {
		if !preview {
			_, err := IamClient().CreateRole(ctx, &iam.CreateRoleInput{
				RoleName:                 aws.String(EC2SpotFleetTaggingRole),
				AssumeRolePolicyDocument: aws.String(string(bytes)),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			_, err = IamClient().AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
				RoleName:  aws.String(EC2SpotFleetTaggingRole),
				PolicyArn: aws.String(policyArn),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview) + "create ec2 spot roles")
	}
	if !preview {
		_, err = IamClient().CreateServiceLinkedRole(ctx, &iam.CreateServiceLinkedRoleInput{
			AWSServiceName: aws.String("spot.amazonaws.com"),
		})
		if err != nil {
			if !strings.Contains(err.Error(), "InvalidInput") {
				Logger.Println("error:", err)
				return err
			}
		}
		_, err = IamClient().CreateServiceLinkedRole(ctx, &iam.CreateServiceLinkedRoleInput{
			AWSServiceName: aws.String("spotfleet.amazonaws.com"),
		})
		if err != nil {
			if !strings.Contains(err.Error(), "InvalidInput") {
				Logger.Println("error:", err)
				return err
			}
		}
	}
	return nil
}
