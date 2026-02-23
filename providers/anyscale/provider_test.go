package anyscale

import (
	"context"
	"testing"

	"github.com/xraph/nexus/providertest"
	"github.com/xraph/nexus/testutil"
)

func TestName(t *testing.T) {
	p := New("test-key")
	if p.Name() != "anyscale" {
		t.Fatalf("got %q, want %q", p.Name(), "anyscale")
	}
}

func TestCapabilities(t *testing.T) {
	p := New("test-key")
	caps := p.Capabilities()
	if !caps.Chat {
		t.Error("expected Chat capability")
	}
	if !caps.Streaming {
		t.Error("expected Streaming capability")
	}
	if !caps.JSON {
		t.Error("expected JSON capability")
	}
	if caps.Embeddings {
		t.Error("expected Embeddings to be false")
	}
}

func TestModels(t *testing.T) {
	p := New("test-key")
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("no models")
	}
	for _, m := range models {
		if m.Provider != "anyscale" {
			t.Errorf("model %q provider=%q, want %q", m.ID, m.Provider, "anyscale")
		}
	}
}

func TestComplete(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New("test-key", WithBaseURL(mock.Server.URL))
	providertest.TestProviderComplete(t, p)
}

func TestHealthy(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New("test-key", WithBaseURL(mock.Server.URL))
	if !p.Healthy(context.Background()) {
		t.Error("expected healthy")
	}
}

func TestEmbedNotSupported(t *testing.T) {
	p := New("test-key")
	providertest.TestProviderEmbedNotSupported(t, p)
}

func TestConformance(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New("test-key", WithBaseURL(mock.Server.URL))
	providertest.TestProviderContract(t, p)
}
