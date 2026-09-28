# 参与开发

NAS Wallboard 只支持 TrueNAS SCALE 25.10 及更高版本。提交改动前请先开 Issue 说明用途；不要加入动态下载并执行的插件、TrueNAS 写操作或新的平台适配层。

本地验证：

```sh
go test -race ./...
go vet ./...
npm ci
npm test
npm run test:e2e
dotnet test windows/NASWallboard.Desktop.sln -c Release
go test ./tests -count=1
```

所有 UI 文案优先使用清晰中文。测试数据只能使用虚构地址和假密钥，日志与失败输出不得包含用户提交的密码、Token、Cookie、API Key 或硬盘序列号。
