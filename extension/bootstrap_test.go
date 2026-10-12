package extension

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/xraph/forge"

	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/store"
)

var (
	envKey    = "nxs_" + strings.Repeat("e1", 32)
	configKey = "nxs_" + strings.Repeat("c2", 32)
)

// startWith builds an extension the way Register would configure it (config,
// then the env fallback, then the gateway options), starts it on a memory
// store and returns it. It does not mount any routes.
func startWith(t *testing.T, cfg Config) *Extension {
	t.Helper()
	e := New(WithDatabase(store.NewMemory()))
	e.SetLogger(forge.NewNoopLogger())
	e.config = e.mergeWithDefaults(cfg)
	e.resolveBootstrapAdminKey()
	e.applyConfigToGatewayOpts()
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop(context.Background()) })
	return e
}

func mustValidate(t *testing.T, e *Extension, raw string) *key.APIKey {
	t.Helper()
	k, err := e.Gateway().Keys().Validate(context.Background(), raw)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	return k
}

func TestBootstrapAdminKeyComesFromTheEnvVarWhenTheConfigIsEmpty(t *testing.T) {
	t.Setenv("NEXUS_BOOTSTRAP_ADMIN_KEY", envKey)
	e := startWith(t, Config{})
	k := mustValidate(t, e, envKey)
	if len(k.Scopes) != 1 || k.Scopes[0] != key.ScopeAdmin {
		t.Fatalf("scopes = %v, want [admin]", k.Scopes)
	}
}

func TestBootstrapAdminKeyInTheConfigBeatsTheEnvVar(t *testing.T) {
	t.Setenv("NEXUS_BOOTSTRAP_ADMIN_KEY", envKey)
	e := startWith(t, Config{BootstrapAdminKey: configKey})
	mustValidate(t, e, configKey)
	if _, err := e.Gateway().Keys().Validate(context.Background(), envKey); err == nil {
		t.Fatal("the env var key must not be created when the config sets one")
	}
}

func TestNoBootstrapAdminKeyMeansNoKeyAndNoTenant(t *testing.T) {
	t.Setenv("NEXUS_BOOTSTRAP_ADMIN_KEY", "")
	e := startWith(t, Config{})
	res, err := e.Gateway().Keys().ListPage(context.Background(), &key.ListOptions{})
	if err != nil || len(res.Items) != 0 {
		t.Fatalf("keys = %+v (err %v), want none", res, err)
	}
}

func TestBootstrapAdminKeyIsCreatedEvenWhenMigrationIsDisabled(t *testing.T) {
	e := startWith(t, Config{BootstrapAdminKey: configKey, DisableMigrate: true})
	mustValidate(t, e, configKey)
}

func TestBootstrapAdminKeyMergesLikeBasePath(t *testing.T) {
	e := New()
	merged := e.mergeConfigurations(Config{BootstrapAdminKey: configKey}, Config{BootstrapAdminKey: envKey})
	if merged.BootstrapAdminKey != configKey {
		t.Fatal("the YAML value must win")
	}
	merged = e.mergeConfigurations(Config{}, Config{BootstrapAdminKey: envKey})
	if merged.BootstrapAdminKey != envKey {
		t.Fatal("the programmatic value must fill a YAML gap")
	}
}

func TestAMalformedBootstrapAdminKeyFailsStartNamingItsSourceAndLength(t *testing.T) {
	const bad = "swordfish-not-a-key"
	for name, tc := range map[string]struct{ cfg, env, source string }{
		"config": {cfg: bad, source: "bootstrap_admin_key"},
		"env":    {env: bad, source: "NEXUS_BOOTSTRAP_ADMIN_KEY"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("NEXUS_BOOTSTRAP_ADMIN_KEY", tc.env)
			e := New(WithDatabase(store.NewMemory()))
			e.SetLogger(forge.NewNoopLogger())
			e.config = e.mergeWithDefaults(Config{BootstrapAdminKey: tc.cfg})
			e.resolveBootstrapAdminKey()
			e.applyConfigToGatewayOpts()
			err := e.Start(context.Background())
			if err == nil {
				t.Fatal("start must fail on a malformed bootstrap admin key")
			}
			msg := err.Error()
			if strings.Contains(msg, bad) {
				t.Fatal("the error echoes the key")
			}
			if !strings.Contains(msg, tc.source) || !strings.Contains(msg, fmt.Sprintf("%d characters", len(bad))) {
				t.Fatalf("error %q must name %s and the length %d", msg, tc.source, len(bad))
			}
			if strings.Count(msg, "nexus:") != 1 {
				t.Fatalf("error %q repeats its nexus: prefix", msg)
			}
		})
	}
}

func TestBootstrapAdminKeyIsTrimmedFromConfigAndEnv(t *testing.T) {
	t.Setenv("NEXUS_BOOTSTRAP_ADMIN_KEY", "")
	e := startWith(t, Config{BootstrapAdminKey: " \t" + configKey + "\n"})
	mustValidate(t, e, configKey)

	t.Setenv("NEXUS_BOOTSTRAP_ADMIN_KEY", envKey+"\n")
	e = startWith(t, Config{})
	mustValidate(t, e, envKey)
}

// registerAndStart takes the real path: the app's config manager under
// extensions.nexus, then Register (which runs loadConfiguration and so the
// env fallback), then Start.
func registerAndStart(t *testing.T, keys map[string]any) *Extension {
	t.Helper()
	cm := forge.NewManager()
	if keys != nil {
		cm.Set("extensions.nexus", keys)
	}
	e := New(WithDatabase(store.NewMemory()), WithDisableRoutes())
	if err := e.Register(forge.New(forge.WithAppName("t"), forge.WithAppConfigManager(cm))); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop(context.Background()) })
	return e
}

func TestRegisterReadsTheBootstrapAdminKeyFromTheEnvVar(t *testing.T) {
	t.Setenv("NEXUS_BOOTSTRAP_ADMIN_KEY", envKey)
	e := registerAndStart(t, nil)
	mustValidate(t, e, envKey)
}

func TestRegisterReadsTheBootstrapAdminKeyFromYAMLAndItBeatsTheEnvVar(t *testing.T) {
	t.Setenv("NEXUS_BOOTSTRAP_ADMIN_KEY", envKey)
	e := registerAndStart(t, map[string]any{"bootstrap_admin_key": configKey})
	mustValidate(t, e, configKey)
	if _, err := e.Gateway().Keys().Validate(context.Background(), envKey); err == nil {
		t.Fatal("the env var key must not be created when the YAML sets one")
	}
}
