package server

import (
	"github.com/xraph/forge"

	"github.com/xraph/blackstart/pkg/buildinfo"
)

// routerOptions describes the HTTP surface to Forge's OpenAPI generator.
//
// Worth having rather than boilerplate: every controller in this platform
// already declares its request and response schemas through
// forge.WithRequestSchema and forge.WithResponseSchema, so the specification
// is generated from the same declarations the router enforces. A hand-written
// spec would be a second description of the API that drifts from the first,
// and the one that drifts is always the one nothing validates against.
func routerOptions(cfg Config) []forge.RouterOption {
	return []forge.RouterOption{
		forge.WithOpenAPI(forge.OpenAPIConfig{
			Title: cfg.Name + " API",
			Description: "Weather-driven outage risk and cascading failure " +
				"analysis for electric power systems.\n\n" +
				"**Advisory only.** Nothing in this API commands anything. " +
				"There is no control endpoint, no write path to any SCADA, EMS " +
				"or ADMS, and no protocol client here can issue one — that is " +
				"an architectural boundary rather than a phase-one limitation, " +
				"and CI checks it on every commit. See docs/SAFETY.md.\n\n" +
				"Every number this API returns names the network it was " +
				"computed from by content hash, the solver version that " +
				"produced it, and the dataset version behind that. A figure " +
				"you cannot trace to those three things is a bug.",
			// The build's version, not a literal. A specification that claims
			// a version the binary does not have is worse than one with no
			// version at all: it looks authoritative and is wrong.
			Version: buildinfo.Get().Version,

			Servers: []forge.OpenAPIServer{
				{
					URL:         "http://localhost" + cfg.HTTPAddress,
					Description: "This instance",
				},
			},

			// Declared even though nothing enforces them yet. The schemes name
			// what a deployment is expected to put in front of this, and a
			// generated client built against a spec with no security block
			// would have no way to send a credential at all.
			Security: map[string]forge.SecurityScheme{
				"bearerAuth": {
					Type:         "http",
					Scheme:       "bearer",
					BearerFormat: "JWT",
					Description: "JWT bearer token. Issued by the identity " +
						"service when one is configured; absent in a " +
						"single-tenant deployment, which is the default.",
				},
				"apiKey": {
					Type:        "apiKey",
					In:          "header",
					Name:        "X-API-Key",
					Description: "Service-to-service API key.",
				},
			},

			ExternalDocs: &forge.ExternalDocs{
				Description: "Architecture decisions and the safety boundary",
				URL:         "https://github.com/xraph/blackstart/tree/main/docs",
			},

			License: &forge.License{
				Name: "Apache-2.0",
				URL:  "https://www.apache.org/licenses/LICENSE-2.0.html",
			},

			UIPath:      "/docs",
			SpecPath:    "/openapi.json",
			UIEnabled:   true,
			SpecEnabled: true,
			PrettyJSON:  true,
		}),

		// The event surface. Domain events already travel through pkg/hooks
		// and the Fabriq outbox, and describing them here is what lets a
		// consumer of the stream be generated rather than hand-written.
		forge.WithAsyncAPI(forge.AsyncAPIConfig{
			Title:       cfg.Name + " events",
			Version:     buildinfo.Get().Version,
			SpecEnabled: true,
			UIEnabled:   true,
			SpecPath:    "/asyncapi.json",
			UIPath:      "/asyncapi",
		}),
	}
}
