# 仓库与发布脚本

[English](README.md) · [测试](../docs/testing_CN.md)

这些脚本需要 Python 3.10 及以上，只使用标准库，在仓库副本中运行。

| 脚本 | 用途 |
|---|---|
| `check_repository.py` | 检查仓库内 Markdown 链接及锚点，并限制首方 Go 文件少于 2,000 行 |
| `configure_cgo.py` | 定位并实际运行原生 GCC/Clang，再导出 `CC`、`CGO_ENABLED=1` 及适用的 macOS SDK |
| `check_toolchain.py` | 拒绝与原生任务不一致的 Go 版本、主机/目标架构、CGO 设置或工具链策略 |
| `package_release.py` | 使用 Go 1.27.1 构建六个平台的无 CGO 二进制，生成可复现归档及校验文件 |
| `verify_release.py` | 检查归档字节、成员、内嵌元数据及当前 Go 工具链下的真实二进制行为 |
| `verify_install.py` | 使用空缓存安装远程 CLI/规则模块的固定版本并验证聚合规则行为 |

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
| macOS amd64 | `macos-15-intel` | `xcrun --sdk macosx --find clang` 及对应 SDK |
| macOS arm64 | `macos-15` | `xcrun --sdk macosx --find clang` 及对应 SDK |

编译器配置输出实际版本，并编译、运行使用标准头文件的 C 程序；Unix 同时验证 pthread。在 macOS 上，将 `xcrun --sdk macosx --show-sdk-path` 得到的 `SDKROOT` 同时传给探测程序及后续 Go 进程。头文件、SDK 或原生运行能力缺失时配置失败，不会静默关闭 CGO。

Unix 不会把编译器目录前置到 PATH，避免 `/usr/bin/go` 覆盖 setup-go。只有 Windows 为 GCC 运行时 DLL 增加编译器目录，并保持所选 Go 目录优先。随后校验实际 Go 可执行文件、精确版本、主机和目标系统/架构、`GOTOOLCHAIN=local` 及预期 CGO 设置。发布构建和归档验证任务也执行同样的检查。

独立五平台任务使用两个 Go 版本运行 `go test -race ./internal/...`，不会用交叉编译代替原生执行。Linux amd64 额外验证两个 Gin 依赖基线。

## 本地构建归档

激活 Go 1.27.1 后运行：

```sh
python scripts/package_release.py --version v0.1.0-beta.1 --output dist
```

脚本为 Linux、Windows、macOS 的 amd64/arm64 构建二进制，固定 `CGO_ENABLED=0`、`-trimpath`、`-buildvcs=false`，通过链接参数写入 `main.version` 和完整 `main.commit`。归档包含可执行文件、`README.md`、`README_CN.md`、`LICENSE` 和 `THIRD_PARTY_LICENSES.txt`。Windows 使用 ZIP，其余使用 tar.gz；`SHA256SUMS` 覆盖六个归档，不覆盖已有同名产物。

归档时间使用 `SOURCE_DATE_EPOCH` 或提交时间。参数可通过 `python scripts/package_release.py --help` 查看。交叉构建成功本身不证明运行支持。

## 验证实际发布产物

在对应原生主机上使用指定 Go 版本：

```sh
python scripts/verify_release.py --directory dist --version v0.1.0-beta.1 --commit <完整提交ID> --goos linux --goarch amd64 --go-version 1.26.8
```

验证器检查 `SHA256SUMS`，只接受预期的普通文件成员，验证内嵌版本、提交及无 CGO 的 Go 1.27.1 构建元数据，再使用解压出的原始二进制运行 basic 和 HTTP 示例，不会重新构建待测工具。

[Release artifacts](../.github/workflows/release-artifacts.yml)只构建一次，并在十二个原生验证任务中下载同一批归档：六个平台分别使用两个支持的 Go 版本。工作流只上传 Actions artifacts，仓库权限为只读，不创建、公开或更新 GitHub Release。

`v0.1.0-beta.1` 的公开标题为 `beta-0.1`，标记 prerelease。对应工作流及源码 CI 通过后再发布。Release 公开和嵌套规则模块标签由维护者单独操作。
