# Eino 供应商接入测试

独立 Go 模块，不修改 `shiftory-server/go.mod`，不连接 PostgreSQL 或 Redis，不接入正式导入业务。完整目标见 [AI 导入 Spec](../../docs/specs/ai-import-eino.md)。

## 配置

实际开发配置：`config/ai.development.yaml`，已创建且被 Git 忽略。样例：`config/ai.development.example.yaml`。填写每个模型的 `api_key`、`model`、`base_url`；不使用的模型设置 `enabled: false`。

`order` 越小越优先，默认 doubao=10、relay=20、deepseek=40，不能重复。可通过唯一 id 添加同一家其他模型，支持的 supplier 仅这三类。配置修改在下次运行时生效。此工具只读取所选 YAML，不加载环境变量、生产配置或 `.env`。

`response_format` 支持 prompt、json_object、json_schema。字节初始为 prompt，验证原生 JSON 支持后再修改。`thinking` 仅 Ark 支持 enabled/disabled，其余保持空。测试返回仅为 `{ "summary": "...", "issues": [] }`，用于核实识图、文字及 JSON 协议，不代表完成正式排班规则或逐日结果校验。

## 运行

从项目根目录进入工具目录：

```bash
cd tools/ai-smoke
go run test.go
```

默认仅验证配置并显示 ready／待填写状态，不发出模型调用。没有填写的启用实例允许暂留，实际调用会跳过；指定单实例时不允许缺项。

指定字节做文字接入测试：

```bash
go run test.go -run -provider doubao
```

指定字节做图片测试，需要提供本地图片：

```bash
go run test.go -run -provider doubao -image /absolute/path/schedule.png \
  -text '请阅读这张排班表，概括明确可见的日期和班次，列出无法辨认的内容。'
```

仓库也提供无业务数据的合成测试图片 `testdata/schedule.png`：10 月 1 日 09:00–18:00 工作，10 月 2 日休息，10 月 3 日 22:00–次日 06:00 工作。可将 `-image` 指向此文件验证图片输入；合成图通过不代表真实截图识别准确率。

按 order 尝试，首个有效结果结束：

```bash
go run test.go -run -provider auto
```

逐个调用所有配置完整、启用且支持当前输入的模型：

```bash
go run test.go -run -provider all
```

`all` 模式整轮仍有超时上限，不完整／禁用的实例不会调用，先检查启动时列表；可单独指定 id 排查。此工具只实现单轮顺序尝试，不实现 Asynq 重试、共享熔断或全局并发限制，这些字段是正式重构配置。

使用样例检查或指定其他文件：

```bash
go run test.go -config ../../config/ai.development.example.yaml
```

网络调用可能消耗供应商额度。错误只显示安全分类及 HTTP 状态，不打印上游错误正文和密钥。成功时显示结果；不写入任何排班。修改 order、请求超时或新增模型后，整轮上限必须大于启用实例超时总和，停机等待必须大于整轮期限。

## 非联网验证

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
```

测试使用本地 HTTP 模拟服务及假密钥，验证两种适配器的图片内容块、三种输出模式、参数传递、单次 503 不在 SDK 内重试、取消、配置边界及输出结构。不证明真实账号权限、套餐资格、识图质量、任务恢复或正式业务一致性。
