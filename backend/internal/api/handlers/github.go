package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/environment-manager/backend/internal/credentials"
	"go.uber.org/zap"
)

// githubProvider is the credential-store key used for the global GitHub PAT.
const githubProvider = "github"

// GitHubHandler exposes endpoints for storing a single GitHub Personal Access
// Token and using it to browse the authenticated user's repositories, so
// users don't have to paste a token every time they clone.
type GitHubHandler struct {
	creds  *credentials.Store
	client *http.Client
	logger *zap.Logger
}

// NewGitHubHandler creates a new GitHub integration handler.
func NewGitHubHandler(creds *credentials.Store, logger *zap.Logger) *GitHubHandler {
	return &GitHubHandler{
		creds:  creds,
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}
}

type githubTokenRequest struct {
	Token string `json:"token"`
}

type githubStatusResponse struct {
	Connected bool   `json:"connected"`
	Login     string `json:"login,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

type githubRepoSummary struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	Private       bool      `json:"private"`
	CloneURL      string    `json:"clone_url"`
	HTMLURL       string    `json:"html_url"`
	Description   string    `json:"description"`
	DefaultBranch string    `json:"default_branch"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// SetToken stores a PAT. We probe /user immediately to fail fast on a bad
// token rather than silently saving garbage.
func (h *GitHubHandler) SetToken(w http.ResponseWriter, r *http.Request) {
	if h.creds == nil {
		respondError(w, http.StatusServiceUnavailable, "NO_CRED_STORE", "credential store not initialized")
		return
	}

	var req githubTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if req.Token == "" {
		respondError(w, http.StatusBadRequest, "TOKEN_REQUIRED", "token is required")
		return
	}

	user, err := h.fetchUser(r.Context(), req.Token)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "TOKEN_INVALID", err.Error())
		return
	}

	if err := h.creds.SaveGlobalToken(githubProvider, req.Token); err != nil {
		respondError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
		return
	}

	respondSuccess(w, githubStatusResponse{
		Connected: true,
		Login:     user.Login,
		AvatarURL: user.AvatarURL,
	})
}

// GetStatus reports whether a PAT is stored. When connected, also returns the
// authenticated login for display so the UI doesn't need to guess.
func (h *GitHubHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	if h.creds == nil || !h.creds.HasGlobalToken(githubProvider) {
		respondSuccess(w, githubStatusResponse{Connected: false})
		return
	}

	token, err := h.creds.GetGlobalToken(githubProvider)
	if err != nil {
		respondSuccess(w, githubStatusResponse{Connected: false})
		return
	}

	user, err := h.fetchUser(r.Context(), token)
	if err != nil {
		// Token is stored but invalid (expired/revoked). Report disconnected
		// so the UI prompts for a new PAT.
		h.logger.Warn("stored GitHub token is invalid", zap.Error(err))
		respondSuccess(w, githubStatusResponse{Connected: false})
		return
	}

	respondSuccess(w, githubStatusResponse{
		Connected: true,
		Login:     user.Login,
		AvatarURL: user.AvatarURL,
	})
}

// DeleteToken removes the stored PAT.
func (h *GitHubHandler) DeleteToken(w http.ResponseWriter, r *http.Request) {
	if h.creds == nil {
		respondError(w, http.StatusServiceUnavailable, "NO_CRED_STORE", "credential store not initialized")
		return
	}
	if err := h.creds.DeleteGlobalToken(githubProvider); err != nil {
		respondError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}
	respondSuccess(w, map[string]bool{"disconnected": true})
}

// ListRepos proxies GET /user/repos with the stored PAT, returning a trimmed
// projection. Sorted by recently updated so the interesting repos float up.
func (h *GitHubHandler) ListRepos(w http.ResponseWriter, r *http.Request) {
	if h.creds == nil {
		respondError(w, http.StatusServiceUnavailable, "NO_CRED_STORE", "credential store not initialized")
		return
	}
	token, err := h.creds.GetGlobalToken(githubProvider)
	if err != nil || token == "" {
		respondError(w, http.StatusUnauthorized, "NOT_CONNECTED", "no GitHub token stored")
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), "GET",
		"https://api.github.com/user/repos?per_page=100&sort=updated&affiliation=owner,collaborator,organization_member", nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "GITHUB_REQUEST_FAILED", err.Error())
		return
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := h.client.Do(req)
	if err != nil {
		respondError(w, http.StatusBadGateway, "GITHUB_UNREACHABLE", err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		respondError(w, resp.StatusCode, "GITHUB_ERROR", string(body))
		return
	}

	var repos []githubRepoSummary
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		respondError(w, http.StatusBadGateway, "GITHUB_DECODE_FAILED", err.Error())
		return
	}

	respondSuccess(w, repos)
}

// githubUser is a minimal shape for /user.
type githubUser struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

// fetchUser calls GET /user to validate the token and read the authenticated
// login. Any non-200 response is treated as an invalid token.
func (h *GitHubHandler) fetchUser(ctx context.Context, token string) (*githubUser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github returned %d", resp.StatusCode)
	}

	var u githubUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}
	return &u, nil
}
