package tracer

import (
	"context"
	"fmt"

	"github.com/tribunadigital/graphql-go/errors"
	"github.com/tribunadigital/graphql-go/introspection"
	"github.com/tribunadigital/graphql-go/trace"
	"go.elastic.co/apm"
)

func NewTracer() *Tracer {
	return &Tracer{}
}

type Tracer struct {
}

func (Tracer) TraceQuery(ctx context.Context, queryString string, operationName string, variables map[string]interface{}, varTypes map[string]*introspection.Type) (context.Context, trace.TraceQueryFinishFunc) {

	name := "graphql request"
	if operationName != "" {
		name = operationName
	}

	span, spanCtx := apm.StartSpan(ctx, name, "graphql request")
	span.Context.SetTag("graphql.query", queryString)

	if len(variables) != 0 {
		span.Context.SetLabel("graphql.variables", variables)
	}

	return spanCtx, func(errs []*errors.QueryError) {
		if len(errs) > 0 {
			msg := errs[0].Error()
			if len(errs) > 1 {
				msg += fmt.Sprintf(" (and %d more errors)", len(errs)-1)
			}
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
			span.Context.SetLabel("graphql.args."+name, value)
		}
	}

	return ctx, func(err *errors.QueryError) {
		if err != nil {
			apmErr := apm.DefaultTracer.NewError(err)
			apmErr.Context.SetLabel("path", err.Path)
			apmErr.Context.SetLabel("extensions", err.Extensions)
			if tx != nil {
				tx.Result = err.Message
				apmErr.SetTransaction(tx)
			}

			if span != nil {
				apmErr.SetSpan(span)
			}

			apmErr.Send()
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
