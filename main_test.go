package main

import (
	"testing"
	"time"
)

func TestParseSessionTTLDefaults(t *testing.T) {
	got, err := parseSessionTTL("")
	if err != nil {
		t.Fatalf("空值应回落默认 12h,得到错误: %v", err)
	}
	if got != 12*time.Hour {
		t.Fatalf("默认值 = %v, want 12h", got)
	}
}

func TestParseSessionTTLAcceptsValidRange(t *testing.T) {
	for _, raw := range []string{"15m", "12h", "168h"} {
		if _, err := parseSessionTTL(raw); err != nil {
			t.Fatalf("parseSessionTTL(%q) 应为合法值,得到错误: %v", raw, err)
		}
	}
}

// TestParseSessionTTLRejectsOutOfRange 回归点:越界值此前会静默回落 12h,
// 启动日志毫无提示,配置与生效值不一致。
func TestParseSessionTTLRejectsOutOfRange(t *testing.T) {
	for _, raw := range []string{"1m", "169h", "-1h"} {
		if _, err := parseSessionTTL(raw); err == nil {
			t.Fatalf("parseSessionTTL(%q) 应返回错误,得到 nil", raw)
		}
	}
}

func TestParseSessionTTLRejectsGarbage(t *testing.T) {
	if _, err := parseSessionTTL("十二小时"); err == nil {
		t.Fatal("非法时长字符串应返回错误")
	}
}
