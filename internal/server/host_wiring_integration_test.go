//go:build integration

package server_test

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/fabriq"

	"github.com/xraph/blackstart/apps/blackstart"
	"github.com/xraph/blackstart/extensions/fabric"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/risk/store"
	riskfabriq "github.com/xraph/blackstart/extensions/risk/store/fabriq"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/internal/server"
	"github.com/xraph/blackstart/internal/testsupport"
)

// TestRiskMigratesAndWritesThroughTheFullHost closes the two items task 7
// left open (progress.md, "R5: CARRY TO R8"): risk's migration group has
// never run alongside the other eleven extensions' — only on its own, in
// extensions/risk/store/fabriq/migrations_integration_test.go — and
// command.Command{Entity: ValueEntity} has never been resolved against the
// registry the host actually builds.
//
// Neither gap could be closed by risk's own package tests. A registry built
// from riskfabriq.Contributor{} alone (as risk's own integration tests do)
// cannot detect a name collision with a sibling extension's entity, and a
// migration group run by itself cannot detect one that only breaks in the
// presence of every other group's DDL. This test runs server.MigrationGroups
// — the exact list internal/server.Build hands the orchestrator, not a
// hand-copied one that could quietly drift from it — and opens Fabriq against
// entities.Build(blackstart.Contributors...), the exact registry both
// binaries construct, then performs one real write and read through it.
func TestRiskMigratesAndWritesThroughTheFullHost(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	// Every extension's migration group, run together through one
	// orchestrator — proving risk's group applies cleanly in the presence of
	// the other eleven, not merely in isolation.
	testsupport.Migrate(t, superDSN, server.MigrationGroups()...)

	appDSN := testsupport.AppDSN(t, superDSN)

	// The complete registry every extension in the host contributes to —
	// what apps/blackstart.New and apps/bsctl.App both build, not a
	// single-extension registry that trivially cannot collide with anything.
	reg, err := entities.Build(blackstart.Contributors...)
	if err != nil {
		t.Fatalf("building the full entity registry: %v", err)
	}

	fab, _, err := fabriq.Open(context.Background(), reg, fabriq.Config{
		Postgres: fabriq.PostgresConfig{DSN: appDSN},
	})
	if err != nil {
		t.Fatalf("opening fabriq against the full registry: %v", err)
	}

	defer func() { _ = fab.Close() }()

	riskStore, err := riskfabriq.New(fab, fabric.DefaultTenant)
	if err != nil {
		t.Fatalf("building the risk store: %v", err)
	}

	ctx := context.Background()
	span := gid.NewSpan()
	issuance := time.Date(2026, 8, 5, 6, 0, 0, 0, time.UTC)

	value := store.ComponentValue{
		ComponentID: span,
		Quantity:    store.QuantityConductorFailure,
		IssuedAt:    issuance,
		ValidAt:     issuance,
		Probability: 0.07,
		Scalar:      1,
	}

	// The write task 5 flagged as never resolved by the registry: PutBatch
	// issues command.Command{Entity: riskfabriq.ValueEntity}, and this is the
	// first time that command has run against a registry built the way the
	// host actually builds one.
	if err := riskStore.Values().PutBatch(ctx, []store.ComponentValue{value}); err != nil {
		t.Fatalf("PutBatch through the full host registry: %v", err)
	}

	got, err := riskStore.Values().ForComponent(ctx, span,
		[]store.Quantity{store.QuantityConductorFailure},
		issuance, issuance, issuance.Add(time.Hour))
	if err != nil {
		t.Fatalf("ForComponent: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("read %d rows back, want 1", len(got))
	}

	if got[0].Probability != value.Probability {
		t.Errorf("probability = %v, want %v", got[0].Probability, value.Probability)
	}
}
