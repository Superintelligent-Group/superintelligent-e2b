// Code generated by ent, DO NOT EDIT.

package env

import (
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqlgraph"
	"github.com/e2b-dev/infra/packages/shared/pkg/models/internal"
	"github.com/e2b-dev/infra/packages/shared/pkg/models/predicate"
	"github.com/google/uuid"
)

// ID filters vertices based on their ID field.
func ID(id string) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldID, id))
}

// IDEQ applies the EQ predicate on the ID field.
func IDEQ(id string) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldID, id))
}

// IDNEQ applies the NEQ predicate on the ID field.
func IDNEQ(id string) predicate.Env {
	return predicate.Env(sql.FieldNEQ(FieldID, id))
}

// IDIn applies the In predicate on the ID field.
func IDIn(ids ...string) predicate.Env {
	return predicate.Env(sql.FieldIn(FieldID, ids...))
}

// IDNotIn applies the NotIn predicate on the ID field.
func IDNotIn(ids ...string) predicate.Env {
	return predicate.Env(sql.FieldNotIn(FieldID, ids...))
}

// IDGT applies the GT predicate on the ID field.
func IDGT(id string) predicate.Env {
	return predicate.Env(sql.FieldGT(FieldID, id))
}

// IDGTE applies the GTE predicate on the ID field.
func IDGTE(id string) predicate.Env {
	return predicate.Env(sql.FieldGTE(FieldID, id))
}

// IDLT applies the LT predicate on the ID field.
func IDLT(id string) predicate.Env {
	return predicate.Env(sql.FieldLT(FieldID, id))
}

// IDLTE applies the LTE predicate on the ID field.
func IDLTE(id string) predicate.Env {
	return predicate.Env(sql.FieldLTE(FieldID, id))
}

// IDEqualFold applies the EqualFold predicate on the ID field.
func IDEqualFold(id string) predicate.Env {
	return predicate.Env(sql.FieldEqualFold(FieldID, id))
}

// IDContainsFold applies the ContainsFold predicate on the ID field.
func IDContainsFold(id string) predicate.Env {
	return predicate.Env(sql.FieldContainsFold(FieldID, id))
}

// CreatedAt applies equality check predicate on the "created_at" field. It's identical to CreatedAtEQ.
func CreatedAt(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldCreatedAt, v))
}

// UpdatedAt applies equality check predicate on the "updated_at" field. It's identical to UpdatedAtEQ.
func UpdatedAt(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldUpdatedAt, v))
}

// TeamID applies equality check predicate on the "team_id" field. It's identical to TeamIDEQ.
func TeamID(v uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldTeamID, v))
}

// CreatedBy applies equality check predicate on the "created_by" field. It's identical to CreatedByEQ.
func CreatedBy(v uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldCreatedBy, v))
}

// Public applies equality check predicate on the "public" field. It's identical to PublicEQ.
func Public(v bool) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldPublic, v))
}

// BuildCount applies equality check predicate on the "build_count" field. It's identical to BuildCountEQ.
func BuildCount(v int32) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldBuildCount, v))
}

// SpawnCount applies equality check predicate on the "spawn_count" field. It's identical to SpawnCountEQ.
func SpawnCount(v int64) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldSpawnCount, v))
}

// LastSpawnedAt applies equality check predicate on the "last_spawned_at" field. It's identical to LastSpawnedAtEQ.
func LastSpawnedAt(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldLastSpawnedAt, v))
}

// ClusterID applies equality check predicate on the "cluster_id" field. It's identical to ClusterIDEQ.
func ClusterID(v uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldClusterID, v))
}

// CreatedAtEQ applies the EQ predicate on the "created_at" field.
func CreatedAtEQ(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldCreatedAt, v))
}

// CreatedAtNEQ applies the NEQ predicate on the "created_at" field.
func CreatedAtNEQ(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldNEQ(FieldCreatedAt, v))
}

// CreatedAtIn applies the In predicate on the "created_at" field.
func CreatedAtIn(vs ...time.Time) predicate.Env {
	return predicate.Env(sql.FieldIn(FieldCreatedAt, vs...))
}

// CreatedAtNotIn applies the NotIn predicate on the "created_at" field.
func CreatedAtNotIn(vs ...time.Time) predicate.Env {
	return predicate.Env(sql.FieldNotIn(FieldCreatedAt, vs...))
}

// CreatedAtGT applies the GT predicate on the "created_at" field.
func CreatedAtGT(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldGT(FieldCreatedAt, v))
}

