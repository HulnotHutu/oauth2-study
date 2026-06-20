package types

import (
	"html/template"
	"io"
	"path/filepath"

	"github.com/labstack/echo/v5"
)

// ErrorCode OAuth2 标准错误码
type ErrorCode string

const (
	ErrorInvalidRequest          ErrorCode = "invalid_request"
	ErrorInvalidClient           ErrorCode = "invalid_client"
	ErrorInvalidGrant            ErrorCode = "invalid_grant"
	ErrorUnsupportedGrantType    ErrorCode = "unsupported_grant_type"
	ErrorInvalidScope            ErrorCode = "invalid_scope"
	ErrorServerError             ErrorCode = "server_error"
)

// ErrorResponse OAuth2 错误响应
type ErrorResponse struct {
	Error            ErrorCode `json:"error"`
	ErrorDescription string    `json:"error_description,omitempty"`
}

// AccessTokenResponse 访问令牌成功响应
type AccessTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// IntrospectResponse introspection 端点响应
type IntrospectResponse struct {
	Active   bool   `json:"active"`
	ClientID string `json:"client_id,omitempty"`
	Username string `json:"username,omitempty"`
	Exp      int64  `json:"exp,omitempty"`
}

// TemplateRenderer 实现 echo.Renderer 接口
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
