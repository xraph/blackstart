package server_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/blackstart/internal/server"
)

// write puts a config file in dir and returns dir, for chaining into FromFile.
func write(t *testing.T, dir, name, body string) string {
	t.Helper()

	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}

	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}

	return dir
}

func TestAConfigFileSetsEveryFieldItNames(t *testing.T) {
	dir := write(t, t.TempDir(), server.ConfigBaseName, `
app:
  environment: staging
server:
  address: ":9090"
database:
  dsn: "postgres://u:p@db:5432/blackstart"
extensions:
  fabric:
    tenant: acme
    redis:
      addr: "redis:6379"
    falkordb:
      addr: "falkor:6379"
`)

	cfg, loaded, err := server.FromFile(server.DefaultConfig("blackstart"), dir)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("loaded %v, want exactly the base file", loaded)
	}

	for _, tc := range []struct{ field, got, want string }{
		{"Environment", cfg.Environment, "staging"},
		{"HTTPAddress", cfg.HTTPAddress, ":9090"},
		{"DatabaseDSN", cfg.DatabaseDSN, "postgres://u:p@db:5432/blackstart"},
		{"Tenant", cfg.Tenant, "acme"},
		{"RedisAddr", cfg.RedisAddr, "redis:6379"},
		{"FalkorDBAddr", cfg.FalkorDBAddr, "falkor:6379"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.field, tc.got, tc.want)
		}
	}
}

// A file is a partial statement about configuration. One that mentions only
// the database must not blank the tenant that DefaultConfig set.
func TestAPartialFileLeavesUnmentionedFieldsAlone(t *testing.T) {
	dir := write(t, t.TempDir(), server.ConfigBaseName, "database:\n  dsn: \"postgres://x@y/z\"\n")

	base := server.DefaultConfig("blackstart")

	cfg, _, err := server.FromFile(base, dir)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}

	if cfg.Tenant != base.Tenant {
		t.Errorf("Tenant = %q, want the default %q — a file that never mentioned it", cfg.Tenant, base.Tenant)
	}

	if cfg.Environment != base.Environment {
		t.Errorf("Environment = %q, want the default %q", cfg.Environment, base.Environment)
	}
}

func TestTheLocalFileOverridesTheBaseFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, server.ConfigBaseName, "database:\n  dsn: \"base\"\napp:\n  environment: development\n")
	write(t, dir, server.ConfigLocalName, "database:\n  dsn: \"local\"\n")

	cfg, loaded, err := server.FromFile(server.DefaultConfig("blackstart"), dir)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("loaded %v, want both files", loaded)
	}

	if cfg.DatabaseDSN != "local" {
		t.Errorf("DatabaseDSN = %q, want the local file to win", cfg.DatabaseDSN)
	}

	// The local layer overrides, it does not replace: a key only the base file
	// sets must survive.
	if cfg.Environment != "development" {
		t.Errorf("Environment = %q, want the base file's value to survive", cfg.Environment)
	}
}

// The environment is the layer a deployment controls without rebuilding an
// image, so it has to beat both files. Load is the only place that ordering
// is expressed, which is why it is tested here rather than assumed.
func TestTheEnvironmentBeatsEveryFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, server.ConfigBaseName, "database:\n  dsn: \"from-base\"\n")
	write(t, dir, server.ConfigLocalName, "database:\n  dsn: \"from-local\"\n")

	t.Setenv(server.EnvDatabaseDSN, "from-env")

	cfg, _, err := server.FromFile(server.DefaultConfig("blackstart"), dir)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}

	if got := server.FromEnv(cfg).DatabaseDSN; got != "from-env" {
		t.Errorf("DatabaseDSN = %q, want the environment to win over both files", got)
	}
}

// Running on nothing but environment variables is what a container does. If a
// missing file were an error, the common deployment would be the awkward one.
func TestAMissingFileIsNotAnError(t *testing.T) {
	cfg, loaded, err := server.FromFile(server.DefaultConfig("blackstart"), t.TempDir())
	if err != nil {
		t.Fatalf("FromFile with no files present: %v", err)
	}

	if len(loaded) != 0 {
		t.Errorf("loaded %v, want nothing", loaded)
	}

	if cfg.Tenant != server.DefaultConfig("blackstart").Tenant {
		t.Error("the defaults did not survive an empty search path")
	}
}

