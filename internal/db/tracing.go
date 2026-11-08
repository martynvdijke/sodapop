package db

import (
	"context"
	"strings"

	"entgo.io/ent/dialect"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	tracerName = "sodapop/internal/db"
	// maxStatementAttr bounds the recorded SQL statement. Statements contain
	// only placeholders, never bound values, but long IN-lists can still be
	// large.
	maxStatementAttr = 512
)

// TraceQueryEx wraps a database operation in a client span. Queries are
// summarized to bounded, parameter-free text (args are never recorded).
func TraceQueryEx(ctx context.Context, operation, query string, fn func(context.Context) error) error {
	// Resolve the tracer per call so a provider installed after Init is used.
	ctx, span := otel.Tracer(tracerName).Start(ctx, "db."+operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "sqlite"),
			attribute.String("db.operation", operation),
			attribute.String("db.statement", summarizeQuery(query)),
		),
	)
	defer span.End()

	if err := fn(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

// summarizeQuery collapses whitespace and truncates the statement to a bounded
// number of bytes.
func summarizeQuery(query string) string {
	summarized := strings.Join(strings.Fields(query), " ")
	if len(summarized) > maxStatementAttr {
		summarized = summarized[:maxStatementAttr]
	}
	return summarized
}

// tracingDriver wraps an Ent driver so every Exec/Query emits a span.
type tracingDriver struct {
	dialect.Driver
}

func (d *tracingDriver) Exec(ctx context.Context, query string, args, v any) error {
	return TraceQueryEx(ctx, "exec", query, func(ctx context.Context) error {
		return d.Driver.Exec(ctx, query, args, v)
	})
}

func (d *tracingDriver) Query(ctx context.Context, query string, args, v any) error {
	return TraceQueryEx(ctx, "query", query, func(ctx context.Context) error {
		return d.Driver.Query(ctx, query, args, v)
	})
}

func (d *tracingDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	// The transaction span covers acquisition only; Commit/Rollback delegate
	// through the embedded transaction without an open parent span.
	_, span := otel.Tracer(tracerName).Start(ctx, "db.tx",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "sqlite"),
			attribute.String("db.operation", "tx"),
		),
	)

	tx, err := d.Driver.Tx(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return nil, err
	}
	span.End()
	return &tracingTx{Tx: tx}, nil
}

// tracingTx wraps an Ent transaction so its statements are traced as well.
type tracingTx struct {
	dialect.Tx
}

func (t *tracingTx) Exec(ctx context.Context, query string, args, v any) error {
	return TraceQueryEx(ctx, "exec", query, func(ctx context.Context) error {
		return t.Tx.Exec(ctx, query, args, v)
	})
}

func (t *tracingTx) Query(ctx context.Context, query string, args, v any) error {
	return TraceQueryEx(ctx, "query", query, func(ctx context.Context) error {
		return t.Tx.Query(ctx, query, args, v)
	})
}
