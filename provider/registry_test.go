package provider_test

import (
	"context"
	"testing"

	"github.com/xraph/nexus/provider"
)

// mockProvider is a minimal provider.Provider implementation for registry tests.
type mockProvider struct {
	name    string
	caps    provider.Capabilities
	healthy bool
	models  []provider.Model
}

func (m *mockProvider) Name() string                     { return m.name }
func (m *mockProvider) Capabilities() provider.Capabilities { return m.caps }
func (m *mockProvider) Models(_ context.Context) ([]provider.Model, error) {
	return m.models, nil
}
func (m *mockProvider) Complete(_ context.Context, _ *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return &provider.CompletionResponse{Provider: m.name}, nil
}
func (m *mockProvider) CompleteStream(_ context.Context, _ *provider.CompletionRequest) (provider.Stream, error) {
	return nil, provider.ErrNotSupported
}
func (m *mockProvider) Embed(_ context.Context, _ *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return &provider.EmbeddingResponse{Provider: m.name}, nil
}
func (m *mockProvider) Healthy(_ context.Context) bool { return m.healthy }

// newMock creates a mock provider with the given name, capabilities, and health.
func newMock(name string, caps provider.Capabilities, healthy bool) *mockProvider {
	return &mockProvider{name: name, caps: caps, healthy: healthy}
}

func TestNewRegistry(t *testing.T) {
	reg := provider.NewRegistry()
	if reg == nil {
		t.Fatal("NewRegistry() returned nil")
	}
	if reg.Count() != 0 {
		t.Errorf("new registry Count() = %d, want 0", reg.Count())
	}
}

func TestRegister_And_Get(t *testing.T) {
	reg := provider.NewRegistry()

	p := newMock("openai", provider.Capabilities{Chat: true}, true)
	reg.Register(p)

	got, ok := reg.Get("openai")
	if !ok {
		t.Fatal("Get(openai) returned false")
	}
	if got.Name() != "openai" {
		t.Errorf("Get(openai).Name() = %q, want %q", got.Name(), "openai")
	}
}

func TestRegister_MultipleProviders(t *testing.T) {
	reg := provider.NewRegistry()

	providers := []*mockProvider{
		newMock("openai", provider.Capabilities{Chat: true}, true),
		newMock("anthropic", provider.Capabilities{Chat: true}, true),
		newMock("ollama", provider.Capabilities{Chat: true}, false),
	}
	for _, p := range providers {
		reg.Register(p)
	}

	for _, p := range providers {
		got, ok := reg.Get(p.Name())
		if !ok {
			t.Errorf("Get(%q) returned false", p.Name())
			continue
		}
		if got.Name() != p.Name() {
			t.Errorf("Get(%q).Name() = %q", p.Name(), got.Name())
		}
	}
}

func TestGet_UnknownName(t *testing.T) {
	reg := provider.NewRegistry()

	reg.Register(newMock("openai", provider.Capabilities{Chat: true}, true))

	_, ok := reg.Get("nonexistent")
	if ok {
		t.Error("Get(nonexistent) should return false")
	}
}

func TestGet_EmptyRegistry(t *testing.T) {
	reg := provider.NewRegistry()

	_, ok := reg.Get("anything")
	if ok {
		t.Error("Get on empty registry should return false")
	}
}

func TestAll_ReturnsRegistrationOrder(t *testing.T) {
	reg := provider.NewRegistry()

	names := []string{"first", "second", "third", "fourth"}
	for _, name := range names {
		reg.Register(newMock(name, provider.Capabilities{Chat: true}, true))
	}

	all := reg.All()
	if len(all) != len(names) {
		t.Fatalf("All() returned %d providers, want %d", len(all), len(names))
	}
	for i, p := range all {
		if p.Name() != names[i] {
			t.Errorf("All()[%d].Name() = %q, want %q", i, p.Name(), names[i])
		}
	}
}

