# dotenv 配置解析与变量展开服务

这是一个纯 Go 的 `.env` 文本解析服务。它通过 HTTP 接收配置文本，解析键值对，按依赖关系展开 `${KEY}` 引用，并返回解析后的变量或结构化错误；同时支持前向引用、循环检测、引号语义、JSON 请求校验和内置自检。

## 标准构建、运行和测试

在本目录执行：

```bash
go build ./...                  # 编译全部包
go run . server --addr :8080   # 启动 HTTP 服务
go test ./...                   # 运行单元测试
go vet ./...                   # 执行静态检查
go run . --smoke-test          # 执行无需外部服务的自检并退出
```

服务入口是根目录的 `main.go`。`GET /healthz` 用于健康检查，`POST /parse` 接收 `{"content": string, "base": {string: string}}` 并返回解析结果。

## Benzhi 镜像

`build_benzhi_docker.sh` 固定使用 `benzhi.Dockerfile`，参数分别是镜像名和平台，默认值为 `my-project` 与 `linux/amd64`。镜像构建使用 Go 1.26.3，并在构建阶段执行 `go build ./...`；容器启动后进入 shell。

```bash
bash ./build_benzhi_docker.sh task030-dotenv:amd64 linux/amd64
bash ./build_benzhi_docker.sh task030-dotenv:arm64 linux/arm64
docker run -it task030-dotenv:amd64
```
