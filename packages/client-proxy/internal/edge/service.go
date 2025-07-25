package edge

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/e2b-dev/infra/packages/proxy/internal/edge/handlers"
	e2binfo "github.com/e2b-dev/infra/packages/proxy/internal/edge/info"
	e2borchestrators "github.com/e2b-dev/infra/packages/proxy/internal/edge/pool"
	"github.com/e2b-dev/infra/packages/proxy/internal/edge/sandboxes"
)

func NewEdgeAPIStore(
	ctx context.Context,
	logger *zap.Logger,
	tracer trace.Tracer,
	info *e2binfo.ServiceInfo,
	edges *e2borchestrators.EdgePool,
	orchestrators *e2borchestrators.OrchestratorsPool,
	catalog sandboxes.SandboxesCatalog,
) (*handlers.APIStore, error) {
	store, err := handlers.NewStore(ctx, logger, tracer, info, orchestrators, edges, catalog)
	if err != nil {
		return nil, err
	}

	return store, nil
}
