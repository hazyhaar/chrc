package veille

import (
	"github.com/hazyhaar/chrc/veille/internal/pipeline"
	"github.com/hazyhaar/pkg/connectivity"
)

// NewGitHubService returns a connectivity.Handler for the "github_fetch" service.
// apiBaseURL overrides the GitHub API base (for testing). Empty string uses production.
// Register on a connectivity.Router with: router.RegisterLocal("github_fetch", ...)
func NewGitHubService(apiBaseURL string) connectivity.Handler {
	return pipeline.NewGitHubService(apiBaseURL)
}
