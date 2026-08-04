package server

import (
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/hazard"
	"github.com/xraph/blackstart/extensions/weather"
	"github.com/xraph/blackstart/extensions/weather/model"
)

func TestValidateRequiresADatabase(t *testing.T) {
	t.Parallel()

	if err := (Config{}).Validate(); err == nil {
		t.Error("accepted a configuration with no database DSN")
	}
}

// A worker with no Redis is a misconfiguration with no symptom until disk
// fills, so it is refused at startup rather than discovered later.
func TestValidateRequiresRedisWhenRunningTheWorker(t *testing.T) {
	t.Parallel()

	cfg := Config{DatabaseDSN: "postgres://localhost/blackstart", RunWorker: true}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("accepted a worker configuration with no Redis address")
	}

	if !strings.Contains(err.Error(), "outbox") {
		t.Errorf("the error should explain the consequence, got %q", err)
	}

	cfg.RedisAddr = "localhost:6379"
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// The CLI does not run the worker, so it does not need Redis. A short-lived
// command should not take over draining a shared queue and then exit
// mid-batch.
func TestTheCLIConfigurationNeedsNoRedis(t *testing.T) {
	t.Parallel()

	cfg := Config{DatabaseDSN: "postgres://localhost/blackstart", RunWorker: false}

	if err := cfg.Validate(); err != nil {
		t.Errorf("a non-worker configuration should not require Redis: %v", err)
	}
}

func TestFromEnvLeavesUnsetValuesAlone(t *testing.T) {
	t.Parallel()

	base := Config{
		DatabaseDSN: "postgres://original/db",
		Environment: "production",
	}

	got := FromEnv(base)

	if got.DatabaseDSN != base.DatabaseDSN {
		t.Errorf("an unset variable overwrote the DSN: %q", got.DatabaseDSN)
	}

	if got.Environment != "production" {
		t.Errorf("an unset variable overwrote the environment: %q", got.Environment)
	}
}

func TestFromEnvReadsEachVariable(t *testing.T) {
	t.Setenv(EnvDatabaseDSN, "postgres://env/db")
	t.Setenv(EnvRedisAddr, "redis:6379")
	t.Setenv(EnvFalkorAddr, "falkor:6379")
	t.Setenv(EnvHTTPAddress, ":9090")
	t.Setenv(EnvEnvironment, "staging")
	t.Setenv(EnvTenant, "other")

	got := FromEnv(Config{})

	for name, pair := range map[string][2]string{
		"DSN":         {got.DatabaseDSN, "postgres://env/db"},
		"Redis":       {got.RedisAddr, "redis:6379"},
		"FalkorDB":    {got.FalkorDBAddr, "falkor:6379"},
		"HTTP":        {got.HTTPAddress, ":9090"},
		"Environment": {got.Environment, "staging"},
		"Tenant":      {got.Tenant, "other"},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s = %q, want %q", name, pair[0], pair[1])
		}
	}
}

// Startup log lines that echo configuration are useful and are also the most
// common way a connection string reaches a log aggregator.
func TestRedactRemovesTheDatabasePassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "password is removed",
			dsn:  "postgres://user:hunter2@localhost:5432/blackstart",
			want: "postgres://***@localhost:5432/blackstart",
		},
		{
			name: "no credentials is left alone",
			dsn:  "postgres://localhost:5432/blackstart",
			want: "postgres://localhost:5432/blackstart",
		},
		{
			name: "malformed is left alone rather than mangled",
			dsn:  "not-a-dsn",
			want: "not-a-dsn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Config{DatabaseDSN: tt.dsn}.Redact()

			if got.DatabaseDSN != tt.want {
				t.Errorf("Redact() = %q, want %q", got.DatabaseDSN, tt.want)
			}

			if strings.Contains(got.DatabaseDSN, "hunter2") {
				t.Error("the password survived redaction")
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig("blackstart")

	if cfg.Name != "blackstart" {
		t.Errorf("Name = %q", cfg.Name)
	}

	if cfg.Tenant == "" {
		t.Error("the default configuration has no tenant; every Fabriq entry " +
			"point requires one")
	}
}

// The graph projection only advances when a worker drains the outbox, so
// enabling it without one would claim the system maintains something it does
// not.
func TestGraphProjectionRequiresBothAnEngineAndAWorker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		falkor string
		worker bool
		want   bool
	}{
		{"engine and worker", "falkor:6379", true, true},
		{"engine without a worker", "falkor:6379", false, false},
		{"worker without an engine", "", true, false},
		{"neither", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := fabricConfig(Config{FalkorDBAddr: tt.falkor, RunWorker: tt.worker})

			if got.Fabriq.Projections.Graph != tt.want {
				t.Errorf("graph projection = %v, want %v",
					got.Fabriq.Projections.Graph, tt.want)
			}
		})
	}
}

