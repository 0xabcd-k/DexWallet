// Code scaffolded by goctl. Safe to edit.
// goctl {{.version}}

package {{.PkgName}}

import (
	"net/http"

	{{if .HasRequest}}"github.com/zeromicro/go-zero/rest/httpx"
	"{{.projectPkg}}/apperror"{{end}}
	"{{.projectPkg}}/__API_DIR__/internal/httpapi"
	{{.ImportPackages}}
)

{{if .HasDoc}}{{.Doc}}{{end}}
func {{.HandlerName}}(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		{{if .HasRequest}}var req types.{{.RequestType}}
		if err := httpx.Parse(r, &req); err != nil {
			httpapi.Error(r.Context(), w, apperror.New(apperror.InvalidArgument, err))
			return
		}

		{{end}}l := {{.LogicName}}.New{{.LogicType}}(r.Context(), svcCtx)
		{{if .HasResp}}resp, {{end}}err := l.{{.Call}}({{if .HasRequest}}&req{{end}})
		if err != nil {
			httpapi.Error(r.Context(), w, err)
		} else {
			{{if .HasResp}}httpapi.JSON(r.Context(), w, http.StatusOK, resp){{else}}w.WriteHeader(http.StatusOK){{end}}
		}
	}
}
