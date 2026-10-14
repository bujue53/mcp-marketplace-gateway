# OpenAPI 导入、请求映射与验证

## 本轮完成范围

| 场景 | 已实现 |
| --- | --- |
| 文档格式 | OpenAPI 3.0.x / 3.1.x，JSON 对象或 JSON/YAML 文本字符串 |
| 本地引用 | components 中的参数、请求体、响应、Schema 与安全方案引用；Schema 递归引用改写为工具内部 `$defs` |
| 公共参数 | 合并 Path Item 与 Operation 参数，同一 name/in 由 Operation 覆盖；检查重复声明和未声明的路径占位符 |
| HTTP 方法 | GET、POST、PUT、PATCH、DELETE、HEAD、OPTIONS |
| 参数映射 | path/header 的 simple；query 的 form、deepObject、spaceDelimited、pipeDelimited；标量 cookie |
| 参数值 | 标量；查询、路径、请求头的一层数组/对象；deepObject 支持一层标量属性 |
| 请求体 | application/json、application/*+json、application/x-www-form-urlencoded、text/plain；固定优先级选择媒体类型 |
| 响应元数据 | 保存成功响应或 default 的真实 Schema，以及 Schema 引用定义 |
| 上游认证 | HTTP Bearer；header/query/cookie API Key；security 的 OR/AND 与操作级覆盖/匿名调用 |
| 地址选择 | base_url 覆盖 > Operation servers > Path Item servers > 根 servers；使用 server variables 的 default |
| 重启恢复 | 请求映射存入 PostgreSQL，网关重启后首次调用重新装载；调用凭据由调用者提供 |
| 审计摘要 | 隐藏 auth、headers、cookies 的值；网络请求错误去掉含 query 凭据的 URL 外层 |
| 生成代码 | Go 与网关共用请求映射源代码，TypeScript 实现对应规则；生成项目可独立编译运行 |

参数覆盖和 security 组合语义参照 [OpenAPI 3.1 官方规范](https://spec.openapis.org/oas/v3.1.0.html)。本项目实现以上常见导入与调用场景。

## 导入方法

接口保持 JSON 请求格式，`spec` 可以是对象，也可以是文档文本。例如在项目根目录运行：

```powershell
$login = Invoke-RestMethod 'http://localhost:8080/api/auth/login' -Method Post -ContentType 'application/json' -Body '{"username":"admin","password":"admin123"}'
$headers = @{Authorization="Bearer $($login.token)"}
$spec = Get-Content -Raw 'specs/weather-v1.json'
$body = @{spec=$spec; base_url='http://legacy-api:9002'} | ConvertTo-Json -Depth 50
Invoke-RestMethod 'http://localhost:8080/api/servers/generate' -Method Post -ContentType 'application/json' -Headers $headers -Body $body
```

YAML 使用同样方式读取 `.yaml` 文件。注册到 Docker 网关时，地址必须能从网关容器访问；生成独立代码时使用该代码运行环境能访问的地址。文档已有绝对服务器地址时可省略 `base_url`。

生成 Go/TS 项目使用 `POST /api/wrapper/skeleton`，增加 `lang='go'` 或 `'ts'`；保存与启动步骤见 `骨架代码生成与验证.md`。独立服务可用 `REST_BASE_URL` 覆盖全部操作地址，也可用 `REST_BEARER_TOKEN` 提供 Bearer 默认值。

## 调用参数

普通 path/query 参数保留在顶层，请求头放入 `headers`，cookie 放入 `cookies`，请求体放入 `body`，上游凭据放入 `auth`（键为安全方案名称）：

```json
{
  "id": "用户 /?#汉",
  "tags": ["one", "two"],
  "filter": {"active": true},
  "headers": {"X-Tenant": "tenant", "X-Flags": ["one", "two"]},
  "cookies": {"session": "value"},
  "auth": {"apiKeyScheme": "上游凭据"}
}
```

同名 path/query 参数分别放到 `path` 和 `query` 对象；名称与 `body/headers/cookies/path/query/auth` 冲突时，也使用所属位置对象。以生成出的 `inputSchema` 为准。

deepObject 参数需在文档中显式设置 `explode: true`；spaceDelimited/pipeDelimited 只处理数组且使用 `explode: false`。cookie 标量采用表单百分号编码，接收方应按同样规则解码。

## 明确的边界

- 外部文件/URL 引用需先合并进文档；只处理文档内的 JSON Pointer。未定义引用和非 Schema 循环引用返回错误。
- 不支持 OAuth2/OIDC 授权流程、HTTP Basic、multipart 文件上传、参数 content/allowReserved、自定义请求体 encoding、matrix/label 参数、复杂 cookie、TRACE、GET/HEAD 请求体。这些已识别的场景会返回错误。
- 保留 Schema 定义与约束，不提供完整 OpenAPI 文档校验器或完整 JSON Schema 调用值校验器；不执行 callbacks/webhooks。Schema 自定义 `$id`/`$anchor`/`$dynamicRef` 等高级引用不在本轮覆盖范围。
- 旧版本只保存在内存的自定义导入，需要重新导入一次以保存映射；新的导入可在网关重启后恢复。

## 自动验证与结果

在项目根目录执行：

```powershell
docker compose up --build --no-deps -d gateway
node tools/verify-skeletons.mjs
```

脚本执行真实 Docker 网关的导入、REST/MCP 调用、响应 Schema 检查、无效媒体类型拒绝、网关重启恢复与凭据摘要隐藏检查；随后编译并启动生成的 Go、TypeScript 项目，向临时 REST 服务发起真实请求。它会重启网关一次。默认测试账号为 admin，可通过 `GATEWAY_URL`、`MCP_USERNAME`、`MCP_PASSWORD` 配置。

后端测试可在 `gateway` 目录执行 `go test ./...`。已缓存全部依赖但网络无法连接模块代理时，可以临时设置 `$env:GOPROXY='off'` 后测试。

2026-10-15 验证通过：全量 Go 测试、Docker 构建、网关真实 REST/MCP 调用与重启恢复、两种生成项目的编译/启动/调用。机器结果见 `OpenAPI兼容性联调结果.json`。

脚本停止临时服务、删除临时项目目录，并按本次 server ID 和 trace ID 清理注册、映射与日志。原有项目数据保留，全部功能完成后再执行整体清理。

## 后续三项的完成记录

Agent Skills 规范包、ClickHouse 调用日志分析、2000 QPS/P99 性能与真实 Agent 任务均已完成并验收。详情见 `三项功能实现与验收.md` 和 `最终三项验收结果.json`。本页列出的 OpenAPI 高级支持边界保留。