// Malformed YAML must fail loudly. Silently falling back to defaults would
// start the process against the wrong database with no indication why.
func TestMalformedYAMLIsReported(t *testing.T) {
	dir := write(t, t.TempDir(), server.ConfigBaseName, "database:\n\tdsn: [unclosed\n")

	_, _, err := server.FromFile(server.DefaultConfig("blackstart"), dir)
	if err == nil {
		t.Fatal("FromFile accepted malformed YAML; it must not start on a guess")
	}
}

// The reason FromFile returns paths at all: every number this platform
// produces has to be traceable to what configured the process, and the config
// file is one of those inputs.
func TestTheLoadedPathsAreReportedForProvenance(t *testing.T) {
	dir := write(t, t.TempDir(), server.ConfigBaseName, "app:\n  environment: production\n")

	_, loaded, err := server.FromFile(server.DefaultConfig("blackstart"), dir)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}

	if len(loaded) != 1 || !strings.HasSuffix(loaded[0], server.ConfigBaseName) {
		t.Fatalf("loaded = %v, want the absolute path of the file that was read", loaded)
	}

	if !filepath.IsAbs(loaded[0]) {
		t.Errorf("loaded[0] = %q, want an absolute path — a relative one is ambiguous in a log", loaded[0])
	}
}

// Discovery is given configs/ as a search path and walks upward from it, which
// is the only reason a configs/ directory works at all: Forge's own discovery
// never descends into subdirectories.
func TestDiscoveryFindsAConfigsSubdirectoryAndTheRootAboveIt(t *testing.T) {
	root := t.TempDir()
	write(t, root, server.ConfigBaseName, "app:\n  environment: from-root\n")

	sub := write(t, filepath.Join(root, server.ConfigDir), server.ConfigLocalName,
		"database:\n  dsn: \"from-configs\"\n")

	cfg, loaded, err := server.FromFile(server.DefaultConfig("blackstart"), sub, root)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("loaded %v, want both the configs/ file and the root file", loaded)
	}

	if cfg.DatabaseDSN != "from-configs" {
		t.Errorf("DatabaseDSN = %q, want the configs/ file to have been read", cfg.DatabaseDSN)
	}

	if cfg.Environment != "from-root" {
		t.Errorf("Environment = %q, want the root file to have been read", cfg.Environment)
	}
}

// Confy stops climbing a hierarchy at the first level holding either kind of
// file, so a base file one level above a local file is never reached through
// that hierarchy. Requiring it anyway fails a startup that should have
// succeeded — which is what a naive existence check does.
func TestABaseFileAboveALocalFileDoesNotBreakStartup(t *testing.T) {
	root := t.TempDir()
	write(t, root, server.ConfigBaseName, "app:\n  environment: from-root\n")

	sub := write(t, filepath.Join(root, server.ConfigDir), server.ConfigLocalName,
		"database:\n  dsn: \"from-configs\"\n")

	// Only the deep path is given, so discovery never sees the root base file.
	cfg, loaded, err := server.FromFile(server.DefaultConfig("blackstart"), sub)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("loaded %v, want only the local file discovery can reach", loaded)
	}

	if cfg.DatabaseDSN != "from-configs" {
		t.Errorf("DatabaseDSN = %q, want the local file's value", cfg.DatabaseDSN)
	}
}

// The solver is optional, and the config has to be able to say so. A host with
// no solver address registers no solver-backed extensions, which is what
// `bsctl entities` and `blackstart migrate` want.
func TestTheSolverIsConfigurableAndOptional(t *testing.T) {
	dir := write(t, t.TempDir(), server.ConfigBaseName,
		"solver:\n  address: \"solver:50051\"\n  version: \"1.2.3\"\n")

	cfg, _, err := server.FromFile(server.DefaultConfig("blackstart"), dir)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}

	if cfg.SolverAddress != "solver:50051" {
		t.Errorf("SolverAddress = %q", cfg.SolverAddress)
	}

	if cfg.SolverVersion != "1.2.3" {
		t.Errorf("SolverVersion = %q", cfg.SolverVersion)
	}

	// And a file that never mentions the solver leaves it unset rather than
	// inventing a default address that would be dialed and fail.
	bare := write(t, t.TempDir(), server.ConfigBaseName, "app:\n  environment: test\n")

	empty, _, err := server.FromFile(server.DefaultConfig("blackstart"), bare)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}

	if empty.SolverAddress != "" {
		t.Errorf("a config with no solver section produced address %q", empty.SolverAddress)
	}
}
