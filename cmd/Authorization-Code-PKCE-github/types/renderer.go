package types

import (
	"html/template"
	"io"
	"path/filepath"

	"github.com/labstack/echo/v5"
)

// TemplateRenderer 实现 echo.Renderer 接口
type TemplateRenderer struct {
	templates *template.Template
}

// NewTemplateRenderer 创建模板渲染器，pattern 是模板文件匹配模式
func NewTemplateRenderer(patterns ...string) *TemplateRenderer {
	// 从调用方目录计算绝对路径模式
	absPatterns := make([]string, len(patterns))
	for i, p := range patterns {
		absPatterns[i] = filepath.Join(p)
	}
	return &TemplateRenderer{
		templates: template.Must(template.ParseFiles(absPatterns...)),
	}
}

// Render 实现 echo.Renderer 接口
func (t *TemplateRenderer) Render(c *echo.Context, w io.Writer, name string, data any) error {
	return t.templates.ExecuteTemplate(w, name, data)
}
