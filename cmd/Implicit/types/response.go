package types

import (
	"html/template"
	"io"
	"path/filepath"

	"github.com/labstack/echo/v5"
)

// ──────────────────────────────────────────────
// Error Response (RFC 4.2.2.1)
// ──────────────────────────────────────────────

// ErrorCode OAuth2 标准错误码
type ErrorCode string

const (
	ErrorInvalidRequest          ErrorCode = "invalid_request"
	ErrorUnauthorizedClient      ErrorCode = "unauthorized_client"
	ErrorAccessDenied            ErrorCode = "access_denied"
	ErrorUnsupportedResponseType ErrorCode = "unsupported_response_type"
	ErrorInvalidScope            ErrorCode = "invalid_scope"
	ErrorServerError             ErrorCode = "server_error"
	ErrorTemporarilyUnavailable  ErrorCode = "temporarily_unavailable"
)

// ErrorResponse OAuth2 错误响应
type ErrorResponse struct {
	Error            ErrorCode `json:"error"`
	ErrorDescription string    `json:"error_description,omitempty"`
	ErrorURI         string    `json:"error_uri,omitempty"`
	State            string    `json:"state,omitempty"`
}

// ──────────────────────────────────────────────
// Access Token Response (RFC 4.2.2)
// ──────────────────────────────────────────────

// FragmentResponse 隐式授权返回的 fragment 参数
type FragmentResponse struct {
	AccessToken string `json:"access_token"`          // REQUIRED
	TokenType   string `json:"token_type"`            // REQUIRED
	ExpiresIn   int    `json:"expires_in"`            // RECOMMENDED
	Scope       string `json:"scope,omitempty"`       // OPTIONAL
	State       string `json:"state,omitempty"`       // REQUIRED if in request
}

// ──────────────────────────────────────────────
// Introspect Response
// ──────────────────────────────────────────────

type IntrospectResponse struct {
	Active   bool   `json:"active"`
	ClientID string `json:"client_id,omitempty"`
	Username string `json:"username,omitempty"`
	Exp      int64  `json:"exp,omitempty"`
}

// ──────────────────────────────────────────────
// Template Renderer
// ──────────────────────────────────────────────

type TemplateRenderer struct {
	templates *template.Template
}

func NewTemplateRenderer(patterns ...string) *TemplateRenderer {
	absPatterns := make([]string, len(patterns))
	for i, p := range patterns {
		absPatterns[i] = filepath.Join(p)
	}
	return &TemplateRenderer{
		templates: template.Must(template.ParseFiles(absPatterns...)),
	}
}

func (t *TemplateRenderer) Render(c *echo.Context, w io.Writer, name string, data any) error {
	return t.templates.ExecuteTemplate(w, name, data)
}
