// Package fabriq is the Fabriq-backed implementation of the provenance store.
//
// Models here are unexported and per-backend, with explicit mappers to and
// from the domain types. The domain structs carry no grove tags: keeping the
// persistence shape separate is what lets the column layout change without
// touching the type the rest of the platform passes around.
//
// Append-only, and not bitemporal. A provenance record states what happened;
// correcting one would mean claiming a different thing happened, which is
// falsification rather than correction. There is no Update and no Delete here,
// and reads go through Base.Query rather than Base.QueryAsOf — whose own
// documentation names this extension as the case it does not apply to.
package fabriq

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xraph/grove"

	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/pkg/id"
)

// Table names. The prefix is the extension name, matching the house
// convention and keeping a shared schema legible.
const (
	DatasetTable = "blackstart_provenance_dataset"
	ModelTable   = "blackstart_provenance_model"
	RunTable     = "blackstart_provenance_run"
	LineageTable = "blackstart_provenance_lineage"
)

// Entity names as registered with Fabriq. These are what a command.Command
// carries, and they are global to the registry.
const (
	DatasetEntity = "provenance_dataset"
	ModelEntity   = "provenance_model"
	RunEntity     = "provenance_run"
	LineageEntity = "provenance_lineage"
)

// datasourceSourceEntity is the registry name of the datasource extension's
// source, named as a literal rather than imported.
//
// Importing the constant would make datasource a build-time dependency of this
// package for the sake of one string, and the registry name is a published
// contract between extensions in any case: the registry is global, so this
// name is already shared whether or not the import exists.
const datasourceSourceEntity = "datasource_source"

// datasetModel is the persistence shape of one retrieval.
//
// The first three fields are Fabriq's structural contract: every entity must
// declare id, tenant_id and version, and the registry refuses a model that
// does not. They are stamped by the executor, so caller values are ignored.
type datasetModel struct {
	grove.BaseModel `grove:"table:blackstart_provenance_dataset"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	// SourceID is the column the graph projection follows, tying a retrieval
	// back to a license and a publisher rather than to a bare URL.
	//
	// Untagged rather than notnull, and the distinction is not cosmetic:
	// Fabriq reads that tag as "required, and an empty value is a validation
	// failure", not as the column constraint of the same name. The column is
	// NOT NULL DEFAULT '' all the same. It has to be optional because
	// DatasetVersion.Validate does not require a source, and an empty value is
	// what tells Fabriq's projector to write no edge rather than a dangling
	// one.
	SourceID string `grove:"source_id"`

	URL           string    `grove:"url,notnull"`
	ContentHash   string    `grove:"content_hash,notnull"`
	SizeBytes     int64     `grove:"size_bytes,notnull"`
	ParserVersion string    `grove:"parser_version,notnull"`
	RetrievedAt   time.Time `grove:"retrieved_at,notnull"`
}

// modelModel is the persistence shape of one producer version.
type modelModel struct {
	grove.BaseModel `grove:"table:blackstart_provenance_model"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	Kind string `grove:"kind,notnull"`
	Name string `grove:"name,notnull"`

	// ProducerVersion is the producer's own version string, and it cannot be
	// called `version`: that column is Fabriq's structural one, a BIGINT the
	// executor stamps. A model mapping the string onto it would silently fight
	// the version stamping on every write.
	ProducerVersion string `grove:"producer_version,notnull"`

	CodeCommit string `grove:"code_commit,notnull"`

	// Parameters are JSON in the column, as contingency stores its candidates.
	// A child table would mean a join to answer "what settings produced this",
	// which is never asked without the version it belongs to.
	Parameters []byte `grove:"parameters,notnull"`

	RecordedAt time.Time `grove:"recorded_at,notnull"`
}