func TestAll_EmptyRegistry(t *testing.T) {
	reg := provider.NewRegistry()

	all := reg.All()
	if len(all) != 0 {
		t.Errorf("All() on empty registry returned %d providers, want 0", len(all))
	}
}

func TestCount(t *testing.T) {
	reg := provider.NewRegistry()

	if reg.Count() != 0 {
		t.Errorf("Count() on empty registry = %d, want 0", reg.Count())
	}

	reg.Register(newMock("a", provider.Capabilities{Chat: true}, true))
	if reg.Count() != 1 {
		t.Errorf("Count() after 1 register = %d, want 1", reg.Count())
	}

	reg.Register(newMock("b", provider.Capabilities{Chat: true}, true))
	if reg.Count() != 2 {
		t.Errorf("Count() after 2 registers = %d, want 2", reg.Count())
	}

	reg.Register(newMock("c", provider.Capabilities{Chat: true}, true))
	if reg.Count() != 3 {
		t.Errorf("Count() after 3 registers = %d, want 3", reg.Count())
	}
}

func TestRegister_OverwritesSameName(t *testing.T) {
	reg := provider.NewRegistry()

	old := newMock("openai", provider.Capabilities{Chat: true}, false)
	reg.Register(old)

	// Overwrite with a new provider using the same name but different caps
	replacement := newMock("openai", provider.Capabilities{Chat: true, Embeddings: true}, true)
	reg.Register(replacement)

	// Count should not increase
	if reg.Count() != 1 {
		t.Errorf("Count() after overwrite = %d, want 1", reg.Count())
	}

	// Should get the replacement
	got, ok := reg.Get("openai")
	if !ok {
		t.Fatal("Get(openai) returned false after overwrite")
	}
	if !got.Capabilities().Embeddings {
		t.Error("overwritten provider should have Embeddings=true")
	}
	if !got.Healthy(context.Background()) {
		t.Error("overwritten provider should be healthy")
	}
}

func TestRegister_OverwritePreservesOrder(t *testing.T) {
	reg := provider.NewRegistry()

	reg.Register(newMock("first", provider.Capabilities{Chat: true}, true))
	reg.Register(newMock("second", provider.Capabilities{Chat: true}, true))
	reg.Register(newMock("third", provider.Capabilities{Chat: true}, true))

	// Overwrite "second" - order should stay the same
	reg.Register(newMock("second", provider.Capabilities{Chat: true, Embeddings: true}, true))

	all := reg.All()
	if len(all) != 3 {
		t.Fatalf("All() returned %d, want 3", len(all))
	}

	expected := []string{"first", "second", "third"}
	for i, p := range all {
		if p.Name() != expected[i] {
			t.Errorf("All()[%d].Name() = %q, want %q", i, p.Name(), expected[i])
		}
	}
}

func TestWithCapability_FiltersByChat(t *testing.T) {
	reg := provider.NewRegistry()

	reg.Register(newMock("chat-only", provider.Capabilities{Chat: true}, true))
	reg.Register(newMock("embed-only", provider.Capabilities{Embeddings: true}, true))
	reg.Register(newMock("both", provider.Capabilities{Chat: true, Embeddings: true}, true))

	chatProviders := reg.WithCapability("chat")
	if len(chatProviders) != 2 {
		t.Fatalf("WithCapability(chat) returned %d providers, want 2", len(chatProviders))
	}

	names := make(map[string]bool)
	for _, p := range chatProviders {
		names[p.Name()] = true
	}
	if !names["chat-only"] {
		t.Error("WithCapability(chat) should include chat-only")
	}
	if !names["both"] {
		t.Error("WithCapability(chat) should include both")
	}
	if names["embed-only"] {
		t.Error("WithCapability(chat) should not include embed-only")
	}
}

