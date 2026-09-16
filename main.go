// Command icloud-hme 启动 iCloud Hide My Email 多账号管理平台。
//
// 两个核心 HTTP 接口:
//
//	POST /api/create  — 创建隐私邮箱别名
//	GET  /api/inbox   — 读取邮件
//
// 用法:
//
//	./icloud-hme                    # 默认 :8081
//	./icloud-hme -addr :9000        # 指定端口
//	./icloud-hme -data ./data       # 指定数据目录
//	./icloud-hme -debug             # 调试模式
//	./icloud-hme -log-level debug   # 日志级别 (debug/info/warn/error)
//
// 安全配置(必填):
//
//	ICLOUD_HME_ADMIN_PASSWORD      管理员密码,至少 8 字符(进程启动后从环境清除)
//	ICLOUD_HME_SESSION_TTL         会话有效期,默认 12h,范围 15m-168h
//	ICLOUD_HME_SECURE_COOKIE       TLS 反向代理部署时设为 true
//	ICLOUD_HME_IMAP_POOL_SIZE      每账号 IMAP 并发连接数,默认 10(范围 1-50)
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"icloud-hme/internal/account"
	"icloud-hme/internal/server"
)

func main() {
	addr := flag.String("addr", ":8081", "HTTP 监听地址")
	dataDir := flag.String("data", "./data", "数据目录 (accounts.json 存放位置)")
	debug := flag.Bool("debug", false, "调试模式 (启用 Gin 调试日志)")
	flag.Parse()

	adminPassword := os.Getenv("ICLOUD_HME_ADMIN_PASSWORD")
	if len(adminPassword) < 8 {
		log.Fatal("请通过环境变量 ICLOUD_HME_ADMIN_PASSWORD 设置管理员密码(至少 8 个字符)")
	}
	sessionTTL, err := parseSessionTTL(os.Getenv("ICLOUD_HME_SESSION_TTL"))
	if err != nil {
		log.Fatalf("ICLOUD_HME_SESSION_TTL 无效: %v", err)
	}
	secureCookie := os.Getenv("ICLOUD_HME_SECURE_COOKIE") == "true"

	log.Printf("iCloud Hide My Email 服务启动 addr=%s", *addr)

	abs, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatalf("数据目录路径错误: %v", err)
	}

	mgr, err := account.NewManager(abs)
	if err != nil {
		log.Fatalf("初始化账号管理器失败: %v", err)
	}
	defer mgr.Close()
	count := len(mgr.ListAccounts())
	log.Printf("账号加载完成 count=%d data_dir=%s", count, abs)

	srv, err := server.New(mgr, server.Config{
		Debug:         *debug,
		AdminPassword: adminPassword,
		SessionTTL:    sessionTTL,
		SecureCookie:  secureCookie,
		DataDir:       abs,
	})
	if err != nil {
		log.Fatalf("初始化服务失败: %v", err)
	}

	// 密码只用于初始化认证,随后立即从进程环境清除
	_ = os.Unsetenv("ICLOUD_HME_ADMIN_PASSWORD")

	log.Printf("HTTP 服务就绪 addr=%s", *addr)
	if err := srv.Run(*addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

// parseSessionTTL 解析会话有效期,默认 12h,范围 15m-168h。
func parseSessionTTL(raw string) (time.Duration, error) {
	if raw == "" {
		return 12 * time.Hour, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if d < 15*time.Minute || d > 168*time.Hour {
		return 0, fmt.Errorf("超出允许范围 15m-168h: %s", raw)
	}
	return d, nil
}