// runModel is the persistence shape of one execution.
type runModel struct {
	grove.BaseModel `grove:"table:blackstart_provenance_run"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	// ModelVersionID is both a foreign key and the column the COMPUTED_BY edge
	// follows.
	ModelVersionID string `grove:"model_version_id,notnull"`

	Kind string `grove:"kind,notnull"`

	// Both untagged, for the same reason SourceID is: Fabriq reads notnull as
	// "an empty value is a validation failure". Run.Validate accepts a run
	// with neither — a result hash is optional, and an empty seed is how a
	// deterministic computation records that it had none, since a zero seed is
	// a real seed.
	ResultHash string `grove:"result_hash"`
	Seed       string `grove:"seed"`

	StartedAt time.Time `grove:"started_at,notnull"`

	// FinishedAt is nullzero: Run.Validate permits a zero FinishedAt, and
	// writing a zero time.Time into a NOT NULL column stores a year-1
	// timestamp that reads as data rather than as absence.
	FinishedAt time.Time `grove:"finished_at,nullzero"`
}

// lineageModel is the persistence shape of one reified edge.
type lineageModel struct {
	grove.BaseModel `grove:"table:blackstart_provenance_lineage"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	RunID            string `grove:"run_id,notnull"`
	DatasetVersionID string `grove:"dataset_version_id,notnull"`

	Role string `grove:"role,notnull"`

	// Relation is the graph edge type, derived from Role at write time so the
	// two cannot disagree. Fabriq's GraphEdgeSpec reads the relationship type
	// from a column value, and the domain's roles are lower-case words that
	// would make poor Cypher.
	Relation string `grove:"relation,notnull"`

	RecordedAt time.Time `grove:"recorded_at,notnull"`
}

// relationFor maps a lineage role onto its graph relationship type.
//
// The three stay distinct in the graph for the same reason they are distinct
// in the domain: a backtest that reads training data has leaked, and one edge
// type for all three would hide it behind a traversal that looks clean.
func relationFor(role record.Role) string {
	switch role {
	case record.RoleTraining:
		return "TRAINED_ON"

	case record.RoleValidation:
		return "VALIDATED_ON"

	case record.RoleInput:
		return "USED"

	default:
		return "USED"
	}
}

func datasetToModel(in record.DatasetVersion) (*datasetModel, error) {
	return &datasetModel{
		ID: in.ID.String(),
		// The zero identifier stringifies to "", which is what the projector
		// reads as "no edge".
		SourceID:      in.SourceID.String(),
		URL:           in.URL,
		ContentHash:   in.ContentHash,
		SizeBytes:     in.SizeBytes,
		ParserVersion: in.ParserVersion,
		// Stored in UTC. A timestamptz round trip preserves the instant but
		// not the zone, and a retrieval time compared against one read back in
		// another zone is a comparison nobody can reason about.
		RetrievedAt: in.RetrievedAt.UTC(),
	}, nil
}

func datasetFromModel(m datasetModel) (record.DatasetVersion, error) {
	parsed, err := id.Parse(m.ID)
	if err != nil {
		return record.DatasetVersion{}, fmt.Errorf(
			"provenance: stored dataset id %q: %w", m.ID, err)
	}

	out := record.DatasetVersion{
		ID:            parsed,
		URL:           m.URL,
		ContentHash:   m.ContentHash,
		SizeBytes:     m.SizeBytes,
		ParserVersion: m.ParserVersion,
		RetrievedAt:   m.RetrievedAt.UTC(),
	}

	// Optional, so an empty column is absence rather than a parse failure.
	if m.SourceID != "" {
		source, err := id.Parse(m.SourceID)
		if err != nil {
			return record.DatasetVersion{}, fmt.Errorf(
				"provenance: stored source id %q on dataset %s: %w", m.SourceID, m.ID, err)
		}

		out.SourceID = source
	}

	return out, nil
}

func modelToModel(in record.ModelVersion) (*modelModel, error) {
	// Marshaled even when nil, so the column holds `{}` rather than SQL NULL
	// and a reader never has to distinguish "no parameters" from "not
	// recorded" — for a producer version they are the same thing.
	params := in.Parameters
	if params == nil {
		params = map[string]string{}
	}

	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf(
			"provenance: encoding parameters of model version %s: %w", in.ID, err)
	}

	return &modelModel{
		ID:              in.ID.String(),
		Kind:            string(in.Kind),
		Name:            in.Name,
		ProducerVersion: in.Version,
		CodeCommit:      in.CodeCommit,
		Parameters:      encoded,
		RecordedAt:      in.RecordedAt.UTC(),
	}, nil
}

