// Package app wires configuration and runtime dependencies together.
package app

import (
	"fmt"
	"net/http"
	"time"

	"github.com/meme-launchpad/app-rebuild/internal/config"
	"github.com/meme-launchpad/app-rebuild/internal/httpapi"
)

// Application is the dependency container for one API process.
// Step 3 will add the PostgreSQL dependency here.
type Application struct {
	Config config.Config
}

func New(cfg config.Config) *Application {
	return &Application{Config: cfg}
}

func (a *Application) HTTPServer() *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", a.Config.HTTP.Port),
		Handler:           httpapi.NewHandler(a.Config.ServiceName),
		ReadHeaderTimeout: 5 * time.Second,
	}
}
