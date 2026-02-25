package veille

import (
	"github.com/hazyhaar/chrc/veille/internal/pipeline"
	"github.com/hazyhaar/pkg/connectivity"
)

// NewAPIService returns a connectivity.Handler for the "api_fetch" service.
// Register on a connectivity.Router with: router.RegisterLocal("api_fetch", ...)
func NewAPIService() connectivity.Handler {
	return pipeline.NewAPIService()
}
