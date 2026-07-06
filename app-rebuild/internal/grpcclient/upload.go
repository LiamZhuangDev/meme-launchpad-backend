package grpcclient

import (
	"context"
	"fmt"
	"math"

	uploadv1 "github.com/meme-launchpad/app-rebuild/gen/upload/v1"
	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"github.com/meme-launchpad/app-rebuild/internal/upload"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// UploadService adapts the internal UploadService client to the upload
// interface consumed by the REST transport.
type UploadService struct {
	client uploadv1.UploadServiceClient
}

func NewUploadService(connection grpc.ClientConnInterface) *UploadService {
	return &UploadService{client: uploadv1.NewUploadServiceClient(connection)}
}

func (s *UploadService) Presign(ctx context.Context, folder, mimeType string, chainID int) (upload.PresignResult, error) {
	token, ok := auth.BearerTokenFromContext(ctx)
	if !ok {
		return upload.PresignResult{}, fmt.Errorf("bearer token is required for internal upload presigning")
	}
	kind, err := imageKind(folder)
	if err != nil {
		return upload.PresignResult{}, err
	}
	if chainID < 1 || chainID > math.MaxInt32 {
		return upload.PresignResult{}, fmt.Errorf("chain ID exceeds gRPC range")
	}
	value := int32(chainID)
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	response, err := s.client.PresignImage(ctx, &uploadv1.PresignImageRequest{
		Kind: kind, MimeType: mimeType, ChainId: &value,
	})
	if err != nil {
		return upload.PresignResult{}, fmt.Errorf("presign upload over internal gRPC: %w", err)
	}

	expires := int64(0)
	if timestamp := response.GetExpiresAt(); timestamp != nil {
		if err := timestamp.CheckValid(); err != nil {
			return upload.PresignResult{}, fmt.Errorf("internal gRPC returned an invalid upload expiry: %w", err)
		}
		expires = timestamp.AsTime().Unix()
	}
	return upload.PresignResult{
		UploadURL: response.GetUploadUrl(),
		PublicURL: response.GetPublicUrl(),
		FileName:  response.GetFileName(),
		Key:       response.GetKey(),
		Expires:   expires,
	}, nil
}

func (s *UploadService) Confirm(ctx context.Context) error {
	token, ok := auth.BearerTokenFromContext(ctx)
	if !ok {
		return fmt.Errorf("bearer token is required for internal upload confirmation")
	}
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	response, err := s.client.ConfirmUpload(ctx, &uploadv1.ConfirmUploadRequest{})
	if err != nil {
		return fmt.Errorf("confirm upload over internal gRPC: %w", err)
	}
	if !response.GetOk() {
		return fmt.Errorf("internal upload confirmation was rejected")
	}
	return nil
}

func imageKind(folder string) (uploadv1.ImageKind, error) {
	switch folder {
	case "token-logo":
		return uploadv1.ImageKind_IMAGE_KIND_TOKEN_LOGO, nil
	case "token-banner":
		return uploadv1.ImageKind_IMAGE_KIND_TOKEN_BANNER, nil
	case "activity-image":
		return uploadv1.ImageKind_IMAGE_KIND_ACTIVITY_IMAGE, nil
	default:
		return uploadv1.ImageKind_IMAGE_KIND_UNSPECIFIED, fmt.Errorf("unsupported upload folder %q", folder)
	}
}
