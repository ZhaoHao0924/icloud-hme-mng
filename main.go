// Command icloud-hme 启动 iCloud Hide My Email 多账号管理平台。
//
// 两个核心 HTTP 接口:
//
//	POST /api/create  — 创建隐私邮箱别名
//	GET  /api/inbox   — 读取邮件
//
// 用法:
//
//	./icloud-hme                              # 默认 127.0.0.1:8081
//	./icloud-hme -addr 127.0.0.1:9000         # 指定本机端口
//	ICLOUD_HME_API_TOKEN=... ./icloud-hme \
//	  -addr 0.0.0.0:8081                     # 带鉴权远程监听
//	./icloud-hme -data ./data       # 指定数据目录
//	./icloud-hme -debug             # 调试模式
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"icloud-hme/internal/account"
	"icloud-hme/internal/server"
)

const (
	defaultListenAddress = "127.0.0.1:8081"
	apiTokenEnv          = "ICLOUD_HME_API_TOKEN"
	minAPITokenLength    = 32
)

// version 可在构建时通过 -ldflags "-X main.version=<version>" 注入。
var version = "dev"

func main() {
	addr := flag.String("addr", defaultListenAddress, "HTTP 监听地址")
	dataDir := flag.String("data", "./data", "数据目录 (accounts.json 存放位置)")
	debug := flag.Bool("debug", false, "调试模式 (启用 Gin 调试日志)")
	flag.Parse()

	apiToken := strings.TrimSpace(os.Getenv(apiTokenEnv))
	if err := validateAccessConfiguration(*addr, apiToken); err != nil {
		log.Fatalf("访问控制配置错误: %v", err)
	}

	log.Printf("iCloud Hide My Email 服务启动 version=%s addr=%s", version, *addr)
	if apiToken == "" {
		log.Printf("API 访问范围: 仅本机")
	} else {
		log.Printf("API Bearer Token 鉴权已启用")
	}

	abs, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatalf("数据目录路径错误: %v", err)
	}

	mgr, err := account.NewManager(abs)
	if err != nil {
		log.Fatalf("初始化账号管理器失败: %v", err)
	}
	count := len(mgr.ListAccounts())
	log.Printf("账号加载完成 count=%d data_dir=%s", count, abs)

	srv := server.NewWithVersion(mgr, *debug, apiToken, version)

	log.Printf("HTTP 服务就绪 addr=%s", *addr)
	if err := srv.Run(*addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

func validateAccessConfiguration(addr, apiToken string) error {
	apiToken = strings.TrimSpace(apiToken)
	if apiToken != "" && len(apiToken) < minAPITokenLength {
		return fmt.Errorf("环境变量 %s 至少需要 %d 个字符", apiTokenEnv, minAPITokenLength)
	}
	if isLoopbackListenAddress(addr) || apiToken != "" {
		return nil
	}
	return fmt.Errorf("非本机监听地址 %q 必须设置环境变量 %s", addr, apiTokenEnv)
}

func isLoopbackListenAddress(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
