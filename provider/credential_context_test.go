package provider_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/xraph/nexus/provider"
)

func TestCredentialRoundTripsThroughTheContext(t *testing.T) {
	ctx := context.Background()
	if _, ok := provider.CredentialFromContext(ctx); ok {
		t.Fatal("a bare context carries a credential")
	}

	want := provider.Credential{APIKey: "sk-tenant", BaseURL: "https://llm.example.com/v1"}
	got, ok := provider.CredentialFromContext(provider.WithCredential(ctx, want))
	if !ok || got != want {
		t.Fatalf("CredentialFromContext = %+v, %v; want %+v, true", got, ok, want)
	}

	// An empty credential is no credential, so it can clear one set earlier.
	cleared := provider.WithCredential(provider.WithCredential(ctx, want), provider.Credential{})
	if c, ok := provider.CredentialFromContext(cleared); ok {
		t.Fatalf("an empty credential still reads as %+v", c)
	}
}

func TestCredentialNeverPrintsItsKey(t *testing.T) {
	c := provider.Credential{APIKey: "sk-very-secret", BaseURL: "https://llm.example.com"}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if out := fmt.Sprintf(format, c); strings.Contains(out, "sk-very-secret") {
			t.Errorf("%s printed the key: %s", format, out)
		}
	}
	if out := fmt.Sprintf("%v", &c); strings.Contains(out, "sk-very-secret") {
		t.Errorf("a pointer printed the key: %s", out)
	}
}

func TestStaticAndEnvCredentialsReportNotFound(t *testing.T) {
	_, err := provider.NewStaticCredential(nil).GetCredential(context.Background(), "openai")
	if !errors.Is(err, provider.ErrCredentialNotFound) {
		t.Fatalf("static: err = %v, want ErrCredentialNotFound", err)
	}
	_, err = provider.NewEnvCredential("NEXUS_TEST_NO_SUCH_").GetCredential(context.Background(), "openai")
	if !errors.Is(err, provider.ErrCredentialNotFound) {
		t.Fatalf("env: err = %v, want ErrCredentialNotFound", err)
	}
}
