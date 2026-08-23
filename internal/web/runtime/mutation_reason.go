package runtime

import "context"

type nodeMutationReasonKey struct{}

const (
	MutationReasonQuotaBlock = "quota-block"
	MutationReasonQuotaReset = "quota-reset"
)

func WithNodeMutationReason(ctx context.Context, reason string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, nodeMutationReasonKey{}, reason)
}

func NodeMutationReason(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	reason, _ := ctx.Value(nodeMutationReasonKey{}).(string)
	return reason
}
