# 使用指南

[English](usage.md) · [README](../README_CN.md)

## 构建和测试

```sh
go-inject build [Go build 参数] [包]
go-inject test [Go test 参数] [包]
```

在 Go 项目中运行命令，使用平时传给 Go 的目标和参数：

```sh
go-inject build -o server ./cmd/server
go-inject build -tags enterprise ./cmd/server
go-inject test -count=1 ./cmd/server
go-inject test ./...
```

每个选中的包都是规则选择入口。`cmd/server` 中的登记适用于该服务及其依赖，不会自动为每个库的独立测试选择规则。测试某个库本身时，在该库目录增加登记文件。

私有依赖和本地开发继续使用 Go 模块设置。`replace`、`go.work` 指向本地源码，版本和校验信息保留在模块文件中。登记文件使用 `//go:build goinject`，因此 `go mod tidy` 保留它的 imports，而普通应用编译排除该文件。不要手动给最终应用添加 `goinject` 构建标签。

## 登记和聚合

入口的登记文件包含静态 imports：

```go
//go:build goinject

package main

import (
    _ "example.com/app/inject/local"
    _ "example.com/team/rules/all"
)
```

聚合包使用相同的带标签文件导入子规则包。同一规则重复发现时去重。模板或 helper 库中的普通 import 只是代码依赖，不会启用更多规则。[external 示例](../examples/external/README_CN.md) 同时验证聚合和去重。

## 保留并查看构建

```sh
go-inject build -work -o server ./cmd/server
go-inject inspect --json <构建会话目录>
```

`-work` 保留会话并打印目录。查看会话可读取已保存的结果，包括源码位置和生成文件。这些是为本次构建准备的源码，不是另外计算的预览。排错时保留目录，完成后自行删除。

`go-inject version` 输出工具版本。报告问题时，请附工具版本、`go version`、目标命令及相关会话信息。

## 生成 vendor 源码

```sh
go-inject vendor ./cmd/server
go build -mod=vendor -o server ./cmd/server
go test -mod=vendor ./cmd/server
go-inject vendor --restore
```

vendor 模式修改磁盘上的第三方依赖源码，并记录原始状态，供 `--restore` 撤销工具写入的变更。已有用户修改和生成文件中的后续修改需要明确处理。重复执行同一生成应得到相同结果。

一个物理 vendor 目录只能保存一套结果。多个入口要求同一个包生成不同源码时，会拒绝共同生成。需要同时保留两套结果时使用不同项目副本。普通 `go build -mod=vendor` 读取当前磁盘文件，不会重新选择规则。

应用主模块、main 初始化、标准库和 runtime 目标使用 `build` 或 `test`，vendor 会拒绝这些目标。[Gin 示例](../examples/gin/README_CN.md) 展示了第三方依赖的原生 Go 编译和恢复操作。

`-toolexec` 由工具管理。多个入口不能共享单个 `-o` 或 profile 输出路径，需要分别运行。用户 overlay 和支持的 Go 构建、测试参数会传入构建上下文。

## 理解诊断

| 结果 | 含义 | 下一步 |
|---|---|---|
| 不适用 | 入口没有依赖目标包 | 检查入口和登记 imports |
| 目标缺失 | 包存在，但文件、函数或类型不存在 | 根据依赖版本更新规则 |
| 签名或投影失配 | 模板与目标 API 或字段不一致 | 对照预期和实际声明 |
| 没有版本变体 | 没有实现支持实际版本 | 使用支持该依赖的规则版本 |
| 多个版本变体 | 多个实现同时适用 | 使版本区间或构建条件互斥 |
| 声明冲突 | 新增声明或字段发生冲突 | 改名或去掉冲突规则 |
| 依赖循环 | 新增 imports 会形成环 | 将目标专属 helper 留在目标包 |
| vendor 冲突 | 入口或本地修改不能共享结果 | 处理修改或使用独立目录 |

适用目标发生失配时，操作失败。Go 编译成功本身不能证明请求的注入已经发生。

## 编辑器设置

模板是 Go 源码，但投影描述另一个包，版本变体描述不同目标。以工具在真实目标中的验证为准。编辑器需要查看登记 imports 时，可配置 `goinject` 标签；不要将该标签加入普通应用构建。
