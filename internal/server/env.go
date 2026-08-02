package server

import (
	"os"
	"strings"
)

// Environment variable names. Declared here rather than spelled at each read
// site, so `grep BLACKSTART_` finds every one of them.
const (
	EnvDatabaseDSN = "BLACKSTART_DATABASE_DSN"
	EnvRedisAddr   = "BLACKSTART_REDIS_ADDR"
	EnvFalkorAddr  = "BLACKSTART_FALKORDB_ADDR"
	EnvBlobDir     = "BLACKSTART_BLOB_DIR"
	EnvHTTPAddress = "BLACKSTART_HTTP_ADDRESS"
	EnvEnvironment = "BLACKSTART_ENV"
	EnvTenant      = "BLACKSTART_TENANT"
	EnvSolverAddr  = "BLACKSTART_SOLVER_ADDRESS"
	EnvSolverVer   = "BLACKSTART_SOLVER_VERSION"
)

// FromEnv fills a configuration from the environment, leaving anything unset
// at its existing value.
//
// The database DSN is the only secret here, and it is read from the
// environment rather than from a config file so that it never lands in the
// repository by accident. Blackstart runs on public data, but a database
// credential is still a credential.
func FromEnv(cfg Config) Config {
	cfg.DatabaseDSN = envOr(EnvDatabaseDSN, cfg.DatabaseDSN)
	cfg.RedisAddr = envOr(EnvRedisAddr, cfg.RedisAddr)
	cfg.FalkorDBAddr = envOr(EnvFalkorAddr, cfg.FalkorDBAddr)
	cfg.BlobDir = envOr(EnvBlobDir, cfg.BlobDir)
	cfg.HTTPAddress = envOr(EnvHTTPAddress, cfg.HTTPAddress)
	cfg.Environment = envOr(EnvEnvironment, cfg.Environment)
	cfg.Tenant = envOr(EnvTenant, cfg.Tenant)
	cfg.SolverAddress = envOr(EnvSolverAddr, cfg.SolverAddress)
	cfg.SolverVersion = envOr(EnvSolverVer, cfg.SolverVersion)

	return cfg
}

// Redact returns the configuration with the database password removed, for
// logging at startup.
//
// Startup log lines that echo configuration are genuinely useful and are also
// the most common way a connection string reaches a log aggregator. Redacting
// at the boundary is cheaper than remembering not to log it.
func (c Config) Redact() Config {
	c.DatabaseDSN = redactDSN(c.DatabaseDSN)

	return c
}

func redactDSN(dsn string) string {
	scheme := strings.Index(dsn, "://")
	at := strings.LastIndex(dsn, "@")

	if scheme < 0 || at < 0 || at < scheme {
		return dsn
	}

	return dsn[:scheme+3] + "***" + dsn[at:]
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}