func TestBuildRejectsAnInvalidConfiguration(t *testing.T) {
	t.Parallel()

	if _, err := Build(Config{}); err == nil {
		t.Error("built an application with no database DSN")
	}
}

// The ingest loop needs somewhere to fetch from and a process that runs
// workers. Neither is a flag, because a flag could claim a loop was on in a
// process that would never tick it.
func TestWeatherPollIsEnabledOnlyWhenItCanRun(t *testing.T) {
	t.Parallel()

	full := Config{
		RunWorker:     true,
		WeatherSource: "noaa",
		WeatherFeeds:  map[string]string{"hrrr": "hrrr"},
	}

	for name, tt := range map[string]struct {
		cfg  Config
		want bool
	}{
		"source, feeds and a worker": {full, true},
		"no worker":                  {func() Config { c := full; c.RunWorker = false; return c }(), false},
		"no source":                  {func() Config { c := full; c.WeatherSource = ""; return c }(), false},
		"no feeds":                   {func() Config { c := full; c.WeatherFeeds = nil; return c }(), false},
	} {
		if got := weatherPollConfig(tt.cfg).Enabled; got != tt.want {
			t.Errorf("%s: enabled = %v, want %v", name, got, tt.want)
		}
	}
}

// A feed key naming no model is dropped rather than failing the load. The
// vocabulary is closed and the file is hand-edited, so a typo should cost that
// one model's ingest and not the whole process.
func TestWeatherFeedsDropsUnknownModels(t *testing.T) {
	t.Parallel()

	got := weatherPollConfig(Config{
		RunWorker:     true,
		WeatherSource: "noaa",
		WeatherFeeds:  map[string]string{"hrrr": "hrrr", "hrrrr": "typo", "gfs": ""},
	}).Feeds

	if len(got) != 1 {
		t.Fatalf("resolved %d feeds from one good key and two bad, want 1: %v", len(got), got)
	}

	if got[model.HRRR] != "hrrr" {
		t.Errorf("hrrr resolved to %q", got[model.HRRR])
	}
}

// Zero values fall back to the extension's own defaults rather than producing
// a loop that spins or sweeps nothing.
func TestScheduleZeroesFallBackToDefaults(t *testing.T) {
	t.Parallel()

	poll := weatherPollConfig(Config{RunWorker: true})
	if poll.Interval != weather.DefaultPollConfig().Interval ||
		poll.Backfill != weather.DefaultPollConfig().Backfill ||
		len(poll.Hours) == 0 {
		t.Errorf("weather fell back to %+v", poll)
	}

	compute := hazardComputeConfig(Config{RunWorker: true})
	if compute.Interval != hazard.DefaultComputeConfig().Interval ||
		compute.Lookback != hazard.DefaultComputeConfig().Lookback {
		t.Errorf("hazard fell back to %+v", compute)
	}
}

// The rating loop needs no registry slugs — it reads what weather stored — so
// running a worker is the whole condition.
func TestHazardComputeFollowsTheWorkerFlag(t *testing.T) {
	t.Parallel()

	if !hazardComputeConfig(Config{RunWorker: true}).Enabled {
		t.Error("a worker process did not enable the rating loop")
	}

	if hazardComputeConfig(Config{RunWorker: false}).Enabled {
		t.Error("a non-worker process enabled the rating loop")
	}
}
