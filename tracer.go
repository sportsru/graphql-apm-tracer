package tracer

import (
	"context"
	"fmt"

	"github.com/graph-gophers/graphql-go/errors"
	"github.com/graph-gophers/graphql-go/introspection"
	"github.com/graph-gophers/graphql-go/trace"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/opentracing/opentracing-go/log"
	"go.elastic.co/apm"
)

func NewTracer() *Tracer {
	return &Tracer{}
}

type Tracer struct {
}

func (Tracer) TraceQuery(ctx context.Context, queryString string, operationName string, variables map[string]interface{}, varTypes map[string]*introspection.Type) (context.Context, trace.TraceQueryFinishFunc) {

	span, spanCtx := opentracing.StartSpanFromContext(ctx, "GraphQL request")
	span.SetTag("graphql.query", queryString)

	if operationName != "" {
		span.SetTag("graphql.operationName", operationName)
	}

	if len(variables) != 0 {
		span.LogFields(log.Object("graphql.variables", variables))
	}

	return spanCtx, func(errs []*errors.QueryError) {
		if len(errs) > 0 {
			msg := errs[0].Error()
			if len(errs) > 1 {
				msg += fmt.Sprintf(" (and %d more errors)", len(errs)-1)
			}
			ext.Error.Set(span, true)
			span.Context.SetTag("graphql.error", msg)
		}
		span.End()
	}
}

func (Tracer) TraceField(ctx context.Context, label, typeName, fieldName string, trivial bool, args map[string]interface{}) (context.Context, trace.TraceFieldFinishFunc) {

	var tx *apm.Transaction
	var span *apm.Span

	if trivial {
		return ctx, noop
	}

	if typeName == "Query" || typeName == "Mutation" || typeName == "Subscription" {
		opts := apm.TransactionOptions{TraceContext: apm.TransactionFromContext(ctx).TraceContext()}
		tx = apm.DefaultTracer.StartTransactionOptions(typeName+"."+fieldName, "graphql operation", opts)
		ctx = apm.ContextWithTransaction(ctx, tx)
	} else {
		span, ctx = apm.StartSpan(ctx, label, "graphql field")
		span.Context.SetTag("graphql.type", typeName)
		span.Context.SetTag("graphql.field", fieldName)
		for name, value := range args {
			span.Context.SetTag("graphql.args."+name, fmt.Sprint(value))
		}
	}

	return ctx, func(err *errors.QueryError) {
		if err != nil {
			if tx != nil {
				tx.Result = err.Message
			}
		}

		if tx != nil {
			tx.End()
		}
		if span != nil {
			span.End()
		}
	}
}

func noop(*errors.QueryError) {}

type NoopTracer struct{}

func (NoopTracer) TraceQuery(ctx context.Context, queryString string, operationName string, variables map[string]interface{}, varTypes map[string]*introspection.Type) (context.Context, trace.TraceQueryFinishFunc) {
	return ctx, func(errs []*errors.QueryError) {}
}

func (NoopTracer) TraceField(ctx context.Context, label, typeName, fieldName string, trivial bool, args map[string]interface{}) (context.Context, trace.TraceFieldFinishFunc) {
	return ctx, func(err *errors.QueryError) {}
}
