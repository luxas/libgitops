package frame

import (
	"context"

	"github.com/weaveworks/libgitops/pkg/tracing"
	"go.opentelemetry.io/otel/trace"
)

func closeWithTrace(ctx context.Context, c Closer, obj interface{}) tracing.TraceFuncResult {
	return tracing.FromContext(ctx, obj).TraceFunc(ctx, "Close", func(ctx context.Context, _ trace.Span) error {
		return c.Close(ctx)
	})
}