func TestWithCapability_FiltersByEmbeddings(t *testing.T) {
	reg := provider.NewRegistry()

	reg.Register(newMock("chat-only", provider.Capabilities{Chat: true}, true))
	reg.Register(newMock("embed-only", provider.Capabilities{Embeddings: true}, true))
	reg.Register(newMock("both", provider.Capabilities{Chat: true, Embeddings: true}, true))

	embedProviders := reg.WithCapability("embeddings")
	if len(embedProviders) != 2 {
		t.Fatalf("WithCapability(embeddings) returned %d providers, want 2", len(embedProviders))
	}

	names := make(map[string]bool)
	for _, p := range embedProviders {
		names[p.Name()] = true
	}
	if !names["embed-only"] {
		t.Error("WithCapability(embeddings) should include embed-only")
	}
	if !names["both"] {
		t.Error("WithCapability(embeddings) should include both")
	}
}

func TestWithCapability_FiltersByStreaming(t *testing.T) {
	reg := provider.NewRegistry()

	reg.Register(newMock("with-stream", provider.Capabilities{Chat: true, Streaming: true}, true))
	reg.Register(newMock("no-stream", provider.Capabilities{Chat: true}, true))

	result := reg.WithCapability("streaming")
	if len(result) != 1 {
		t.Fatalf("WithCapability(streaming) returned %d, want 1", len(result))
	}
	if result[0].Name() != "with-stream" {
		t.Errorf("expected with-stream, got %q", result[0].Name())
	}
}

func TestWithCapability_FiltersByTools(t *testing.T) {
	reg := provider.NewRegistry()

	reg.Register(newMock("with-tools", provider.Capabilities{Chat: true, Tools: true}, true))
	reg.Register(newMock("no-tools", provider.Capabilities{Chat: true}, true))

	result := reg.WithCapability("tools")
	if len(result) != 1 {
		t.Fatalf("WithCapability(tools) returned %d, want 1", len(result))
	}
	if result[0].Name() != "with-tools" {
		t.Errorf("expected with-tools, got %q", result[0].Name())
	}
}

func TestWithCapability_UnknownCapability(t *testing.T) {
	reg := provider.NewRegistry()

	reg.Register(newMock("any", provider.Capabilities{Chat: true}, true))

	result := reg.WithCapability("teleportation")
	if len(result) != 0 {
		t.Errorf("WithCapability(teleportation) returned %d, want 0", len(result))
	}
}

func TestWithCapability_EmptyRegistry(t *testing.T) {
	reg := provider.NewRegistry()

	result := reg.WithCapability("chat")
	if len(result) != 0 {
		t.Errorf("WithCapability on empty registry returned %d, want 0", len(result))
	}
}

func TestWithCapability_PreservesOrder(t *testing.T) {
	reg := provider.NewRegistry()

	reg.Register(newMock("alpha", provider.Capabilities{Chat: true, Streaming: true}, true))
	reg.Register(newMock("beta", provider.Capabilities{Chat: true}, true))
	reg.Register(newMock("gamma", provider.Capabilities{Chat: true, Streaming: true}, true))

	result := reg.WithCapability("streaming")
	if len(result) != 2 {
		t.Fatalf("got %d, want 2", len(result))
	}
	if result[0].Name() != "alpha" {
		t.Errorf("result[0] = %q, want alpha", result[0].Name())
	}
	if result[1].Name() != "gamma" {
		t.Errorf("result[1] = %q, want gamma", result[1].Name())
	}
}

func TestHealthy_FiltersUnhealthy(t *testing.T) {
	reg := provider.NewRegistry()

	reg.Register(newMock("healthy-1", provider.Capabilities{Chat: true}, true))
	reg.Register(newMock("unhealthy", provider.Capabilities{Chat: true}, false))
	reg.Register(newMock("healthy-2", provider.Capabilities{Chat: true}, true))

	ctx := context.Background()
	healthy := reg.Healthy(ctx)

	if len(healthy) != 2 {
		t.Fatalf("Healthy() returned %d providers, want 2", len(healthy))
	}

	names := make(map[string]bool)
	for _, p := range healthy {
		names[p.Name()] = true
	}
	if !names["healthy-1"] {
		t.Error("Healthy() should include healthy-1")
	}
	if !names["healthy-2"] {
		t.Error("Healthy() should include healthy-2")
	}
	if names["unhealthy"] {
		t.Error("Healthy() should not include unhealthy")
	}
}

