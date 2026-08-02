package server_test

import (
	"bytes"
	"testing"

	"github.com/xraph/blackstart/internal/server"
)

// Both binaries must read the same layers. Two entry points reading two sets
// is how a CLI ends up writing to a different database than the server.
func TestLoadAppliesFilesThenEnvironment(t *testing.T) {
	t.Setenv(server.EnvTenant, "from-env")

	cfg, _, err := server.Load("blackstart")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Tenant != "from-env" {
		t.Errorf("Tenant = %q, want Load to apply the environment last", cfg.Tenant)
	}

	if cfg.Name != "blackstart" {
		t.Errorf("Name = %q, want the name Load was given", cfg.Name)
	}
}

// Nothing found means nothing printed. "No config file" is the normal
// container case and does not deserve a warning; the absence of the line is
// itself the signal that only defaults and environment variables are in play.
func TestReportConfigSourcesIsSilentWhenNothingWasLoaded(t *testing.T) {
	var buf bytes.Buffer

	server.ReportConfigSources(&buf, nil)

	if buf.Len() != 0 {
		t.Errorf("wrote %q, want nothing", buf.String())
	}
}

func TestReportConfigSourcesNamesEveryFile(t *testing.T) {
	var buf bytes.Buffer

	server.ReportConfigSources(&buf, []string{"/etc/a.yaml", "/etc/b.yaml"})

	out := buf.String()
	for _, want := range []string{"/etc/a.yaml", "/etc/b.yaml"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("output %q does not name %s", out, want)
		}
	}
}
