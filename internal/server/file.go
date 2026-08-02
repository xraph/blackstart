package server

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/xraph/confy"
)

// ConfigDir is the directory a project-local configuration lives in.
//
// Forge's own discovery walks *upward* from each search path looking for
// config.yaml — it never descends into a subdirectory — so a configs/ folder
// only works if it is named as a search path. That is what SearchPaths below
// does, and it is why both configs/config.yaml and a repository-root
// config.yaml are found.
const ConfigDir = "configs"

// Config file names, in the two-layer shape Forge uses everywhere: a committed
// base file carrying everything safe to publish, and a gitignored local file
// carrying the connection strings.
const (
	ConfigBaseName  = "config.yaml"
	ConfigLocalName = "config.local.yaml"
)

// fileConfig mirrors the parts of the configuration a file may set.
//
// It is a separate type from Config on purpose. Config is what the application
// needs; this is what a YAML document is allowed to say, and the two drift for
// good reasons — RunWorker is a property of which binary is running, not
// something an operator should be able to switch on in a shared file.
type fileConfig struct {
	App struct {
		Environment string `yaml:"environment"`
	} `yaml:"app"`

	Server struct {
		Address string `yaml:"address"`
	} `yaml:"server"`

	// Grove reads database.driver and database.dsn from this same key, so the
	// name is fixed by convention rather than chosen here.
	Database struct {
		DSN string `yaml:"dsn"`
	} `yaml:"database"`

	Solver struct {
		Address string `yaml:"address"`
		Version string `yaml:"version"`
	} `yaml:"solver"`

	Extensions struct {
		Fabric struct {
			Tenant string `yaml:"tenant"`

			Redis struct {
				Addr string `yaml:"addr"`
			} `yaml:"redis"`

			FalkorDB struct {
				Addr string `yaml:"addr"`
			} `yaml:"falkordb"`

			Storage struct {
				Driver string `yaml:"driver"`
			} `yaml:"storage"`
		} `yaml:"fabric"`
	} `yaml:"extensions"`
}

// FromFile layers configuration files over cfg and reports which files were
// read.
//
// The paths come back rather than being logged here because every number this
// platform produces has to be traceable to the inputs that produced it, and
// "which config file was in effect" is one of those inputs. A caller that
// discards them is choosing not to be able to answer that question.
//
// A missing file is not an error. Running with nothing but environment
// variables is a legitimate deployment — it is what a container does — and
// failing on a missing file would make the common case the awkward one.
//
// Precedence is base file, then local file, then environment. Environment
// wins because it is the layer a deployment controls without rebuilding an
// image, and because the DSN belongs there.
func FromFile(cfg Config, searchPaths ...string) (Config, []string, error) {
	if len(searchPaths) == 0 {
		var err error
		if searchPaths, err = defaultSearchPaths(); err != nil {
			return cfg, nil, err
		}
	}

	// Which files exist is settled here rather than left to Confy, because the
	// two Require flags below are the only thing that makes a parse error
	// visible. With them false, DiscoverAndLoadConfigs discards the error from
	// LoadFrom and returns a manager holding nothing — so a corrupt
	// config.yaml would start the process on defaults, silently, against
	// whatever database the defaults name. Setting each flag from what was
	// actually found keeps a missing file harmless and an unreadable one loud.
	baseFound, localFound := presence(searchPaths)

	if !baseFound && !localFound {
		return cfg, nil, nil
	}

	manager, result, err := confy.DiscoverAndLoadConfigs(confy.AutoDiscoveryConfig{
		AppName:          cfg.Name,
		SearchPaths:      searchPaths,
		ConfigNames:      []string{ConfigBaseName, "config.yml"},
		LocalConfigNames: []string{ConfigLocalName, "config.local.yml"},
		MaxDepth:         defaultSearchDepth,
		RequireBase:      baseFound,
		RequireLocal:     localFound,
		// Blackstart reads its own BLACKSTART_-prefixed variables in FromEnv,
		// with every name declared in one place. Letting Confy synthesize a
		// second, differently-spelled set from the same struct would give two
		// env mechanisms that disagree silently — the worst kind of
		// configuration bug, because both look right in isolation.
		EnableEnvSource: false,
	})
	if err != nil {
		return cfg, nil, fmt.Errorf("app: loading configuration: %w", err)
	}

	loaded := append(append([]string{}, result.BaseConfigPaths...), result.LocalConfigPaths...)
	if len(loaded) == 0 {
		return cfg, nil, nil
	}

	var file fileConfig
	if err := manager.Bind("", &file); err != nil {
		return cfg, loaded, fmt.Errorf("app: reading %v: %w", loaded, err)
	}

	return file.apply(cfg), loaded, nil
}