// CreatedAtGTE applies the GTE predicate on the "created_at" field.
func CreatedAtGTE(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldGTE(FieldCreatedAt, v))
}

// CreatedAtLT applies the LT predicate on the "created_at" field.
func CreatedAtLT(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldLT(FieldCreatedAt, v))
}

// CreatedAtLTE applies the LTE predicate on the "created_at" field.
func CreatedAtLTE(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldLTE(FieldCreatedAt, v))
}

// UpdatedAtEQ applies the EQ predicate on the "updated_at" field.
func UpdatedAtEQ(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldUpdatedAt, v))
}

// UpdatedAtNEQ applies the NEQ predicate on the "updated_at" field.
func UpdatedAtNEQ(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldNEQ(FieldUpdatedAt, v))
}

// UpdatedAtIn applies the In predicate on the "updated_at" field.
func UpdatedAtIn(vs ...time.Time) predicate.Env {
	return predicate.Env(sql.FieldIn(FieldUpdatedAt, vs...))
}

// UpdatedAtNotIn applies the NotIn predicate on the "updated_at" field.
func UpdatedAtNotIn(vs ...time.Time) predicate.Env {
	return predicate.Env(sql.FieldNotIn(FieldUpdatedAt, vs...))
}

// UpdatedAtGT applies the GT predicate on the "updated_at" field.
func UpdatedAtGT(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldGT(FieldUpdatedAt, v))
}

// UpdatedAtGTE applies the GTE predicate on the "updated_at" field.
func UpdatedAtGTE(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldGTE(FieldUpdatedAt, v))
}

// UpdatedAtLT applies the LT predicate on the "updated_at" field.
func UpdatedAtLT(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldLT(FieldUpdatedAt, v))
}

// UpdatedAtLTE applies the LTE predicate on the "updated_at" field.
func UpdatedAtLTE(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldLTE(FieldUpdatedAt, v))
}

// TeamIDEQ applies the EQ predicate on the "team_id" field.
func TeamIDEQ(v uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldTeamID, v))
}

// TeamIDNEQ applies the NEQ predicate on the "team_id" field.
func TeamIDNEQ(v uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldNEQ(FieldTeamID, v))
}

// TeamIDIn applies the In predicate on the "team_id" field.
func TeamIDIn(vs ...uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldIn(FieldTeamID, vs...))
}

// TeamIDNotIn applies the NotIn predicate on the "team_id" field.
func TeamIDNotIn(vs ...uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldNotIn(FieldTeamID, vs...))
}

// CreatedByEQ applies the EQ predicate on the "created_by" field.
func CreatedByEQ(v uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldCreatedBy, v))
}

// CreatedByNEQ applies the NEQ predicate on the "created_by" field.
func CreatedByNEQ(v uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldNEQ(FieldCreatedBy, v))
}

// CreatedByIn applies the In predicate on the "created_by" field.
func CreatedByIn(vs ...uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldIn(FieldCreatedBy, vs...))
}

// CreatedByNotIn applies the NotIn predicate on the "created_by" field.
func CreatedByNotIn(vs ...uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldNotIn(FieldCreatedBy, vs...))
}

// CreatedByIsNil applies the IsNil predicate on the "created_by" field.
func CreatedByIsNil() predicate.Env {
	return predicate.Env(sql.FieldIsNull(FieldCreatedBy))
}

// CreatedByNotNil applies the NotNil predicate on the "created_by" field.
func CreatedByNotNil() predicate.Env {
	return predicate.Env(sql.FieldNotNull(FieldCreatedBy))
}

// PublicEQ applies the EQ predicate on the "public" field.
func PublicEQ(v bool) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldPublic, v))
}

// PublicNEQ applies the NEQ predicate on the "public" field.
func PublicNEQ(v bool) predicate.Env {
	return predicate.Env(sql.FieldNEQ(FieldPublic, v))
}

// BuildCountEQ applies the EQ predicate on the "build_count" field.
func BuildCountEQ(v int32) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldBuildCount, v))
}

// BuildCountNEQ applies the NEQ predicate on the "build_count" field.
func BuildCountNEQ(v int32) predicate.Env {
	return predicate.Env(sql.FieldNEQ(FieldBuildCount, v))
}

// BuildCountIn applies the In predicate on the "build_count" field.
func BuildCountIn(vs ...int32) predicate.Env {
	return predicate.Env(sql.FieldIn(FieldBuildCount, vs...))
}

