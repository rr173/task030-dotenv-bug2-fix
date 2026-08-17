# task030-dotenv

.env 文件解析与变量展开服务。接收 .env 文本，解析键值对并按依赖关系展开
`${KEY}` 变量引用，支持前向引用、循环检测、未定义引用报错，以及单/双引号
语义差异与双引号转义。仅使用 Go 标准库。

## 运行

```bash
# 启动 HTTP 服务
go run . server --addr :8080

# 自检
go run . --smoke-test
```

## 接口

- `POST /parse` 请求体 `{"content": string, "base": {string: string}}`
  - 成功 `200`：`{"variables": [{"key","value"}], "count": int}`
  - 失败 `400`：`{"errors": [{"line","code","message"}]}`
- `GET /healthz`：`200 {"ok": true}`

## Docker

```bash
docker buildx build --platform linux/amd64 --load -t task030-dotenv:amd64 .
docker run --rm task030-dotenv:amd64 --smoke-test
```
