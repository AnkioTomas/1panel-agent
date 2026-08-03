// Package buildinfo 存放构建期注入的版本号（-ldflags -X）。
package buildinfo

// Version 由发布构建注入，例如 -X 1panel-agent/internal/buildinfo.Version=v0.1.0。
var Version = "dev"
