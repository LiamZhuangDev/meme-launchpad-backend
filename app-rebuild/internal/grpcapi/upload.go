package grpcapi

import (
	"context"
	"time"

	uploadv1 "github.com/meme-launchpad/app-rebuild/gen/upload/v1"
	"github.com/meme-launchpad/app-rebuild/internal/upload"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type UploadPresigner interface {
	Presign(context.Context, string, string, int) (upload.PresignResult, error)
	Confirm(context.Context) error
}

type uploadHandler struct {
	uploadv1.UnimplementedUploadServiceServer
	auth    TokenParser
	uploads UploadPresigner
}

func registerUploadService(server *grpc.Server, authenticator TokenParser, uploads UploadPresigner) {
	uploadv1.RegisterUploadServiceServer(server, &uploadHandler{auth: authenticator, uploads: uploads})
}

func (h *uploadHandler) PresignImage(ctx context.Context, request *uploadv1.PresignImageRequest) (*uploadv1.PresignImageResponse, error) {
	if _, err := bearerClaims(ctx, h.auth); err != nil {
		return nil, err
	}
	folder, err := imageFolder(request.GetKind())
	if err != nil {
		return nil, err
	}
	mimeType := request.GetMimeType()
	if mimeType == "" {
		mimeType = "image/png"
	}
	chainID := int32(97)
	if request.ChainId != nil {
		chainID = request.GetChainId()
		if chainID < 1 {
			return nil, status.Error(codes.InvalidArgument, "chain_id must be positive")
		}
	}
	result, err := h.uploads.Presign(ctx, folder, mimeType, int(chainID))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &uploadv1.PresignImageResponse{
		UploadUrl: result.UploadURL,
		PublicUrl: result.PublicURL,
		FileName:  result.FileName,
		Key:       result.Key,
		ExpiresAt: timestamppb.New(time.Unix(result.Expires, 0)),
	}, nil
}

func (h *uploadHandler) ConfirmUpload(ctx context.Context, _ *uploadv1.ConfirmUploadRequest) (*uploadv1.ConfirmUploadResponse, error) {
	if _, err := bearerClaims(ctx, h.auth); err != nil {
		return nil, err
	}
	if err := h.uploads.Confirm(ctx); err != nil {
		return nil, status.Error(codes.Internal, "failed to confirm upload")
	}
	return &uploadv1.ConfirmUploadResponse{Ok: true}, nil
}

func imageFolder(kind uploadv1.ImageKind) (string, error) {
	switch kind {
	case uploadv1.ImageKind_IMAGE_KIND_TOKEN_LOGO:
		return "token-logo", nil
	case uploadv1.ImageKind_IMAGE_KIND_TOKEN_BANNER:
		return "token-banner", nil
	case uploadv1.ImageKind_IMAGE_KIND_ACTIVITY_IMAGE:
		return "activity-image", nil
	default:
		return "", status.Error(codes.InvalidArgument, "image kind is required")
	}
}