// BuildCountNotIn applies the NotIn predicate on the "build_count" field.
func BuildCountNotIn(vs ...int32) predicate.Env {
	return predicate.Env(sql.FieldNotIn(FieldBuildCount, vs...))
}

// BuildCountGT applies the GT predicate on the "build_count" field.
func BuildCountGT(v int32) predicate.Env {
	return predicate.Env(sql.FieldGT(FieldBuildCount, v))
}

// BuildCountGTE applies the GTE predicate on the "build_count" field.
func BuildCountGTE(v int32) predicate.Env {
	return predicate.Env(sql.FieldGTE(FieldBuildCount, v))
}

// BuildCountLT applies the LT predicate on the "build_count" field.
func BuildCountLT(v int32) predicate.Env {
	return predicate.Env(sql.FieldLT(FieldBuildCount, v))
}

// BuildCountLTE applies the LTE predicate on the "build_count" field.
func BuildCountLTE(v int32) predicate.Env {
	return predicate.Env(sql.FieldLTE(FieldBuildCount, v))
}

// SpawnCountEQ applies the EQ predicate on the "spawn_count" field.
func SpawnCountEQ(v int64) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldSpawnCount, v))
}

// SpawnCountNEQ applies the NEQ predicate on the "spawn_count" field.
func SpawnCountNEQ(v int64) predicate.Env {
	return predicate.Env(sql.FieldNEQ(FieldSpawnCount, v))
}

// SpawnCountIn applies the In predicate on the "spawn_count" field.
func SpawnCountIn(vs ...int64) predicate.Env {
	return predicate.Env(sql.FieldIn(FieldSpawnCount, vs...))
}

// SpawnCountNotIn applies the NotIn predicate on the "spawn_count" field.
func SpawnCountNotIn(vs ...int64) predicate.Env {
	return predicate.Env(sql.FieldNotIn(FieldSpawnCount, vs...))
}

// SpawnCountGT applies the GT predicate on the "spawn_count" field.
func SpawnCountGT(v int64) predicate.Env {
	return predicate.Env(sql.FieldGT(FieldSpawnCount, v))
}

// SpawnCountGTE applies the GTE predicate on the "spawn_count" field.
func SpawnCountGTE(v int64) predicate.Env {
	return predicate.Env(sql.FieldGTE(FieldSpawnCount, v))
}

// SpawnCountLT applies the LT predicate on the "spawn_count" field.
func SpawnCountLT(v int64) predicate.Env {
	return predicate.Env(sql.FieldLT(FieldSpawnCount, v))
}

// SpawnCountLTE applies the LTE predicate on the "spawn_count" field.
func SpawnCountLTE(v int64) predicate.Env {
	return predicate.Env(sql.FieldLTE(FieldSpawnCount, v))
}

// LastSpawnedAtEQ applies the EQ predicate on the "last_spawned_at" field.
func LastSpawnedAtEQ(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldLastSpawnedAt, v))
}

// LastSpawnedAtNEQ applies the NEQ predicate on the "last_spawned_at" field.
func LastSpawnedAtNEQ(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldNEQ(FieldLastSpawnedAt, v))
}

// LastSpawnedAtIn applies the In predicate on the "last_spawned_at" field.
func LastSpawnedAtIn(vs ...time.Time) predicate.Env {
	return predicate.Env(sql.FieldIn(FieldLastSpawnedAt, vs...))
}

// LastSpawnedAtNotIn applies the NotIn predicate on the "last_spawned_at" field.
func LastSpawnedAtNotIn(vs ...time.Time) predicate.Env {
	return predicate.Env(sql.FieldNotIn(FieldLastSpawnedAt, vs...))
}

// LastSpawnedAtGT applies the GT predicate on the "last_spawned_at" field.
func LastSpawnedAtGT(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldGT(FieldLastSpawnedAt, v))
}

// LastSpawnedAtGTE applies the GTE predicate on the "last_spawned_at" field.
func LastSpawnedAtGTE(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldGTE(FieldLastSpawnedAt, v))
}

// LastSpawnedAtLT applies the LT predicate on the "last_spawned_at" field.
func LastSpawnedAtLT(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldLT(FieldLastSpawnedAt, v))
}

// LastSpawnedAtLTE applies the LTE predicate on the "last_spawned_at" field.
func LastSpawnedAtLTE(v time.Time) predicate.Env {
	return predicate.Env(sql.FieldLTE(FieldLastSpawnedAt, v))
}

