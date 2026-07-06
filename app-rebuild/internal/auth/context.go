package auth

import "context"

type bearerTokenContextKey struct{}

// WithBearerToken carries an already validated bearer token across an
// internal transport adapter. It is intentionally request-scoped.
func WithBearerToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, bearerTokenContextKey{}, token)
}

func BearerTokenFromContext(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(bearerTokenContextKey{}).(string)
	return token, ok && token != ""
}