func TestHealthy_AllUnhealthy(t *testing.T) {
	reg := provider.NewRegistry()

	reg.Register(newMock("a", provider.Capabilities{Chat: true}, false))
	reg.Register(newMock("b", provider.Capabilities{Chat: true}, false))

	ctx := context.Background()
	healthy := reg.Healthy(ctx)

	if len(healthy) != 0 {
		t.Errorf("Healthy() when all unhealthy returned %d, want 0", len(healthy))
	}
}

func TestHealthy_AllHealthy(t *testing.T) {
	reg := provider.NewRegistry()

	reg.Register(newMock("a", provider.Capabilities{Chat: true}, true))
	reg.Register(newMock("b", provider.Capabilities{Chat: true}, true))
	reg.Register(newMock("c", provider.Capabilities{Chat: true}, true))

	ctx := context.Background()
	healthy := reg.Healthy(ctx)

	if len(healthy) != 3 {
		t.Errorf("Healthy() when all healthy returned %d, want 3", len(healthy))
	}
}

func TestHealthy_EmptyRegistry(t *testing.T) {
	reg := provider.NewRegistry()

	ctx := context.Background()
	healthy := reg.Healthy(ctx)

	if len(healthy) != 0 {
		t.Errorf("Healthy() on empty registry returned %d, want 0", len(healthy))
	}
}

func TestHealthy_PreservesOrder(t *testing.T) {
	reg := provider.NewRegistry()

	reg.Register(newMock("first", provider.Capabilities{Chat: true}, true))
	reg.Register(newMock("skip", provider.Capabilities{Chat: true}, false))
	reg.Register(newMock("second", provider.Capabilities{Chat: true}, true))
	reg.Register(newMock("third", provider.Capabilities{Chat: true}, true))

	ctx := context.Background()
	healthy := reg.Healthy(ctx)

	if len(healthy) != 3 {
		t.Fatalf("Healthy() returned %d, want 3", len(healthy))
	}
	expected := []string{"first", "second", "third"}
	for i, p := range healthy {
		if p.Name() != expected[i] {
			t.Errorf("Healthy()[%d] = %q, want %q", i, p.Name(), expected[i])
		}
	}
}

func TestForModel_ReturnsAllProviders(t *testing.T) {
	// ForModel currently returns all providers (filtering not yet implemented).
	reg := provider.NewRegistry()

	reg.Register(newMock("a", provider.Capabilities{Chat: true}, true))
	reg.Register(newMock("b", provider.Capabilities{Chat: true}, true))
	reg.Register(newMock("c", provider.Capabilities{Chat: true}, true))

	result := reg.ForModel("gpt-4o")
	if len(result) != 3 {
		t.Errorf("ForModel() returned %d, want 3", len(result))
	}
}

func TestForModel_EmptyRegistry(t *testing.T) {
	reg := provider.NewRegistry()

	result := reg.ForModel("any-model")
	if len(result) != 0 {
		t.Errorf("ForModel on empty registry returned %d, want 0", len(result))
	}
}

func TestForModel_PreservesOrder(t *testing.T) {
	reg := provider.NewRegistry()

	reg.Register(newMock("alpha", provider.Capabilities{Chat: true}, true))
	reg.Register(newMock("beta", provider.Capabilities{Chat: true}, true))

	result := reg.ForModel("test-model")
	if len(result) != 2 {
		t.Fatalf("ForModel() returned %d, want 2", len(result))
	}
	if result[0].Name() != "alpha" {
		t.Errorf("result[0] = %q, want alpha", result[0].Name())
	}
	if result[1].Name() != "beta" {
		t.Errorf("result[1] = %q, want beta", result[1].Name())
	}
}
