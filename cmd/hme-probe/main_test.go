package main

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"icloud-hme/internal/hme"
)

func TestParseAliasTimeHandlesTimestampMagnitudes(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Time
	}{
		{"1750000000", time.Unix(1750000000, 0)},               // 秒
		{"1750000000000", time.UnixMilli(1750000000000)},       // 毫秒
		{"1750000000000000", time.UnixMicro(1750000000000000)}, // 微秒
		{"2026-09-16T10:30:00+08:00", time.Date(2026, 9, 16, 10, 30, 0, 0, time.FixedZone("", 8*3600))},
		{"2026-09-16 10:30:00", time.Date(2026, 9, 16, 10, 30, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		got, ok := parseAliasTime(tc.raw)
		if !ok {
			t.Fatalf("parseAliasTime(%q) 解析失败", tc.raw)
		}
		if !got.Equal(tc.want) {
			t.Fatalf("parseAliasTime(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestParseAliasTimeRejectsInvalid(t *testing.T) {
	for _, raw := range []string{"", "   ", "昨天", "not-a-time"} {
		if _, ok := parseAliasTime(raw); ok {
			t.Fatalf("parseAliasTime(%q) 应当失败", raw)
		}
	}
}

func TestMaxInWindowFindsDensestInterval(t *testing.T) {
	base := time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)
	// 10:00:00 起连续 5 个(间隔 5s),随后空闲 10 分钟,再来 2 个
	times := []time.Time{
		base,
		base.Add(5 * time.Second),
		base.Add(10 * time.Second),
		base.Add(15 * time.Second),
		base.Add(20 * time.Second),
		base.Add(10 * time.Minute),
		base.Add(10*time.Minute + 5*time.Second),
	}

	count, start, end := maxInWindow(times, time.Minute)
	if count != 5 {
		t.Fatalf("1 分钟窗口应命中 5 个,得到 %d", count)
	}
	if !start.Equal(base) || !end.Equal(base.Add(20*time.Second)) {
		t.Fatalf("区间端点错误: %s → %s", start, end)
	}

	count, _, _ = maxInWindow(times, 10*time.Second)
	if count != 3 {
		t.Fatalf("10 秒窗口应命中 3 个,得到 %d", count)
	}
}

func TestMaxInWindowEdgeCases(t *testing.T) {
	if count, _, _ := maxInWindow(nil, time.Minute); count != 0 {
		t.Fatalf("空序列应为 0,得到 %d", count)
	}
	now := time.Now()
	if count, start, _ := maxInWindow([]time.Time{now}, time.Minute); count != 1 || !start.Equal(now) {
		t.Fatalf("单元素序列应返回 1 且端点为其本身,得到 %d %s", count, start)
	}
	// 完全相同的时间戳都应计入同一窗口
	dup := []time.Time{now, now, now}
	if count, _, _ := maxInWindow(dup, time.Second); count != 3 {
		t.Fatalf("同时间戳应全部计入,得到 %d", count)
	}
}

func TestParseWindowsValidates(t *testing.T) {
	windows, err := parseWindows("10s, 1m ,2h")
	if err != nil {
		t.Fatalf("parseWindows 失败: %v", err)
	}
	if len(windows) != 3 || windows[0] != 10*time.Second || windows[1] != time.Minute || windows[2] != 2*time.Hour {
		t.Fatalf("解析结果异常: %v", windows)
	}

	// 尾随或多余逗号被容忍,便于手输
	if got, err := parseWindows("1m,,"); err != nil || len(got) != 1 || got[0] != time.Minute {
		t.Fatalf("尾随逗号应被容忍,得到 %v / %v", got, err)
	}

	for _, raw := range []string{"abc", "0s", "-5m", "", "  ", ",,"} {
		if _, err := parseWindows(raw); err == nil {
			t.Fatalf("parseWindows(%q) 应当报错", raw)
		}
	}
}

func TestShouldStopReusesSharedClassification(t *testing.T) {
	stop := []string{
		"创建别名失败: HTTP 429: too many requests",
		"保留失败: You have exceeded the maximum number of aliases",
	}
	for _, msg := range stop {
		if !shouldStop(errFromString(msg)) {
			t.Fatalf("%q 应触发停止", msg)
		}
	}

	keep := []string{
		"连接失败: dial tcp: i/o timeout",
		"保留失败: invalid label",
	}
	for _, msg := range keep {
		if shouldStop(errFromString(msg)) {
			t.Fatalf("%q 不应触发停止", msg)
		}
	}
	if shouldStop(nil) {
		t.Fatal("nil 不应触发停止")
	}
}

type stringError string

func (e stringError) Error() string { return string(e) }

func errFromString(s string) error { return stringError(s) }

func TestFormatWindow(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second: "30s",
		2 * time.Minute:  "2m",
		time.Hour:        "1h",
		24 * time.Hour:   "1d",
		90 * time.Minute: "90m",
	}
	for input, want := range cases {
		if got := formatWindow(input); got != want {
			t.Fatalf("formatWindow(%v) = %q, want %q", input, got, want)
		}
	}
}

func TestReportDensityOutputsWindowPeaks(t *testing.T) {
	previousWindows := *windowList
	*windowList = "10s,1m"
	defer func() { *windowList = previousWindows }()

	// 全部使用本地时区,五个点分布在 35 秒内
	base := time.Date(2026, 9, 16, 10, 0, 0, 0, time.Local)
	offsets := []time.Duration{0, 5 * time.Second, 10 * time.Second, 30 * time.Second, 35 * time.Second}
	aliases := make([]hme.Alias, 0, len(offsets)+1)
	for i, offset := range offsets {
		aliases = append(aliases, hme.Alias{
			Email:     "a" + strconv.Itoa(i) + "@icloud.com",
			CreatedAt: strconv.FormatInt(base.Add(offset).UnixMilli(), 10),
		})
	}
	// 混入一条 RFC3339 且缺失时间戳的记录,验证时区统一与缺失跳过
	aliases = append(aliases,
		hme.Alias{Email: "rfc@icloud.com", CreatedAt: base.Add(90 * time.Second).UTC().Format(time.RFC3339)},
		hme.Alias{Email: "none@icloud.com"},
	)

	var buf bytes.Buffer
	reportDensity(&buf, aliases)
	out := buf.String()

	t.Logf("报告输出示例:\n%s", out)
	if !strings.Contains(out, "别名总数: 7") || !strings.Contains(out, "1 条缺少可解析的创建时间") {
		t.Fatalf("统计头部异常:\n%s", out)
	}
	// 10 秒窗口内只应命中 0s/5s/10s 三次;1 分钟窗口覆盖全部五个点
	if got := windowCount(t, out, "10s"); got != 3 {
		t.Fatalf("10s 窗口应为 3,得到 %d:\n%s", got, out)
	}
	if got := windowCount(t, out, "1m"); got != 5 {
		t.Fatalf("1m 窗口应为 5,得到 %d:\n%s", got, out)
	}
	// 时间范围必须按本地时区升序展示(RFC3339 的 UTC 记录要与时间戳记录可比)
	firstLine := strings.Split(out, "\n")
	for _, line := range firstLine {
		if strings.HasPrefix(line, "时间范围: ") {
			if !strings.Contains(line, "2026-09-16 10:00:00 → 2026-09-16 10:01:30") {
				t.Fatalf("时间范围展示错误(时区未统一?): %s", line)
			}
		}
	}
}

// windowCount 从报告中取出某个窗口的最大创建数。
func windowCount(t *testing.T, out, window string) int {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == window {
			count, err := strconv.Atoi(fields[1])
			if err != nil {
				t.Fatalf("无法解析 %q 行的计数: %v", line, err)
			}
			return count
		}
	}
	t.Fatalf("输出中没有窗口 %s 的行:\n%s", window, out)
	return 0
}

func TestReportDensityHandlesNoTimestamps(t *testing.T) {
	var buf bytes.Buffer
	reportDensity(&buf, []hme.Alias{{Email: "a@icloud.com"}})
	if !strings.Contains(buf.String(), "没有可用于统计的创建时间") {
		t.Fatalf("应在无时间数据时给出说明:\n%s", buf.String())
	}
}

func TestReportProbeSummarizesAttempts(t *testing.T) {
	base := time.Now()
	records := []attemptRecord{
		{index: 1, startedAt: base, elapsed: 1800 * time.Millisecond, email: "a@icloud.com"},
		{index: 2, startedAt: base.Add(3 * time.Second), elapsed: 1700 * time.Millisecond, email: "b@icloud.com"},
		{index: 3, startedAt: base.Add(6 * time.Second), elapsed: 2 * time.Second, err: errFromString("创建别名失败: HTTP 429: too many requests")},
	}

	var buf bytes.Buffer
	reportProbe(&buf, records, []string{"a@icloud.com", "b@icloud.com"}, errFromString("HTTP 429: too many requests"))
	out := buf.String()

	for _, want := range []string{"尝试 3 次:成功 2,失败 1", "平均", "本次创建: a@icloud.com, b@icloud.com", "HTTP 429"} {
		if !strings.Contains(out, want) {
			t.Fatalf("输出缺少 %q:\n%s", want, out)
		}
	}
}

func TestValidateProbeFlags(t *testing.T) {
	prevAttempts, prevInterval := *attempts, *interval
	defer func() { *attempts, *interval = prevAttempts, prevInterval }()

	*attempts, *interval = 5, time.Second
	if err := validateProbeFlags(); err != nil {
		t.Fatalf("合法参数不应报错: %v", err)
	}
	*attempts = 0
	if err := validateProbeFlags(); err == nil {
		t.Fatal("count=0 应报错")
	}
	*attempts = probeHardLimit + 1
	if err := validateProbeFlags(); err == nil {
		t.Fatal("超过硬上限应报错")
	}
	*attempts, *interval = 5, -time.Second
	if err := validateProbeFlags(); err == nil {
		t.Fatal("负间隔应报错")
	}
}

func TestReportDensityShowsCapacityReference(t *testing.T) {
	previousCapacity, previousWindows := *capacity, *windowList
	defer func() { *capacity, *windowList = previousCapacity, previousWindows }()

	*capacity, *windowList = 700, "30m"
	base := time.Date(2026, 9, 16, 10, 0, 0, 0, time.Local)
	// 30 分钟窗口内放 6 条(0s..1500s),再放一条窗口外的记录
	aliases := make([]hme.Alias, 0, 7)
	for i := 0; i < 6; i++ {
		aliases = append(aliases, hme.Alias{
			Email:     "a" + strconv.Itoa(i) + "@icloud.com",
			CreatedAt: strconv.FormatInt(base.Add(time.Duration(i)*300*time.Second).UnixMilli(), 10),
		})
	}
	aliases = append(aliases, hme.Alias{
		Email:     "late@icloud.com",
		CreatedAt: strconv.FormatInt(base.Add(2*time.Hour).UnixMilli(), 10),
	})

	var buf bytes.Buffer
	reportDensity(&buf, aliases)
	out := buf.String()

	if got := windowCount(t, out, "30m"); got != 6 {
		t.Fatalf("30m 窗口应为 6,得到 %d:\n%s", got, out)
	}
	for _, want := range []string{"额度参考: 已用 7/700", "剩余约 693", "非 Apple 官方数字"} {
		if !strings.Contains(out, want) {
			t.Fatalf("输出缺少 %q:\n%s", want, out)
		}
	}

	// capacity=0 时不显示额度行
	*capacity = 0
	buf.Reset()
	reportDensity(&buf, aliases)
	if strings.Contains(buf.String(), "额度参考") {
		t.Fatalf("capacity=0 时不应显示额度行:\n%s", buf.String())
	}
}

func TestReportProbeShowsThirtyMinuteWindow(t *testing.T) {
	base := time.Now()
	records := []attemptRecord{
		{index: 1, startedAt: base, elapsed: time.Second, email: "a@icloud.com"},
		{index: 2, startedAt: base.Add(2 * time.Second), elapsed: time.Second, email: "b@icloud.com"},
	}

	var buf bytes.Buffer
	reportProbe(&buf, records, []string{"a@icloud.com", "b@icloud.com"}, nil)
	out := buf.String()

	if !strings.Contains(out, "30m") {
		t.Fatalf("应报告 30 分钟窗口:\n%s", out)
	}
	if !strings.Contains(out, "本次未触发任何限制") {
		t.Fatalf("未触发限制时应给出下一步建议:\n%s", out)
	}
}
