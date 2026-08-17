package dotenv

import (
	"reflect"
	"testing"
)

// TestParseBasic 覆盖常规解析、前向引用（正序与逆序）与空值。
func TestParseBasic(t *testing.T) {
	cases := []struct {
		name    string
		content string
		base    map[string]string
		want    []Variable
	}{
		{
			name:    "直接展开",
			content: "A=1\nB=${A}2",
			want:    []Variable{{Key: "A", Value: "1"}, {Key: "B", Value: "12"}},
		},
		{
			name:    "前向引用",
			content: "A=${B}\nB=ok",
			want:    []Variable{{Key: "A", Value: "ok"}, {Key: "B", Value: "ok"}},
		},
		{
			name:    "前向引用逆序",
			content: "B=ok\nA=${B}",
			want:    []Variable{{Key: "B", Value: "ok"}, {Key: "A", Value: "ok"}},
		},
		{
			name:    "多层链式引用",
			content: "A=${B}\nB=${C}\nC=deep",
			want:    []Variable{{Key: "A", Value: "deep"}, {Key: "B", Value: "deep"}, {Key: "C", Value: "deep"}},
		},
		{
			name:    "空值",
			content: "A=",
			want:    []Variable{{Key: "A", Value: ""}},
		},
		{
			name:    "export 前缀",
			content: "export A=1\nexport  B=2",
			want:    []Variable{{Key: "A", Value: "1"}, {Key: "B", Value: "2"}},
		},
		{
			name:    "注释与空行",
			content: "# 注释\n\nA=1\n# 另一条注释\nB=2",
			want:    []Variable{{Key: "A", Value: "1"}, {Key: "B", Value: "2"}},
		},
		{
			name:    "CRLF",
			content: "A=1\r\nB=2\r\n",
			want:    []Variable{{Key: "A", Value: "1"}, {Key: "B", Value: "2"}},
		},
		{
			name:    "行内井号为值的一部分",
			content: "A=foo # bar",
			want:    []Variable{{Key: "A", Value: "foo # bar"}},
		},
		{
			name:    "未加引号反斜杠按字面量",
			content: `A=a\b`,
			want:    []Variable{{Key: "A", Value: `a\b`}},
		},
		{
			name:    "未加引号尾部空白去除",
			content: "A=${B}   ",
			base:    map[string]string{"B": "v"},
			want:    []Variable{{Key: "A", Value: "v"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Parse(tc.content, tc.base)
			if len(res.Errors) != 0 {
				t.Fatalf("unexpected errors: %+v", res.Errors)
			}
			if !reflect.DeepEqual(res.Variables, tc.want) {
				t.Fatalf("got %+v, want %+v", res.Variables, tc.want)
			}
		})
	}
}

// TestParseQuotes 覆盖单/双引号语义差异与转义。
func TestParseQuotes(t *testing.T) {
	cases := []struct {
		name    string
		content string
		base    map[string]string
		want    []Variable
	}{
		{
			name:    "单引号字面量不展开",
			content: `Q='x=${Y}'`,
			want:    []Variable{{Key: "Q", Value: "x=${Y}"}},
		},
		{
			name:    "双引号展开",
			content: "Y=z\nD=\"x=${Y}\"",
			want:    []Variable{{Key: "Y", Value: "z"}, {Key: "D", Value: "x=z"}},
		},
		{
			name:    "双引号转义引号与反斜杠",
			content: `D="a\"b\\c"`,
			want:    []Variable{{Key: "D", Value: `a"b\c`}},
		},
		{
			name:    "双引号转义换行",
			content: `D="line1\nline2"`,
			want:    []Variable{{Key: "D", Value: "line1\nline2"}},
		},
		{
			name:    "双引号转义美元符号为字面量",
			content: `D="\${A}"`,
			base:    map[string]string{"A": "should-not-appear"},
			want:    []Variable{{Key: "D", Value: "${A}"}},
		},
		{
			name:    "单引号内反斜杠字面量",
			content: `Q='a\nb'`,
			want:    []Variable{{Key: "Q", Value: `a\nb`}},
		},
		{
			name:    "等号两侧空白",
			content: "A   =   1",
			want:    []Variable{{Key: "A", Value: "1"}},
		},
		{
			name:    "双引号内等号",
			content: `A="x=y"`,
			want:    []Variable{{Key: "A", Value: "x=y"}},
		},
		{
			name:    "未加引号值含等号",
			content: "A=B=C",
			want:    []Variable{{Key: "A", Value: "B=C"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Parse(tc.content, tc.base)
			if len(res.Errors) != 0 {
				t.Fatalf("unexpected errors: %+v", res.Errors)
			}
			if !reflect.DeepEqual(res.Variables, tc.want) {
				t.Fatalf("got %+v, want %+v", res.Variables, tc.want)
			}
		})
	}
}

// TestParseBase 覆盖 base 提供值与文档覆盖 base。
func TestParseBase(t *testing.T) {
	t.Run("base 提供引用目标", func(t *testing.T) {
		res := Parse("D=${Y}", map[string]string{"Y": "frombase"})
		assertNoErr(t, res)
		want := []Variable{{Key: "D", Value: "frombase"}}
		if !reflect.DeepEqual(res.Variables, want) {
			t.Fatalf("got %+v, want %+v", res.Variables, want)
		}
	})
	t.Run("文档覆盖 base", func(t *testing.T) {
		res := Parse("Y=doc\nD=${Y}", map[string]string{"Y": "frombase"})
		assertNoErr(t, res)
		want := []Variable{{Key: "Y", Value: "doc"}, {Key: "D", Value: "doc"}}
		if !reflect.DeepEqual(res.Variables, want) {
			t.Fatalf("got %+v, want %+v", res.Variables, want)
		}
	})
}

// TestParseErrors 覆盖各类错误类别。
func TestParseErrors(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		wantCode string
	}{
		{"自循环", "A=${A}", CodeCyclicRef},
		{"二元循环", "A=${B}\nB=${A}", CodeCyclicRef},
		{"三元循环", "A=${B}\nB=${C}\nC=${A}", CodeCyclicRef},
		{"未定义变量", "A=${C}", CodeUndefinedVar},
		{"重复键", "A=1\nA=2", CodeDuplicateKey},
		{"非法键数字开头", "1ABC=v", CodeIllegalKey},
		{"非法键含连字符", "A-B=v", CodeIllegalKey},
		{"缺少赋值符号", "KEYVALUE", CodeMalformedLine},
		{"非法引用名数字开头", "A=${1x}", CodeIllegalRef},
		{"非法引用名缺右花括号", "A=${x", CodeIllegalRef},
		{"未闭合单引号", "A='x", CodeUnterminatedSingle},
		{"未闭合双引号", "A=\"x", CodeUnterminatedDouble},
		{"双引号尾部多余内容", `A="x" y`, CodeTrailingContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Parse(tc.content, nil)
			if len(res.Errors) == 0 {
				t.Fatalf("expected error %q, got none", tc.wantCode)
			}
			if res.Errors[0].Code != tc.wantCode {
				t.Fatalf("first error code=%q, want %q (all: %+v)", res.Errors[0].Code, tc.wantCode, res.Errors)
			}
			if len(res.Variables) != 0 {
				t.Fatalf("expected no variables on error, got %+v", res.Variables)
			}
		})
	}
}

