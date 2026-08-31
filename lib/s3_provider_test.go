package lib

import "testing"

func TestS3CommandDescriptionMakesProviderSupportExplicit(t *testing.T) {
	tests := []struct {
		name       string
		summary    string
		supportsR2 bool
		want       string
	}{
		{name: "AWS only", summary: "list versions", want: "\nlist versions\n\nprovider: AWS S3 only\n"},
		{name: "AWS and R2", summary: "list objects", supportsR2: true, want: "\nlist objects\n\nproviders: AWS S3 (default), Cloudflare R2 (--r2)\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := S3CommandDescription(test.summary, test.supportsR2); got != test.want {
				t.Fatalf("description = %q, want %q", got, test.want)
			}
		})
	}
}
