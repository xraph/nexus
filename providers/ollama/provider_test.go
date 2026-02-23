package ollama

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xraph/nexus/providertest"
	"github.com/xraph/nexus/testutil"
)

func TestName(t *testing.T) {
	p := New()
	if p.Name() != "ollama" {
		t.Fatalf("got %q, want %q", p.Name(), "ollama")
	}
}

func TestCapabilities(t *testing.T) {
	p := New()
	caps := p.Capabilities()
	if !caps.Chat {
		t.Error("expected Chat capability")
	}
	if !caps.Streaming {
		t.Error("expected Streaming capability")
	}
	if !caps.Embeddings {
		t.Error("expected Embeddings capability")
	}
	if !caps.Vision {
		t.Error("expected Vision capability")
	}
	if !caps.Tools {
		t.Error("expected Tools capability")
	}
	if !caps.JSON {
		t.Error("expected JSON capability")
	}
}

func TestModels(t *testing.T) {
	p := New()
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("no models")
	}
	for _, m := range models {
		if m.Provider != "ollama" {
			t.Errorf("model %q provider=%q, want %q", m.ID, m.Provider, "ollama")
		}
	}
}

func TestComplete(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New(WithBaseURL(mock.Server.URL))
	providertest.TestProviderComplete(t, p)
}

func TestHealthy(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New(WithBaseURL(mock.Server.URL))
	if !p.Healthy(context.Background()) {
		t.Error("expected healthy")
	}
}

func TestEmbed(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetEmbedding(json.RawMessage(testutil.DefaultEmbeddingResponse()))
	p := New(WithBaseURL(mock.Server.URL))
	providertest.TestProviderEmbed(t, p)
}

func TestConformance(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New(WithBaseURL(mock.Server.URL))
	providertest.TestProviderContract(t, p)
}
