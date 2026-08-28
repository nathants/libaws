package lib

import (
	"errors"
	"slices"
	"testing"
)

func TestApplyLambdaUpdateStagesConfiguresBeforePublishingCode(t *testing.T) {
	var stages []string
	err := applyLambdaUpdateStages(
		func() error {
			stages = append(stages, "configuration")
			return nil
		},
		func() error {
			stages = append(stages, "code")
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(stages, []string{"configuration", "code"}) {
		t.Fatalf("Lambda update stages=%v, want configuration before code", stages)
	}
}

func TestApplyLambdaUpdateStagesDoesNotPublishCodeAfterConfigurationFailure(t *testing.T) {
	configurationErr := errors.New("configuration failed")
	codeCalled := false
	err := applyLambdaUpdateStages(
		func() error { return configurationErr },
		func() error {
			codeCalled = true
			return nil
		},
	)
	if !errors.Is(err, configurationErr) {
		t.Fatalf("Lambda update error=%v, want configuration failure", err)
	}
	if codeCalled {
		t.Fatal("Lambda code was published after configuration failed")
	}
}
