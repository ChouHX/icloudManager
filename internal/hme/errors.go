// Package hme - iCloud 侧错误分类。
//
// iCloud 对别名创建同时有两类限制,恢复方式不同,必须区分:
//
//	限流(rate limit):窗口内请求过多,等待窗口重置后可继续
//	配额耗尽(quota):账号别名总量已达上限,继续重试不会成功
package hme

import "strings"

// throttleHints 指向"稍后可恢复"的限流特征。
var throttleHints = []string{
	"429", "too many", "rate limit", "ratelimit", "throttl",
	"temporarily", "try again", "slow down",
}

// quotaHints 指向"总量已达上限"的配额特征。
var quotaHints = []string{
	"quota", "maximum number of", "limit reached", "no more",
	"already have the maximum", "reached the limit", "full",
}

// IsThrottled 判断错误是否由速率限制引起(等待后可恢复)。
//
// 判定基于 iCloud 返回的文本(HTTP 状态码或 errorMessage),
// 因此这里只做保守的特征匹配,调用方仍应保留原始错误用于展示。
func IsThrottled(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, hint := range throttleHints {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}

// IsQuotaExhausted 判断错误是否表示账号别名总量已达上限。
//
// 优先于 IsThrottled 使用:配额耗尽的提示里也可能出现 "limit" 之类的词。
func IsQuotaExhausted(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, hint := range quotaHints {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}