func modelFromModel(m modelModel) (record.ModelVersion, error) {
	parsed, err := id.Parse(m.ID)
	if err != nil {
		return record.ModelVersion{}, fmt.Errorf(
			"provenance: stored model version id %q: %w", m.ID, err)
	}

	out := record.ModelVersion{
		ID:         parsed,
		Kind:       record.ModelKind(m.Kind),
		Name:       m.Name,
		Version:    m.ProducerVersion,
		CodeCommit: m.CodeCommit,
		RecordedAt: m.RecordedAt.UTC(),
	}

	if len(m.Parameters) > 0 {
		var params map[string]string

		if err := json.Unmarshal(m.Parameters, &params); err != nil {
			return record.ModelVersion{}, fmt.Errorf(
				"provenance: decoding parameters of model version %s: %w", m.ID, err)
		}

		// An empty object decodes to an empty map; the domain treats absent
		// and empty alike, so it is normalised to nil rather than kept as a
		// map that compares unequal to the one that went in.
		if len(params) > 0 {
			out.Parameters = params
		}
	}

	return out, nil
}

func runToModel(in record.ProvenanceRun) *runModel {
	return &runModel{
		ID:             in.ID.String(),
		ModelVersionID: in.ModelVersionID.String(),
		Kind:           in.Kind,
		ResultHash:     in.ResultHash,
		Seed:           in.Seed,
		StartedAt:      in.StartedAt.UTC(),
		FinishedAt:     in.FinishedAt.UTC(),
	}
}

func runFromModel(m runModel) (record.ProvenanceRun, error) {
	parsed, err := id.Parse(m.ID)
	if err != nil {
		return record.ProvenanceRun{}, fmt.Errorf("provenance: stored run id %q: %w", m.ID, err)
	}

	producer, err := id.Parse(m.ModelVersionID)
	if err != nil {
		return record.ProvenanceRun{}, fmt.Errorf(
			"provenance: stored model version id %q on run %s: %w", m.ModelVersionID, m.ID, err)
	}

	out := record.ProvenanceRun{
		ID:             parsed,
		ModelVersionID: producer,
		Kind:           m.Kind,
		ResultHash:     m.ResultHash,
		Seed:           m.Seed,
		StartedAt:      m.StartedAt.UTC(),
	}

	// A NULL finished_at scans as the zero time, and .UTC() on a zero time
	// yields a value that no longer reports itself as zero — so it is only
	// converted when there is something to convert.
	if !m.FinishedAt.IsZero() {
		out.FinishedAt = m.FinishedAt.UTC()
	}

	return out, nil
}

func lineageToModel(in record.Lineage) *lineageModel {
	return &lineageModel{
		ID:               in.ID.String(),
		RunID:            in.RunID.String(),
		DatasetVersionID: in.DatasetVersionID.String(),
		Role:             string(in.Role),
		Relation:         relationFor(in.Role),
		RecordedAt:       in.RecordedAt.UTC(),
	}
}

func lineageFromModel(m lineageModel) (record.Lineage, error) {
	parsed, err := id.Parse(m.ID)
	if err != nil {
		return record.Lineage{}, fmt.Errorf("provenance: stored lineage id %q: %w", m.ID, err)
	}

	run, err := id.Parse(m.RunID)
	if err != nil {
		return record.Lineage{}, fmt.Errorf(
			"provenance: stored run id %q on lineage %s: %w", m.RunID, m.ID, err)
	}

	dataset, err := id.Parse(m.DatasetVersionID)
	if err != nil {
		return record.Lineage{}, fmt.Errorf(
			"provenance: stored dataset id %q on lineage %s: %w", m.DatasetVersionID, m.ID, err)
	}

	return record.Lineage{
		ID:               parsed,
		RunID:            run,
		DatasetVersionID: dataset,
		Role:             record.Role(m.Role),
		RecordedAt:       m.RecordedAt.UTC(),
	}, nil
}
