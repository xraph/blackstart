package server

import (
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/xraph/blackstart/pkg/solver"
)

// provideSolver puts a solver client in the container.
//
// One connection for the whole application. gRPC connections multiplex, so a
// second one would buy nothing and would double the reconnection state to
// reason about — and two extensions talking to the same solver over different
// connections could see its cache differently after a restart.
//
// The connection is lazy: grpc.NewClient does not dial until the first call.
// That is what lets the solver be configured but not yet running, which is the
// normal case when the platform and the solver start together.
func provideSolver(app forge.App, address string) error {
	if address == "" {
		return nil
	}

	if err := vessel.Provide(app.Container(), func() (*solver.Client, error) {
		conn, err := grpc.NewClient(address, dialOptions()...)
		if err != nil {
			return nil, fmt.Errorf(
				"app: connecting to the solver at %s: %w", address, err)
		}

		return solver.New(conn), nil
	}); err != nil {
		return err
	}

	// The weather grid service shares the binary, so it shares the connection.
	// gRPC multiplexes by path, and a second dial to the same process would
	// double the reconnection state to reason about for nothing.
	return vessel.Provide(app.Container(), func() (*solver.GridClient, error) {
		conn, err := grpc.NewClient(address, dialOptions()...)
		if err != nil {
			return nil, fmt.Errorf(
				"app: connecting to the weather grid service at %s: %w", address, err)
		}

		return solver.NewGrid(conn), nil
	})
}

// dialOptions are how the platform talks to the solver.
//
// Insecure credentials, deliberately and with a boundary attached. The solver
// holds no credentials, enforces no authorization, and binds loopback by
// default: it is a component of this application rather than a service on a
// network. Running it anywhere that needs transport security means this
// function changes, and putting the choice in one named place is what makes
// that a decision rather than an oversight.
func dialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),

		// A 2000-bus model is comfortably over gRPC's 4 MiB default, and the
		// client chunks uploads for that reason. The receive limit is raised
		// anyway because a screen sweep's responses are not chunked and a
		// large network produces a lot of them.
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(64 << 20)),
	}
}
