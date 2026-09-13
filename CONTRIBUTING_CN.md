# 贡献指南

[English](CONTRIBUTING.md)

使用 Go 1.26 或 1.27。先阅读[架构](docs/architecture_CN.md)、[规则契约](docs/rules_CN.md)和[测试](docs/testing_CN.md)。

修改前检查 `git status`，保留无关变更。选择满足契约的最简单实现，优先复用既有 Go 依赖。后端源码文件保持在 2,000 行以内，架构边界变化需要同步文档。

## 验证修改

```sh
gofmt -w <修改的Go文件>
go test ./...
go build -o go-inject ./cmd/go-inject
python examples/check.py --tool ./go-inject
```

Windows 使用 `.exe` 文件名。嵌套示例是独立模块，根目录 `go test ./...` 不会运行它们。修改加载、改写、依赖和后端时，需要运行真实示例 CLI 验证。

针对可观察故障增加回归测试，检查运行结果，不能只检查改写文本。避免固定端口、常驻服务、外部凭据，以及修改用户模块缓存。临时夹具成功后清理，失败时保留有用诊断。

## 提交修改

描述具体问题和最终行为，附复现、相关验证和未运行测试。中英文文档保持一致。公开规则示例应包括目标版本范围和可独立运行的行为测试。

不为已移除的实验接口增加兼容别名。除非帮助用户作出有意义的选择，不要将编译器实现细节变成日常配置。

## 发布

当前 beta 展示名称为 `beta-0.2`，版本为 `v0.1.0-beta.2`。工具安装路径是 `github.com/kakj-go/go-inject/cmd/go-inject`。嵌套规则模块单独使用 `examples/rules/v0.1.0-beta.2` 标签，使用方依赖 `github.com/kakj-go/go-inject/examples/rules v0.1.0-beta.2`。

同步更新 [CHANGELOG.md](CHANGELOG.md)、安装示例与发布默认版本。在最终 master 提交上通过源码 CI 和按 SHA 的干净远程安装验证；运行发行包工作流，要求十二项原生二进制与工具链组合全部通过。在同一提交为两个模块创建标签，不移动已有标签；将实际受测的归档与校验和上传到预发布草稿，验证按标签安装后再发布。发布后复验公开下载。
