# 仓库与发布脚本

[English](README.md) · [测试](../docs/testing_CN.md)

这些脚本需要 Python 3.10 及以上，只使用标准库，在仓库副本中运行。

| 脚本 | 用途 |
|---|---|
| `check_repository.py` | 检查仓库内 Markdown 链接及锚点，并限制首方 Go 文件少于 2,000 行 |
| `configure_cgo.py` | 定位原生 GCC/Clang，将 `CC`、编译器目录及 `CGO_ENABLED=1` 写入 GitHub Actions 环境 |
| `package_release.py` | 使用 Go 1.27.1 构建六个平台的无 CGO 二进制，生成可复现归档及校验文件 |
| `verify_release.py` | 检查归档字节、成员、内嵌元数据及当前 Go 工具链下的真实二进制行为 |

```sh
python scripts/check_repository.py
python -m unittest discover -s scripts -p 'test_*.py'
```

## CI

[CI](../.github/workflows/ci.yml)在六个平台分别使用 Go `1.26.8`、`1.27.1` 运行原生测试和可执行示例，固定 `GOTOOLCHAIN=local`：

| 系统/架构 | Runner | C 编译器 |
|---|---|---|
| Linux amd64 | `ubuntu-24.04` | 原生 GCC |
| Linux arm64 | `ubuntu-24.04-arm` | 原生 GCC |
| Windows amd64 | `windows-2025` | Runner 镜像提供的原生 GCC |
| Windows arm64 | `windows-11-arm` | 关闭 CGO，不运行 race |
| macOS amd64 | `macos-15-intel` | `xcrun --find clang` |
| macOS arm64 | `macos-15` | `xcrun --find clang` |

缺少必需编译器时配置失败，并输出实际编译器版本。独立的五平台 Go 1.27.1 任务运行 `go test -race ./internal/...`，不会用交叉编译代替原生执行。Linux amd64 额外验证两个 Gin 依赖基线。

## 本地构建归档

激活 Go 1.27.1 后运行：

```sh
python scripts/package_release.py --version v0.1.0-beta.1 --output dist
```

脚本为 Linux、Windows、macOS 的 amd64/arm64 构建二进制，固定 `CGO_ENABLED=0`、`-trimpath`、`-buildvcs=false`，通过链接参数写入 `main.version` 和完整 `main.commit`。归档包含可执行文件、`README.md`、`README_CN.md`、`LICENSE`。Windows 使用 ZIP，其余使用 tar.gz；`SHA256SUMS` 覆盖六个归档，不覆盖已有同名产物。

归档时间使用 `SOURCE_DATE_EPOCH` 或提交时间。参数可通过 `python scripts/package_release.py --help` 查看。交叉构建成功本身不证明运行支持。

## 验证实际发布产物

在对应原生主机上使用指定 Go 版本：

```sh
python scripts/verify_release.py --directory dist --version v0.1.0-beta.1 --commit <完整提交ID> --goos linux --goarch amd64 --go-version 1.26.8
```

验证器检查 `SHA256SUMS`，只接受预期的普通文件成员，验证内嵌版本、提交及无 CGO 的 Go 1.27.1 构建元数据，再使用解压出的原始二进制运行 basic 和 HTTP 示例，不会重新构建待测工具。

[Release artifacts](../.github/workflows/release-artifacts.yml)只构建一次，并在十二个原生验证任务中下载同一批归档：六个平台分别使用两个支持的 Go 版本。工作流只上传 Actions artifacts，仓库权限为只读，不创建、公开或更新 GitHub Release。

`v0.1.0-beta.1` 的公开标题为 `beta-0.1`，标记 prerelease。对应工作流及源码 CI 通过后再发布。Release 公开和嵌套规则模块标签由维护者单独操作。