// LastSpawnedAtIsNil applies the IsNil predicate on the "last_spawned_at" field.
func LastSpawnedAtIsNil() predicate.Env {
	return predicate.Env(sql.FieldIsNull(FieldLastSpawnedAt))
}

// LastSpawnedAtNotNil applies the NotNil predicate on the "last_spawned_at" field.
func LastSpawnedAtNotNil() predicate.Env {
	return predicate.Env(sql.FieldNotNull(FieldLastSpawnedAt))
}

// ClusterIDEQ applies the EQ predicate on the "cluster_id" field.
func ClusterIDEQ(v uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldEQ(FieldClusterID, v))
}

// ClusterIDNEQ applies the NEQ predicate on the "cluster_id" field.
func ClusterIDNEQ(v uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldNEQ(FieldClusterID, v))
}

// ClusterIDIn applies the In predicate on the "cluster_id" field.
func ClusterIDIn(vs ...uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldIn(FieldClusterID, vs...))
}

// ClusterIDNotIn applies the NotIn predicate on the "cluster_id" field.
func ClusterIDNotIn(vs ...uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldNotIn(FieldClusterID, vs...))
}

// ClusterIDGT applies the GT predicate on the "cluster_id" field.
func ClusterIDGT(v uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldGT(FieldClusterID, v))
}

// ClusterIDGTE applies the GTE predicate on the "cluster_id" field.
func ClusterIDGTE(v uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldGTE(FieldClusterID, v))
}

// ClusterIDLT applies the LT predicate on the "cluster_id" field.
func ClusterIDLT(v uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldLT(FieldClusterID, v))
}

// ClusterIDLTE applies the LTE predicate on the "cluster_id" field.
func ClusterIDLTE(v uuid.UUID) predicate.Env {
	return predicate.Env(sql.FieldLTE(FieldClusterID, v))
}

// ClusterIDIsNil applies the IsNil predicate on the "cluster_id" field.
func ClusterIDIsNil() predicate.Env {
	return predicate.Env(sql.FieldIsNull(FieldClusterID))
}

// ClusterIDNotNil applies the NotNil predicate on the "cluster_id" field.
func ClusterIDNotNil() predicate.Env {
	return predicate.Env(sql.FieldNotNull(FieldClusterID))
}

// HasTeam applies the HasEdge predicate on the "team" edge.
func HasTeam() predicate.Env {
	return predicate.Env(func(s *sql.Selector) {
		step := sqlgraph.NewStep(
			sqlgraph.From(Table, FieldID),
			sqlgraph.Edge(sqlgraph.M2O, true, TeamTable, TeamColumn),
		)
		schemaConfig := internal.SchemaConfigFromContext(s.Context())
		step.To.Schema = schemaConfig.Team
		step.Edge.Schema = schemaConfig.Env
		sqlgraph.HasNeighbors(s, step)
	})
}

// HasTeamWith applies the HasEdge predicate on the "team" edge with a given conditions (other predicates).
func HasTeamWith(preds ...predicate.Team) predicate.Env {
	return predicate.Env(func(s *sql.Selector) {
		step := newTeamStep()
		schemaConfig := internal.SchemaConfigFromContext(s.Context())
		step.To.Schema = schemaConfig.Team
		step.Edge.Schema = schemaConfig.Env
		sqlgraph.HasNeighborsWith(s, step, func(s *sql.Selector) {
			for _, p := range preds {
				p(s)
			}
		})
	})
}

// HasCreator applies the HasEdge predicate on the "creator" edge.
func HasCreator() predicate.Env {
	return predicate.Env(func(s *sql.Selector) {
		step := sqlgraph.NewStep(
			sqlgraph.From(Table, FieldID),
			sqlgraph.Edge(sqlgraph.M2O, true, CreatorTable, CreatorColumn),
		)
		schemaConfig := internal.SchemaConfigFromContext(s.Context())
		step.To.Schema = schemaConfig.User
		step.Edge.Schema = schemaConfig.Env
		sqlgraph.HasNeighbors(s, step)
	})
}

// HasCreatorWith applies the HasEdge predicate on the "creator" edge with a given conditions (other predicates).
func HasCreatorWith(preds ...predicate.User) predicate.Env {
	return predicate.Env(func(s *sql.Selector) {
		step := newCreatorStep()
		schemaConfig := internal.SchemaConfigFromContext(s.Context())
		step.To.Schema = schemaConfig.User
		step.Edge.Schema = schemaConfig.Env
		sqlgraph.HasNeighborsWith(s, step, func(s *sql.Selector) {
			for _, p := range preds {
				p(s)
			}
		})
	})
}

