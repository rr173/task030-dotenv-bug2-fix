// Package dotenv 解析 .env 配置文本并展开变量引用。
//
// 解析分两阶段：先逐行解析出键值定义（含引用片段），再按依赖关系展开
// 变量引用，期间检测未定义引用与循环引用。服务无内部可变状态。
package dotenv

import "strings"

// 错误类别常量。
const (
	CodeIllegalKey         = "illegal_key"
	CodeDuplicateKey       = "duplicate_key"
	CodeUnterminatedSingle = "unterminated_single_quote"
	CodeUnterminatedDouble = "unterminated_double_quote"
	CodeTrailingContent    = "trailing_content"
	CodeIllegalRef         = "illegal_reference"
	CodeUndefinedVar       = "undefined_variable"
	CodeCyclicRef          = "cyclic_reference"
	CodeMalformedLine      = "malformed_line"
	CodeBadRequest         = "bad_request"
)

// Error 描述解析或展开过程中的一个问题。
type Error struct {
	Line    int    `json:"line"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Variable 是一个已解析的键值对。
type Variable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Result 是一次解析的最终结果。Variables 与 Errors 互斥：
// 存在任意错误时 Variables 为空。
type Result struct {
	Variables []Variable `json:"variables"`
	Errors    []Error    `json:"errors"`
}

// quoteKind 标记值的原始引号形式。
type quoteKind int

const (
	quoteNone quoteKind = iota
	quoteSingle
	quoteDouble
)

// segmentKind 标记值片段是字面量还是变量引用。
type segmentKind int

const (
	segLiteral segmentKind = iota
	segRef
)

// segment 是值的一个组成片段。对字面量，text 为文本；对引用，text 为变量名。
type segment struct {
	kind segmentKind
	text string
}

// def 是一个已解析的键值定义。
type def struct {
	key      string
	line     int
	quote    quoteKind
	segments []segment
}

// isKeyStart 报告字节能否作为键/变量名的首字符。
func isKeyStart(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

// isKeyPart 报告字节能否作为键/变量名的非首字符。
func isKeyPart(b byte) bool {
	return isKeyStart(b) || (b >= '0' && b <= '9')
}

// validKey 报告字符串是否是合法的键名 [A-Za-z_][A-Za-z0-9_]*。
func validKey(s string) bool {
	if s == "" || !isKeyStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isKeyPart(s[i]) {
			return false
		}
	}
	return true
}

// Parse 解析 .env 文本 content，并以 base 作为初始环境展开变量引用。
// base 中的值视为已解析的字面量、不再展开；文档中定义的键覆盖 base 中同名键。
func Parse(content string, base map[string]string) Result {
	defs, errs := parseLines(content)

	// 重复键检测，同时按首现顺序构建去重后的定义表。
	defMap := make(map[string]def, len(defs))
	var order []def
	for _, d := range defs {
		if _, ok := defMap[d.key]; ok {
			errs = append(errs, Error{Line: d.line, Code: CodeDuplicateKey, Message: "重复键: " + d.key})
			continue
		}
		defMap[d.key] = d
		order = append(order, d)
	}

	// 展开阶段：按文档顺序解析每个键，收集未定义/循环引用错误。
	r := &resolver{
		defs:     defMap,
		base:     base,
		memo:     make(map[string]resolveResult, len(order)),
		stackSet: make(map[string]bool),
	}
	var vars []Variable
	for _, d := range order {
		rr := r.resolve(d.key)
		errs = append(errs, rr.errs...)
		if len(rr.errs) == 0 {
			vars = append(vars, Variable{Key: d.key, Value: rr.value})
		}
	}

	errs = dedupeErrs(errs)
	if len(errs) > 0 {
		return Result{Errors: errs}
	}
	return Result{Variables: vars}
}

// resolveResult 是单个键的展开结果，errs 非空表示该键无法完全解析。
type resolveResult struct {
	value string
	errs  []Error
}

// resolver 在一次 Parse 调用内共享展开状态：memo 记忆已解析的键，
// stack/stackSet 用于循环引用检测。
type resolver struct {
	defs     map[string]def
	base     map[string]string
	memo     map[string]resolveResult
	stack    []string
	stackSet map[string]bool
}

// resolve 返回 name 的完全展开值。文档定义优先于 base；
// 既非文档定义又非 base 中的键视为未定义；展开到自身形成环时报循环引用。
func (r *resolver) resolve(name string) resolveResult {
	if res, ok := r.memo[name]; ok {
		return res
	}
	d, isDef := r.defs[name]
	if !isDef {
		if v, ok := r.base[name]; ok {
			res := resolveResult{value: v}
			r.memo[name] = res
			return res
		}
		return resolveResult{errs: []Error{{Line: 0, Code: CodeUndefinedVar, Message: "未定义变量: " + name}}}
	}
	if r.stackSet[name] {
		path := r.cyclePath(name)
		return resolveResult{errs: []Error{{Line: 0, Code: CodeCyclicRef, Message: "循环引用: " + strings.Join(path, " -> ")}}}
	}

	r.stack = append(r.stack, name)
	r.stackSet[name] = true
	defer func() {
		r.stack = r.stack[:len(r.stack)-1]
		delete(r.stackSet, name)
	}()

	var b strings.Builder
	var errs []Error
	for _, seg := range d.segments {
		if seg.kind == segLiteral {
			b.WriteString(seg.text)
			continue
		}
		rr := r.resolve(seg.text)
		b.WriteString(rr.value)
		errs = append(errs, rr.errs...)
	}
	res := resolveResult{value: b.String(), errs: errs}
	r.memo[name] = res
	return res
}

// cyclePath 在检测到环时，从栈中 name 首次出现的位置到栈顶构造环路径，并回指 name。
func (r *resolver) cyclePath(name string) []string {
	for i, s := range r.stack {
		if s == name {
			path := make([]string, 0, len(r.stack)-i+1)
			path = append(path, r.stack[i:]...)
			path = append(path, name)
			return path
		}
	}
	return []string{name, name}
}

// dedupeErrs 按 (code, message) 去重，保留首次出现的行号。
func dedupeErrs(errs []Error) []Error {
	seen := make(map[string]bool, len(errs))
	out := make([]Error, 0, len(errs))
	for _, e := range errs {
		k := e.Code + "\x00" + e.Message
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out
}

// parseLines 把文档文本逐行解析为定义列表与解析错误列表。
func parseLines(content string) ([]def, []Error) {
	var defs []def
	var errs []Error
	lines := strings.Split(content, "\n")
	for i, raw := range lines {
		lineNo := i + 1
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		d, err, ok := parseAssignment(line, lineNo)
		if !ok {
			errs = append(errs, err)
			continue
		}
		defs = append(defs, d)
	}
	return defs, errs
}

// parseAssignment 解析一条赋值行，返回定义与可能的错误。
func parseAssignment(line string, lineNo int) (def, Error, bool) {
	s := strings.TrimLeft(line, " \t")
	s = stripExport(s)
	s = strings.TrimLeft(s, " \t")

	eq := strings.IndexByte(s, '=')
	if eq < 0 {
		return def{}, Error{Line: lineNo, Code: CodeMalformedLine, Message: "缺少赋值符号 ="}, false
	}
	keyTok := strings.TrimSpace(s[:eq])
	if keyTok == "" {
		return def{}, Error{Line: lineNo, Code: CodeIllegalKey, Message: "键不能为空"}, false
	}
	if !validKey(keyTok) {
		return def{}, Error{Line: lineNo, Code: CodeIllegalKey, Message: "非法键: " + keyTok}, false
	}
	rest := strings.TrimLeft(s[eq+1:], " \t")
	return parseValue(keyTok, rest, lineNo)
}

// stripExport 去除行首可选的 "export" 前缀（其后必须跟空白）。
func stripExport(s string) string {
	const prefix = "export"
	if !strings.HasPrefix(s, prefix) {
		return s
	}
	rest := s[len(prefix):]
	if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
		return s
	}
	return rest
}

// parseValue 根据首字符判断值的引号形式并解析。
func parseValue(key, rest string, lineNo int) (def, Error, bool) {
	d := def{key: key, line: lineNo, quote: quoteNone}
	if rest == "" {
		return d, Error{}, true
	}
	switch rest[0] {
	case '"':
		segs, err := parseDoubleQuoted(rest[1:], lineNo)
		if err.Code != "" {
			return def{}, err, false
		}
		d.quote = quoteDouble
		d.segments = segs
		return d, Error{}, true
	case '\'':
		text, err := parseSingleQuoted(rest[1:], lineNo)
		if err.Code != "" {
			return def{}, err, false
		}
		d.quote = quoteSingle
		d.segments = []segment{{kind: segLiteral, text: text}}
		return d, Error{}, true
	default:
		segs, err := parseUnquoted(rest, lineNo)
		if err.Code != "" {
			return def{}, err, false
		}
		d.segments = segs
		return d, Error{}, true
	}
}

// parseSingleQuoted 解析单引号字面值到下一个 '，不展开、不转义。
func parseSingleQuoted(s string, lineNo int) (string, Error) {
	idx := strings.IndexByte(s, '\'')
	if idx < 0 {
		return "", Error{Line: lineNo, Code: CodeUnterminatedSingle, Message: "未闭合的单引号"}
	}
	rest := strings.TrimSpace(s[idx+1:])
	if rest != "" {
		return "", Error{Line: lineNo, Code: CodeTrailingContent, Message: "多余的尾部内容: " + rest}
	}
	return s[:idx], Error{}
}

// parseDoubleQuoted 解析双引号值：处理转义、展开 ${KEY} 引用，直到闭合 "。
func parseDoubleQuoted(s string, lineNo int) ([]segment, Error) {
	var segs []segment
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			segs = append(segs, segment{kind: segLiteral, text: lit.String()})
			lit.Reset()
		}
	}
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '"':
			flush()
			if trailing := strings.TrimSpace(s[i+1:]); trailing != "" {
				return nil, Error{Line: lineNo, Code: CodeTrailingContent, Message: "多余的尾部内容: " + trailing}
			}
			return segs, Error{}
		case c == '\\':
			if i+1 >= len(s) {
				return nil, Error{Line: lineNo, Code: CodeUnterminatedDouble, Message: "未闭合的双引号"}
			}
			switch n := s[i+1]; n {
			case '"':
				lit.WriteByte('"')
			case '\\':
				lit.WriteByte('\\')
			case '$':
				lit.WriteByte('$')
			case 'n':
				lit.WriteByte('\n')
			default:
				lit.WriteByte(n)
			}
			i += 2
		case c == '$' && i+1 < len(s) && s[i+1] == '{':
			name, end, ok := readRefName(s, i+2)
			if !ok {
				return nil, Error{Line: lineNo, Code: CodeIllegalRef, Message: "非法引用名"}
			}
			flush()
			segs = append(segs, segment{kind: segRef, text: name})
			i = end
		default:
			lit.WriteByte(c)
			i++
		}
	}
	return nil, Error{Line: lineNo, Code: CodeUnterminatedDouble, Message: "未闭合的双引号"}
}

// parseUnquoted 解析未加引号的值：${KEY} 被展开，反斜杠按字面量处理，尾部空白被去除。
func parseUnquoted(s string, lineNo int) ([]segment, Error) {
	var segs []segment
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			segs = append(segs, segment{kind: segLiteral, text: lit.String()})
			lit.Reset()
		}
	}
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '$' && i+1 < len(s) && s[i+1] == '{' {
			name, end, ok := readRefName(s, i+2)
			if !ok {
				return nil, Error{Line: lineNo, Code: CodeIllegalRef, Message: "非法引用名"}
			}
			flush()
			segs = append(segs, segment{kind: segRef, text: name})
			i = end
			continue
		}
		lit.WriteByte(c)
		i++
	}
	flush()
	trimTrailingLiteral(&segs)
	return segs, Error{}
}

// trimTrailingLiteral 去除末尾字面量片段中的尾部空白，删除变空的片段。
func trimTrailingLiteral(segs *[]segment) {
	for {
		n := len(*segs)
		if n == 0 {
			return
		}
		last := &(*segs)[n-1]
		if last.kind == segRef {
			return
		}
		t := strings.TrimRight(last.text, " \t")
		if t == "" {
			*segs = (*segs)[:n-1]
			continue
		}
		last.text = t
		return
	}
}

// readRefName 从 start 读取合法变量名及闭合 }，返回变量名与 } 之后的索引。
func readRefName(s string, start int) (string, int, bool) {
	if start >= len(s) || !isKeyStart(s[start]) {
		return "", 0, false
	}
	j := start + 1
	for j < len(s) && isKeyPart(s[j]) {
		j++
	}
	if j >= len(s) || s[j] != '}' {
		return "", 0, false
	}
	return s[start:j], j + 1, true
}
