package grpcapi

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	tokencreationv1 "github.com/meme-launchpad/app-rebuild/gen/tokencreation/v1"
	"github.com/meme-launchpad/app-rebuild/internal/tokencreation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TokenCreator is the signed-intent capability consumed by the gRPC
// transport. The authenticated wallet, rather than client input, is always
// used as the creator.
type TokenCreator interface {
	Create(context.Context, tokencreation.Request) (tokencreation.Response, error)
}

type tokenCreationHandler struct {
	tokencreationv1.UnimplementedTokenCreationServiceServer
	auth    TokenParser
	creator TokenCreator
}

func registerTokenCreationService(server *grpc.Server, authenticator TokenParser, creator TokenCreator) {
	tokencreationv1.RegisterTokenCreationServiceServer(server, &tokenCreationHandler{auth: authenticator, creator: creator})
}

func (h *tokenCreationHandler) CreateToken(ctx context.Context, request *tokencreationv1.CreateTokenRequest) (*tokencreationv1.CreateTokenResponse, error) {
	claims, err := bearerClaims(ctx, h.auth)
	if err != nil {
		return nil, err
	}
	result, err := h.creator.Create(ctx, tokencreation.Request{
		Name:                 request.GetName(),
		Symbol:               request.GetSymbol(),
		Creator:              common.HexToAddress(claims.Address),
		LaunchTime:           request.GetLaunchTime(),
		InitialBuyPercentage: request.GetInitialBuyPercentage(),
	})
	if err != nil {
		// This mirrors the existing REST handler, which exposes creation request
		// validation failures as a client error.
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &tokencreationv1.CreateTokenResponse{
		CreateArg:        result.Data,
		Signature:        result.Signature,
		RequestId:        result.RequestID,
		Create2Salt:      result.Salt,
		PredictedAddress: result.PredictedAddress,
		Nonce:            result.Nonce,
		Timestamp:        result.Timestamp,
	}, nil
}
