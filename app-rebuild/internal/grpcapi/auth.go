package grpcapi

import (
	"context"
	"errors"
	"strings"
	"time"

	authv1 "github.com/meme-launchpad/app-rebuild/gen/auth/v1"
	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Authenticator is the authentication capability consumed by the gRPC
// transport. auth.Service also remains independently available to REST.
type Authenticator interface {
	TokenParser
	RequestMessage(context.Context, string) (auth.SignMessage, error)
	Login(context.Context, string, string) (auth.LoginResult, error)
}

type TokenParser interface {
	ParseToken(string) (auth.Claims, error)
}

type authHandler struct {
	authv1.UnimplementedAuthServiceServer
	auth Authenticator
}

func registerAuthService(server *grpc.Server, authenticator Authenticator) {
	authv1.RegisterAuthServiceServer(server, &authHandler{auth: authenticator})
}

func (h *authHandler) RequestSignMessage(ctx context.Context, request *authv1.RequestSignMessageRequest) (*authv1.RequestSignMessageResponse, error) {
	result, err := h.auth.RequestMessage(ctx, request.GetAddress())
	if err != nil {
		return nil, authStatus(err)
	}
	return &authv1.RequestSignMessageResponse{
		Message:   result.Message,
		Nonce:     result.Nonce,
		ExpiresAt: timestamppb.New(time.Unix(result.Expires, 0)),
	}, nil
}

func (h *authHandler) WalletLogin(ctx context.Context, request *authv1.WalletLoginRequest) (*authv1.WalletLoginResponse, error) {
	result, err := h.auth.Login(ctx, request.GetAddress(), request.GetSignature())
	if err != nil {
		return nil, authStatus(err)
	}
	return &authv1.WalletLoginResponse{
		Token:     result.Token,
		User:      userMessage(result.User),
		ExpiresAt: timestamppb.New(time.Unix(result.Expires, 0)),
	}, nil
}

func (h *authHandler) GetCurrentUser(ctx context.Context, _ *authv1.GetCurrentUserRequest) (*authv1.GetCurrentUserResponse, error) {
	claims, err := bearerClaims(ctx, h.auth)
	if err != nil {
		return nil, err
	}
	response := &authv1.GetCurrentUserResponse{UserId: claims.UserID, Address: claims.Address}
	if claims.IssuedAt != nil {
		response.IssuedAt = timestamppb.New(claims.IssuedAt.Time)
	}
	if claims.ExpiresAt != nil {
		response.ExpiresAt = timestamppb.New(claims.ExpiresAt.Time)
	}
	return response, nil
}

func bearerClaims(ctx context.Context, authenticator TokenParser) (auth.Claims, error) {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) == 0 {
		return auth.Claims{}, status.Error(codes.Unauthenticated, "missing bearer token")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(values[0], prefix) || len(values[0]) == len(prefix) {
		return auth.Claims{}, status.Error(codes.Unauthenticated, "invalid bearer token")
	}
	claims, err := authenticator.ParseToken(strings.TrimPrefix(values[0], prefix))
	if err != nil {
		return auth.Claims{}, status.Error(codes.Unauthenticated, err.Error())
	}
	return claims, nil
}

func authStatus(err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalidAddress):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, auth.ErrInvalidSignature), errors.Is(err, auth.ErrInvalidNonce), errors.Is(err, auth.ErrInvalidSIWE):
		return status.Error(codes.Unauthenticated, err.Error())
	default:
		return status.Error(codes.Internal, "authentication failed")
	}
}

func userMessage(user repository.User) *authv1.User {
	return &authv1.User{
		Id:        user.ID,
		Address:   user.Address,
		Username:  user.Username,
		CreatedAt: timestamppb.New(user.CreatedAt),
		UpdatedAt: timestamppb.New(user.UpdatedAt),
	}
}
