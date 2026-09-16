package mail

import (
	"testing"
	"time"
)

// TestParseMessageDateSupportsRFC3339 覆盖 days 过滤依赖的时间解析。
// 回归点:此前只按 RFC1123Z 解析,而 toMessage 写出的是 RFC3339,
// 导致 days 过滤被静默跳过。
func TestParseMessageDateSupportsRFC3339(t *testing.T) {
	got, err := parseMessageDate("2026-07-09T14:32:10+08:00")
	if err != nil {
		t.Fatalf("parseMessageDate() 解析 RFC3339 失败: %v", err)
	}
	want := time.Date(2026, 7, 9, 14, 32, 10, 0, time.FixedZone("", 8*3600))
	if !got.Equal(want) {
		t.Fatalf("parseMessageDate() = %v, want %v", got, want)
	}
}

func TestParseMessageDateSupportsRFC1123Z(t *testing.T) {
	if _, err := parseMessageDate("Thu, 09 Jul 2026 14:32:10 +0800"); err != nil {
		t.Fatalf("parseMessageDate() 解析 RFC1123Z 失败: %v", err)
	}
}

func TestParseMessageDateRejectsInvalid(t *testing.T) {
	for _, raw := range []string{"", "   ", "昨天"} {
		if _, err := parseMessageDate(raw); err == nil {
			t.Fatalf("parseMessageDate(%q) 期望报错,得到 nil", raw)
		}
	}
}

func TestSortMessagesDescUsesRealTimeAcrossOffsets(t *testing.T) {
	messages := []Message{
		{ID: "1", Date: "2026-07-09T06:00:00Z"},      // 14:00 +08:00
		{ID: "2", Date: "2026-07-09T15:00:00+08:00"}, // 07:00 UTC,最新
		{ID: "3", Date: "2026-07-09T01:00:00+08:00"}, // 前一天 17:00 UTC,最旧
	}

	sortMessagesDesc(messages)

	want := []string{"2", "1", "3"}
	for i, id := range want {
		if messages[i].ID != id {
			t.Fatalf("排序后第 %d 封为 %s,期望 %s (整体: %+v)", i, messages[i].ID, id, messages)
		}
	}
}

func TestSortMessagesDescPushesUnparsableDateLast(t *testing.T) {
	messages := []Message{
		{ID: "bad", Date: "无法解析"},
		{ID: "old", Date: "2026-07-01T10:00:00+08:00"},
		{ID: "new", Date: "2026-07-09T14:32:10+08:00"},
	}

	sortMessagesDesc(messages)

	want := []string{"new", "old", "bad"}
	for i, id := range want {
		if messages[i].ID != id {
			t.Fatalf("排序后第 %d 封为 %s,期望 %s (整体: %+v)", i, messages[i].ID, id, messages)
		}
	}
}
