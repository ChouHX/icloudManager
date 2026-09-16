package hme

import (
	"errors"
	"testing"
)

type stringError string

func (e stringError) Error() string { return string(e) }

func TestIsThrottledRecognizesRateLimits(t *testing.T) {
	cases := []string{
		"创建别名失败: HTTP 429: Too Many Requests",
		"generate 失败: rate limit exceeded",
		"连接失败: please try again later",
		"保留失败: server is temporarily unavailable",
	}
	for _, raw := range cases {
		if !IsThrottled(stringError(raw)) {
			t.Fatalf("应识别为限流: %q", raw)
		}
	}
}

func TestIsThrottledIgnoresOtherErrors(t *testing.T) {
	cases := []string{
		"连接失败: dial tcp: i/o timeout",
		"保留失败: invalid label",
		"账号未配置 Cookie，无法使用 HME 功能",
	}
	for _, raw := range cases {
		if IsThrottled(stringError(raw)) {
			t.Fatalf("不应识别为限流: %q", raw)
		}
	}
	if IsThrottled(nil) {
		t.Fatal("nil 不应识别为限流")
	}
}

func TestIsQuotaExhaustedRecognizesCapacityErrors(t *testing.T) {
	cases := []string{
		"You have already have the maximum number of email addresses",
		"保留失败: quota exceeded",
		"alias limit reached",
		"no more aliases can be created",
	}
	for _, raw := range cases {
		if !IsQuotaExhausted(stringError(raw)) {
			t.Fatalf("应识别为配额耗尽: %q", raw)
		}
	}
}

func TestIsQuotaExhaustedIgnoresTransientErrors(t *testing.T) {
	for _, raw := range []string{
		"HTTP 429: Too Many Requests",
		"连接失败: dial tcp: i/o timeout",
	} {
		if IsQuotaExhausted(stringError(raw)) {
			t.Fatalf("不应识别为配额耗尽: %q", raw)
		}
	}
	if IsQuotaExhausted(nil) {
		t.Fatal("nil 不应识别为配额耗尽")
	}
}

// TestThrottleAndQuotaAreDistinguishable 说明两类错误必须能分开判断:
// 限流要冷却后重试,配额耗尽重试无用。
func TestThrottleAndQuotaAreDistinguishable(t *testing.T) {
	throttle := stringError("HTTP 429: too many requests")
	quota := stringError("quota exceeded")

	if !IsThrottled(throttle) || IsQuotaExhausted(throttle) {
		t.Fatal("限流错误应只命中 IsThrottled")
	}
	if !IsQuotaExhausted(quota) || IsThrottled(quota) {
		t.Fatal("配额错误应只命中 IsQuotaExhausted")
	}
	if errors.Is(throttle, quota) {
		t.Fatal("两个错误不应相等")
	}
}
