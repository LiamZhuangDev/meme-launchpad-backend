package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/meme-launchpad/app-rebuild/internal/repository"
)

type fakeTokenReader struct {
	limit  int
	offset int
}

func (f *fakeTokenReader) List(_ context.Context, limit, offset int) ([]repository.Token, error) {
	f.limit, f.offset = limit, offset
	return []repository.Token{{ID: 1, Name: "Meme", Symbol: "MEME"}}, nil
}

func (f *fakeTokenReader) FindByAddress(context.Context, string) (repository.Token, error) {
	return repository.Token{}, nil
}

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	NewHandler("meme-launchpad-rebuild-api", nil, nil).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	want := "{\"service\":\"meme-launchpad-rebuild-api\",\"status\":\"ok\"}\n"
	if recorder.Body.String() != want {
		t.Fatalf("body = %q, want %q", recorder.Body.String(), want)
	}
}

func TestTokenListUsesPagination(t *testing.T) {
	tokens := &fakeTokenReader{}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/token/list?page=2&pageSize=5", nil)
	recorder := httptest.NewRecorder()

	NewHandler("test", nil, tokens).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if tokens.limit != 5 || tokens.offset != 5 {
		t.Fatalf("limit, offset = %d, %d; want 5, 5", tokens.limit, tokens.offset)
	}
	if !strings.Contains(recorder.Body.String(), `"symbol":"MEME"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}