// HasEnvAliases applies the HasEdge predicate on the "env_aliases" edge.
func HasEnvAliases() predicate.Env {
	return predicate.Env(func(s *sql.Selector) {
		step := sqlgraph.NewStep(
			sqlgraph.From(Table, FieldID),
			sqlgraph.Edge(sqlgraph.O2M, false, EnvAliasesTable, EnvAliasesColumn),
		)
		schemaConfig := internal.SchemaConfigFromContext(s.Context())
		step.To.Schema = schemaConfig.EnvAlias
		step.Edge.Schema = schemaConfig.EnvAlias
		sqlgraph.HasNeighbors(s, step)
	})
}

// HasEnvAliasesWith applies the HasEdge predicate on the "env_aliases" edge with a given conditions (other predicates).
func HasEnvAliasesWith(preds ...predicate.EnvAlias) predicate.Env {
	return predicate.Env(func(s *sql.Selector) {
		step := newEnvAliasesStep()
		schemaConfig := internal.SchemaConfigFromContext(s.Context())
		step.To.Schema = schemaConfig.EnvAlias
		step.Edge.Schema = schemaConfig.EnvAlias
		sqlgraph.HasNeighborsWith(s, step, func(s *sql.Selector) {
			for _, p := range preds {
				p(s)
			}
		})
	})
}

// HasBuilds applies the HasEdge predicate on the "builds" edge.
func HasBuilds() predicate.Env {
	return predicate.Env(func(s *sql.Selector) {
		step := sqlgraph.NewStep(
			sqlgraph.From(Table, FieldID),
			sqlgraph.Edge(sqlgraph.O2M, false, BuildsTable, BuildsColumn),
		)
		schemaConfig := internal.SchemaConfigFromContext(s.Context())
		step.To.Schema = schemaConfig.EnvBuild
		step.Edge.Schema = schemaConfig.EnvBuild
		sqlgraph.HasNeighbors(s, step)
	})
}

// HasBuildsWith applies the HasEdge predicate on the "builds" edge with a given conditions (other predicates).
func HasBuildsWith(preds ...predicate.EnvBuild) predicate.Env {
	return predicate.Env(func(s *sql.Selector) {
		step := newBuildsStep()
		schemaConfig := internal.SchemaConfigFromContext(s.Context())
		step.To.Schema = schemaConfig.EnvBuild
		step.Edge.Schema = schemaConfig.EnvBuild
		sqlgraph.HasNeighborsWith(s, step, func(s *sql.Selector) {
			for _, p := range preds {
				p(s)
			}
		})
	})
}

// HasSnapshots applies the HasEdge predicate on the "snapshots" edge.
func HasSnapshots() predicate.Env {
	return predicate.Env(func(s *sql.Selector) {
		step := sqlgraph.NewStep(
			sqlgraph.From(Table, FieldID),
			sqlgraph.Edge(sqlgraph.O2M, false, SnapshotsTable, SnapshotsColumn),
		)
		schemaConfig := internal.SchemaConfigFromContext(s.Context())
		step.To.Schema = schemaConfig.Snapshot
		step.Edge.Schema = schemaConfig.Snapshot
		sqlgraph.HasNeighbors(s, step)
	})
}

// HasSnapshotsWith applies the HasEdge predicate on the "snapshots" edge with a given conditions (other predicates).
func HasSnapshotsWith(preds ...predicate.Snapshot) predicate.Env {
	return predicate.Env(func(s *sql.Selector) {
		step := newSnapshotsStep()
		schemaConfig := internal.SchemaConfigFromContext(s.Context())
		step.To.Schema = schemaConfig.Snapshot
		step.Edge.Schema = schemaConfig.Snapshot
		sqlgraph.HasNeighborsWith(s, step, func(s *sql.Selector) {
			for _, p := range preds {
				p(s)
			}
		})
	})
}

// And groups predicates with the AND operator between them.
func And(predicates ...predicate.Env) predicate.Env {
	return predicate.Env(sql.AndPredicates(predicates...))
}

// Or groups predicates with the OR operator between them.
func Or(predicates ...predicate.Env) predicate.Env {
	return predicate.Env(sql.OrPredicates(predicates...))
}

// Not applies the not operator on the given predicate.
func Not(p predicate.Env) predicate.Env {
	return predicate.Env(sql.NotPredicates(p))
}
