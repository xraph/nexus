// Package extension provides the Forge extension adapter for Nexus.
//
// It implements the forge.Extension interface to integrate the Nexus
// AI gateway into a Forge application with automatic dependency
// discovery, DI registration, and lifecycle management.
//
// Configuration can be provided programmatically via Option functions
// or via YAML configuration files under "extensions.nexus" or "nexus" keys.
package extension

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/grove"
	"github.com/xraph/vessel"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/store"
	mongostore "github.com/xraph/nexus/store/mongo"
	pgstore "github.com/xraph/nexus/store/postgres"
	sqlitestore "github.com/xraph/nexus/store/sqlite"
)

// ExtensionName is the name registered with Forge.
const ExtensionName = "nexus"

// ExtensionDescription is the human-readable description.
const ExtensionDescription = "Composable AI gateway — route, cache, guard, and observe LLM traffic"

// ExtensionVersion is the semantic version.
const ExtensionVersion = "0.1.0"

// Ensure Extension implements forge.Extension at compile time.
var _ forge.Extension = (*Extension)(nil)

// Extension adapts Nexus as a Forge extension.
type Extension struct {
	*forge.BaseExtension

	config      Config
	gateway     *nexus.Gateway
	gatewayOpts []nexus.Option
	useGrove    bool
}