// TestParseMultiError 验证多个可独立判定错误被一并收集。
func TestParseMultiError(t *testing.T) {
	content := "1A=x\nB=${C}\nB=2"
	res := Parse(content, nil)
	if len(res.Errors) < 3 {
		t.Fatalf("expected >=3 errors, got %d: %+v", len(res.Errors), res.Errors)
	}
	codes := map[string]bool{}
	for _, e := range res.Errors {
		codes[e.Code] = true
	}
	for _, want := range []string{CodeIllegalKey, CodeUndefinedVar, CodeDuplicateKey} {
		if !codes[want] {
			t.Fatalf("missing error code %q in %+v", want, res.Errors)
		}
	}
}

// TestParseCyclePath 验证循环引用的错误信息包含完整环路径。
func TestParseCyclePath(t *testing.T) {
	res := Parse("A=${B}\nB=${C}\nC=${A}", nil)
	if len(res.Errors) != 1 {
		t.Fatalf("expected 1 cycle error after dedupe, got %d: %+v", len(res.Errors), res.Errors)
	}
	msg := res.Errors[0].Message
	want := "循环引用: A -> B -> C -> A"
	if msg != want {
		t.Fatalf("cycle message=%q, want %q", msg, want)
	}
}

func assertNoErr(t *testing.T, res Result) {
	t.Helper()
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", res.Errors)
	}
}