// defaultSearchDepth bounds how far discovery walks up from each search path.
//
// Two is deliberate: it reaches the repository root from configs/, and from a
// subdirectory it reaches the level above. Forge's own default of five will
// happily walk out of the project and load a stranger's config.yaml from a
// parent directory, which is a confusing way to spend an afternoon.
const defaultSearchDepth = 2

// ConfigSearchPaths are the directories searched for configuration, in
// precedence order. Exported so the Forge application can be pointed at the
// same set — two discoveries reading different directories would mean the file
// an operator edits configures only half the process.
func ConfigSearchPaths() ([]string, error) {
	return defaultSearchPaths()
}

func defaultSearchPaths() ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("app: locating the working directory: %w", err)
	}

	// configs/ first so a project-local file wins over one that happens to sit
	// beside the binary.
	return []string{filepath.Join(cwd, ConfigDir), cwd}, nil
}

// apply overlays the file's values, leaving anything it does not set alone.
//
// Every field is guarded by a non-empty check so that a file which mentions
// only the database does not silently blank the tenant. A YAML document is a
// partial statement about configuration, not a replacement for it.
func (f fileConfig) apply(cfg Config) Config {
	cfg.Environment = orKeep(f.App.Environment, cfg.Environment)
	cfg.HTTPAddress = orKeep(f.Server.Address, cfg.HTTPAddress)
	cfg.DatabaseDSN = orKeep(f.Database.DSN, cfg.DatabaseDSN)
	cfg.Tenant = orKeep(f.Extensions.Fabric.Tenant, cfg.Tenant)
	cfg.RedisAddr = orKeep(f.Extensions.Fabric.Redis.Addr, cfg.RedisAddr)
	cfg.FalkorDBAddr = orKeep(f.Extensions.Fabric.FalkorDB.Addr, cfg.FalkorDBAddr)
	cfg.BlobDir = orKeep(f.Extensions.Fabric.Storage.Driver, cfg.BlobDir)
	cfg.SolverAddress = orKeep(f.Solver.Address, cfg.SolverAddress)
	cfg.SolverVersion = orKeep(f.Solver.Version, cfg.SolverVersion)

	return cfg
}

// presence reports which kinds of config file discovery will actually find.
//
// It has to mirror Confy's walk exactly, including the part that is easy to
// miss: each hierarchy stops at the first level holding *either* kind of file.
// A base file one level above a local file is therefore never reached through
// that hierarchy, and claiming it is required would fail a startup that would
// otherwise have been fine. Disagreeing the other way restores the silent
// fallback this whole check exists to prevent, so neither direction is
// tolerable and the walk is duplicated rather than approximated.
func presence(searchPaths []string) (base, local bool) {
	for _, dir := range searchPaths {
		current := filepath.Clean(dir)

		for range defaultSearchDepth {
			foundBase := existsIn(current, ConfigBaseName, "config.yml")
			foundLocal := existsIn(current, ConfigLocalName, "config.local.yml")

			base = base || foundBase
			local = local || foundLocal

			// Confy stops climbing this hierarchy as soon as a level yields
			// anything at all.
			if foundBase || foundLocal {
				break
			}

			parent := filepath.Dir(current)
			if parent == current {
				break
			}

			current = parent
		}
	}

	return base, local
}

func existsIn(dir string, names ...string) bool {
	for _, name := range names {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return true
		}
	}

	return false
}

func orKeep(value, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}
