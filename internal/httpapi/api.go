// Package httpapi 提供 .env 解析服务的 HTTP 接口。
// 服务无内部可变状态，可被多个 goroutine 复用。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"task030-dotenv/internal/dotenv"
)

// ErrBadJSON 表示请求体不是单个合法 JSON 对象。
var ErrBadJSON = errors.New("请求体不是合法的单个 JSON 对象")

// API 是 .env 解析服务的 HTTP 接口实现。
type API struct{}

// New 创建服务实例。
func New() *API { return &API{} }

// Handler 返回 HTTP 路由。
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("POST /parse", a.parse)
	return mux
}

// decodeJSON 解码单个 JSON 对象，拒绝多段 JSON 与未知字段。
func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return ErrBadJSON
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrBadJSON, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return ErrBadJSON
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type parseRequest struct {
	Content string            `json:"content"`
	Base    map[string]string `json:"base"`
}

type parseResponse struct {
	Variables []dotenv.Variable `json:"variables"`
	Count     int               `json:"count"`
}

type errorResponse struct {
	Errors []dotenv.Error `json:"errors"`
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) parse(w http.ResponseWriter, r *http.Request) {
	var req parseRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Errors: []dotenv.Error{{Line: 0, Code: dotenv.CodeBadRequest, Message: err.Error()}},
		})
		return
	}
	res := dotenv.Parse(req.Content, req.Base)
	if len(res.Errors) > 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{Errors: res.Errors})
		return
	}
	writeJSON(w, http.StatusOK, parseResponse{Variables: res.Variables, Count: len(res.Variables)})
}
