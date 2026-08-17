// Package selfcheck 提供无需外部依赖的自检：通过 httptest 启动真实 HTTP
// 服务，覆盖解析端点与各边界约束。成功返回 0，任一失败返回 1。
package selfcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"task030-dotenv/internal/dotenv"
	"task030-dotenv/internal/httpapi"
)

// Run 执行自检并返回退出码。
func Run() int {
	passed, failed := 0, 0
	check := func(name string, fn func() error) {
		if err := fn(); err != nil {
			failed++
			fmt.Printf("FAIL %-36s %v\n", name, err)
		} else {
			passed++
			fmt.Printf("PASS %s\n", name)
		}
	}

	srv := httptest.NewServer(httpapi.New().Handler())
	defer srv.Close()

	do := func(method, path, body string) (*http.Response, []byte, error) {
		var r io.Reader
		if body != "" {
			r = bytes.NewReader([]byte(body))
		}
		req, err := http.NewRequest(method, srv.URL+path, r)
		if err != nil {
			return nil, nil, err
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, nil, err
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, data, readErr
	}
	marshal := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return string(b)
	}

	// parse 端点封装：返回状态码、变量表与错误表。
	parse := func(content string, base map[string]string) (status int, vars []dotenv.Variable, errs []dotenv.Error, err error) {
		body := marshal(map[string]any{"content": content, "base": base})
		resp, data, err := do(http.MethodPost, "/parse", body)
		if err != nil {
			return 0, nil, nil, err
		}
		if resp.StatusCode == http.StatusOK {
			var out struct {
				Variables []dotenv.Variable `json:"variables"`
				Count     int               `json:"count"`
			}
			_ = json.Unmarshal(data, &out)
			return resp.StatusCode, out.Variables, nil, nil
		}
		var out struct {
			Errors []dotenv.Error `json:"errors"`
		}
		_ = json.Unmarshal(data, &out)
		return resp.StatusCode, nil, out.Errors, nil
	}

	// assertOK 断言解析成功并精确匹配变量表。
	assertOK := func(name, content string, base map[string]string, want []dotenv.Variable) {
		check(name, func() error {
			status, vars, errs, err := parse(content, base)
			if err != nil {
				return err
			}
			if status != http.StatusOK {
				return fmt.Errorf("status=%d errs=%+v", status, errs)
			}
			if len(errs) != 0 {
				return fmt.Errorf("unexpected errors: %+v", errs)
			}
			if !equalVars(vars, want) {
				return fmt.Errorf("vars=%+v want %+v", vars, want)
			}
			return nil
		})
	}

	// assertValue 断言解析成功且单个键的值匹配。
	assertValue := func(name, content string, base map[string]string, key, value string) {
		check(name, func() error {
			status, vars, errs, err := parse(content, base)
			if err != nil {
				return err
			}
			if status != http.StatusOK {
				return fmt.Errorf("status=%d errs=%+v", status, errs)
			}
			for _, v := range vars {
				if v.Key == key {
					if v.Value != value {
						return fmt.Errorf("key %s value=%q want %q", key, v.Value, value)
					}
					return nil
				}
			}
			return fmt.Errorf("key %q not found in %+v", key, vars)
		})
	}

	// assertError 断言解析失败并包含指定错误类别。
	assertError := func(name, content string, base map[string]string, wantCode string) {
		check(name, func() error {
			status, _, errs, err := parse(content, base)
			if err != nil {
				return err
			}
			if status != http.StatusBadRequest {
				return fmt.Errorf("status=%d want 400", status)
			}
			if !hasCode(errs, wantCode) {
				return fmt.Errorf("missing code %q in %+v", wantCode, errs)
			}
			return nil
		})
	}

	// ---- 健康检查 ----
	check("健康检查", func() error {
		resp, _, err := do(http.MethodGet, "/healthz", "")
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	// ---- 基础解析与展开 ----
	assertOK("直接展开", "A=1\nB=${A}2", nil,
		[]dotenv.Variable{{Key: "A", Value: "1"}, {Key: "B", Value: "12"}})
	assertValue("前向引用", "A=${B}\nB=ok", nil, "A", "ok")
	assertValue("前向引用逆序", "B=ok\nA=${B}", nil, "A", "ok")
	assertValue("多层链式引用", "A=${B}\nB=${C}\nC=deep", nil, "A", "deep")
	assertValue("空值", "A=", nil, "A", "")
	assertValue("export 前缀", "export A=1", nil, "A", "1")
	assertValue("注释与空行", "# c\n\nA=1\n# c2\nB=2", nil, "A", "1")
	assertValue("CRLF", "A=1\r\nB=2", nil, "B", "2")

	// ---- 引号语义差异（约束2）----
	assertValue("单引号字面量不展开", `Q='x=${Y}'`, nil, "Q", "x=${Y}")
	assertValue("单引号反斜杠字面量", `Q='a\nb'`, nil, "Q", `a\nb`)
	assertValue("双引号展开", "Y=z\nD=\"x=${Y}\"", nil, "D", "x=z")

	// ---- 双引号转义（约束4）----
	assertValue("双引号转义引号反斜杠", `D="a\"b\\c"`, nil, "D", `a"b\c`)
	assertValue("双引号转义换行", `D="line1\nline2"`, nil, "D", "line1\nline2")
	assertValue("转义美元符号为字面量", `D="\${A}"`, map[string]string{"A": "x"}, "D", "${A}")

	// ---- 前向引用与循环检测（约束1）----
	assertError("自循环", "A=${A}", nil, dotenv.CodeCyclicRef)
	assertError("二元循环", "A=${B}\nB=${A}", nil, dotenv.CodeCyclicRef)
	assertError("三元循环", "A=${B}\nB=${C}\nC=${A}", nil, dotenv.CodeCyclicRef)

	// 循环路径在错误信息中
	check("循环路径完整", func() error {
		_, _, errs, err := parse("A=${B}\nB=${C}\nC=${A}", nil)
		if err != nil {
			return err
		}
		if !hasCode(errs, dotenv.CodeCyclicRef) {
			return fmt.Errorf("missing cyclic error in %+v", errs)
		}
		for _, e := range errs {
			if e.Code == dotenv.CodeCyclicRef && e.Message == "循环引用: A -> B -> C -> A" {
				return nil
			}
		}
		return fmt.Errorf("cycle path not exact in %+v", errs)
	})

	// ---- 未定义变量 ----
	assertError("未定义变量", "A=${C}", nil, dotenv.CodeUndefinedVar)
	assertError("双引号未定义变量", `D="x=${Y}"`, nil, dotenv.CodeUndefinedVar)

	// ---- 重复键、非法键、缺少赋值符号（约束3）----
	assertError("重复键", "A=1\nA=2", nil, dotenv.CodeDuplicateKey)
	assertError("非法键数字开头", "1ABC=v", nil, dotenv.CodeIllegalKey)
	assertError("非法键含连字符", "A-B=v", nil, dotenv.CodeIllegalKey)
	assertError("缺少赋值符号", "KEYVALUE", nil, dotenv.CodeMalformedLine)
	assertError("非法引用名数字开头", "A=${1x}", nil, dotenv.CodeIllegalRef)
	assertError("非法引用名缺右花括号", "A=${x", nil, dotenv.CodeIllegalRef)
	assertError("未闭合单引号", "A='x", nil, dotenv.CodeUnterminatedSingle)
	assertError("未闭合双引号", `A="x`, nil, dotenv.CodeUnterminatedDouble)
	assertError("双引号尾部多余内容", `A="x" y`, nil, dotenv.CodeTrailingContent)

	// ---- base 覆盖 ----
	assertValue("base 提供引用目标", "D=${Y}", map[string]string{"Y": "frombase"}, "D", "frombase")
	assertValue("文档覆盖 base", "Y=doc\nD=${Y}", map[string]string{"Y": "frombase"}, "D", "doc")

	// ---- 多错误收集 ----
	check("多错误一并收集", func() error {
		_, _, errs, err := parse("1A=x\nB=${C}\nB=2", nil)
		if err != nil {
			return err
		}
		if len(errs) < 3 {
			return fmt.Errorf("expected >=3 errors, got %d: %+v", len(errs), errs)
		}
		for _, want := range []string{dotenv.CodeIllegalKey, dotenv.CodeUndefinedVar, dotenv.CodeDuplicateKey} {
			if !hasCode(errs, want) {
				return fmt.Errorf("missing code %q in %+v", want, errs)
			}
		}
		return nil
	})

	// ---- 请求格式校验（400）----
	check("非法 JSON 被拒(400)", func() error {
		resp, _, err := do(http.MethodPost, "/parse", "{not json")
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d want 400", resp.StatusCode)
		}
		return nil
	})
	check("多段 JSON 被拒(400)", func() error {
		b1 := marshal(map[string]any{"content": "A=1"})
		b2 := marshal(map[string]any{"content": "B=2"})
		resp, _, err := do(http.MethodPost, "/parse", b1+b2)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d want 400", resp.StatusCode)
		}
		return nil
	})
	check("未知字段被拒(400)", func() error {
		resp, _, err := do(http.MethodPost, "/parse", marshal(map[string]any{"content": "A=1", "extra": 1}))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d want 400", resp.StatusCode)
		}
		return nil
	})
	check("content 非字符串被拒(400)", func() error {
		resp, _, err := do(http.MethodPost, "/parse", marshal(map[string]any{"content": 123}))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d want 400", resp.StatusCode)
		}
		return nil
	})

	// ---- 空文档返回空变量表 ----
	check("空文档返回空变量表", func() error {
		resp, data, err := do(http.MethodPost, "/parse", marshal(map[string]any{"content": ""}))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status=%d want 200", resp.StatusCode)
		}
		if !strings.Contains(string(data), `"count":0`) {
			return fmt.Errorf("expected count:0, got: %s", data)
		}
		return nil
	})

	fmt.Printf("\n%d passed, %d failed\n", passed, failed)
	if failed > 0 {
		return 1
	}
	return 0
}

func equalVars(a, b []dotenv.Variable) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hasCode(errs []dotenv.Error, code string) bool {
	for _, e := range errs {
		if e.Code == code {
			return true
		}
	}
	return false
}