// New creates a new Nexus Forge extension with the given options.
func New(opts ...Option) *Extension {
	e := &Extension{
		BaseExtension: forge.NewBaseExtension(ExtensionName, ExtensionVersion, ExtensionDescription),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Gateway returns the underlying Nexus gateway instance.
// This is nil until Start is called.
func (e *Extension) Gateway() *nexus.Gateway { return e.gateway }

// Register implements [forge.Extension]. It loads configuration
// and registers the gateway constructor in the DI container.
func (e *Extension) Register(fapp forge.App) error {
	if err := e.BaseExtension.Register(fapp); err != nil {
		return err
	}

	if err := e.loadConfiguration(); err != nil {
		return err
	}

	// Resolve store from grove DI if configured.
	if e.useGrove {
		groveDB, err := e.resolveGroveDB(fapp)
		if err != nil {
			return fmt.Errorf("nexus: %w", err)
		}
		s, err := e.buildStoreFromGroveDB(groveDB)
		if err != nil {
			return err
		}
		e.gatewayOpts = append(e.gatewayOpts, nexus.WithDatabase(s))
	}

	// Apply extension config to gateway options.
	e.applyConfigToGatewayOpts()

	// Register the gateway in the DI container so other extensions
	// (Chronicle, Shield, Relay, Authsome) can discover it.
	return vessel.Provide(fapp.Container(), func() (*nexus.Gateway, error) {
		return e.gateway, nil
	})
}

// Start implements [forge.Extension]. It creates and initializes the gateway.
func (e *Extension) Start(ctx context.Context) error {
	gw := nexus.New(e.gatewayOpts...)
	if err := gw.Initialize(ctx); err != nil {
		return fmt.Errorf("nexus: failed to initialize gateway: %w", err)
	}
	e.gateway = gw

	// Run store migration unless disabled.
	if !e.config.DisableMigrate && gw.Store() != nil {
		if err := gw.Store().Migrate(); err != nil {
			return fmt.Errorf("nexus: failed to migrate store: %w", err)
		}
	}

	e.Logger().Info("nexus: gateway started",
		forge.F("providers", gw.Providers().Count()),
		forge.F("extensions", gw.Extensions().Count()),
		forge.F("base_path", e.config.BasePath),
	)

	e.MarkStarted()
	return nil
}

// Stop implements [forge.Extension]. It gracefully shuts down the gateway.
func (e *Extension) Stop(ctx context.Context) error {
	if e.gateway != nil {
		if err := e.gateway.Shutdown(ctx); err != nil {
			e.MarkStopped()
			return fmt.Errorf("nexus: shutdown error: %w", err)
		}
	}
	e.MarkStopped()
	return nil
}

// Health implements [forge.Extension].
func (e *Extension) Health(_ context.Context) error {
	if e.gateway == nil {
		return fmt.Errorf("nexus: gateway not initialized")
	}
	return nil
}

// --- Config Loading (mirrors grove extension pattern) ---

// applyConfigToGatewayOpts translates extension Config fields into
// nexus.Option values that are applied when the gateway is created.
func (e *Extension) applyConfigToGatewayOpts() {
	if e.config.BasePath != "" {
		e.gatewayOpts = append(e.gatewayOpts, nexus.WithBasePath(e.config.BasePath))
	}
	if e.config.DefaultTimeout > 0 {
		e.gatewayOpts = append(e.gatewayOpts, nexus.WithTimeout(e.config.DefaultTimeout))
	}
	if e.config.DefaultMaxRetries > 0 {
		e.gatewayOpts = append(e.gatewayOpts, nexus.WithMaxRetries(e.config.DefaultMaxRetries))
	}
	if e.config.GlobalRateLimit > 0 {
		e.gatewayOpts = append(e.gatewayOpts, nexus.WithRateLimit(e.config.GlobalRateLimit))
	}
}

// loadConfiguration loads config from YAML files or programmatic sources.
func (e *Extension) loadConfiguration() error {
	programmaticConfig := e.config

	// Try loading from config file.
	fileConfig, configLoaded := e.tryLoadFromConfigFile()

	if !configLoaded {
		if programmaticConfig.RequireConfig {
			return errors.New("nexus: configuration is required but not found in config files; " +
				"ensure 'extensions.nexus' or 'nexus' key exists in your config")
		}

		// Use programmatic config merged with defaults.
		e.config = e.mergeWithDefaults(programmaticConfig)
	} else {
		// Config loaded from YAML -- merge with programmatic options.
		e.config = e.mergeConfigurations(fileConfig, programmaticConfig)
	}

	// Enable grove resolution if YAML config specifies a grove database.
	if e.config.GroveDatabase != "" {
		e.useGrove = true
	}

	e.Logger().Debug("nexus: configuration loaded",
		forge.F("disable_routes", e.config.DisableRoutes),
		forge.F("disable_migrate", e.config.DisableMigrate),
		forge.F("base_path", e.config.BasePath),
		forge.F("grove_database", e.config.GroveDatabase),
	)

	return nil
}

// tryLoadFromConfigFile attempts to load config from YAML files.
func (e *Extension) tryLoadFromConfigFile() (Config, bool) {
	cm := e.App().Config()
	var cfg Config

	// Try "extensions.nexus" first (namespaced pattern).
	if cm.IsSet("extensions.nexus") {
		if err := cm.Bind("extensions.nexus", &cfg); err == nil {
			e.Logger().Debug("nexus: loaded config from file",
				forge.F("key", "extensions.nexus"),
			)
			return cfg, true
		}
		e.Logger().Warn("nexus: failed to bind extensions.nexus config",
			forge.F("error", "bind failed"),
		)
	}

	// Try legacy "nexus" key.
	if cm.IsSet("nexus") {
		if err := cm.Bind("nexus", &cfg); err == nil {
			e.Logger().Debug("nexus: loaded config from file",
				forge.F("key", "nexus"),
			)
			return cfg, true
		}
		e.Logger().Warn("nexus: failed to bind nexus config",
			forge.F("error", "bind failed"),
		)
	}

	return Config{}, false
}

// mergeWithDefaults fills zero-valued fields with defaults.
func (e *Extension) mergeWithDefaults(cfg Config) Config {
	defaults := DefaultConfig()

	if cfg.BasePath == "" {
		cfg.BasePath = defaults.BasePath
	}
	if cfg.DefaultTimeout == 0 {
		cfg.DefaultTimeout = defaults.DefaultTimeout
	}
	if cfg.DefaultMaxRetries == 0 {
		cfg.DefaultMaxRetries = defaults.DefaultMaxRetries
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = defaults.LogLevel
	}
	if cfg.EnableUsage == nil {
		cfg.EnableUsage = defaults.EnableUsage
	}

	return cfg
}

// mergeConfigurations merges YAML config with programmatic options.
// YAML config takes precedence for most fields; programmatic bool flags fill gaps.
func (e *Extension) mergeConfigurations(yamlConfig, programmaticConfig Config) Config {
	// Programmatic bool flags override when true.
	if programmaticConfig.DisableRoutes {
		yamlConfig.DisableRoutes = true
	}
	if programmaticConfig.DisableMigrate {
		yamlConfig.DisableMigrate = true
	}
	if programmaticConfig.EnableCache {
		yamlConfig.EnableCache = true
	}

	// String fields: YAML takes precedence.
	if yamlConfig.BasePath == "" && programmaticConfig.BasePath != "" {
		yamlConfig.BasePath = programmaticConfig.BasePath
	}
	if yamlConfig.LogLevel == "" && programmaticConfig.LogLevel != "" {
		yamlConfig.LogLevel = programmaticConfig.LogLevel
	}
	if yamlConfig.GroveDatabase == "" && programmaticConfig.GroveDatabase != "" {
		yamlConfig.GroveDatabase = programmaticConfig.GroveDatabase
	}

	// Duration/int fields: YAML takes precedence, programmatic fills gaps.
	if yamlConfig.DefaultTimeout == 0 && programmaticConfig.DefaultTimeout != 0 {
		yamlConfig.DefaultTimeout = programmaticConfig.DefaultTimeout
	}
	if yamlConfig.DefaultMaxRetries == 0 && programmaticConfig.DefaultMaxRetries != 0 {
		yamlConfig.DefaultMaxRetries = programmaticConfig.DefaultMaxRetries
	}
	if yamlConfig.GlobalRateLimit == 0 && programmaticConfig.GlobalRateLimit != 0 {
		yamlConfig.GlobalRateLimit = programmaticConfig.GlobalRateLimit
	}

	// Pointer fields: YAML takes precedence.
	if yamlConfig.EnableUsage == nil && programmaticConfig.EnableUsage != nil {
		yamlConfig.EnableUsage = programmaticConfig.EnableUsage
	}

	// Fill remaining zeros with defaults.
	return e.mergeWithDefaults(yamlConfig)
}

// resolveGroveDB resolves a *grove.DB from the DI container.
// If GroveDatabase is set, it looks up the named DB; otherwise it uses the default.
func (e *Extension) resolveGroveDB(fapp forge.App) (*grove.DB, error) {
	if e.config.GroveDatabase != "" {
		db, err := vessel.InjectNamed[*grove.DB](fapp.Container(), e.config.GroveDatabase)
		if err != nil {
			return nil, fmt.Errorf("grove database %q not found in container: %w", e.config.GroveDatabase, err)
		}
		return db, nil
	}
	db, err := vessel.Inject[*grove.DB](fapp.Container())
	if err != nil {
		return nil, fmt.Errorf("default grove database not found in container: %w", err)
	}
	return db, nil
}

// buildStoreFromGroveDB constructs the appropriate store backend
// based on the grove driver type (pg, sqlite, mongo).
func (e *Extension) buildStoreFromGroveDB(db *grove.DB) (store.Store, error) {
	driverName := db.Driver().Name()
	switch driverName {
	case "pg":
		return pgstore.New(db), nil
	case "sqlite":
		return sqlitestore.New(db), nil
	case "mongo":
		return mongostore.New(db), nil
	default:
		return nil, fmt.Errorf("nexus: unsupported grove driver %q", driverName)
	}
}
