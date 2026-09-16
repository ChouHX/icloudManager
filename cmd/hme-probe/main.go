// Command hme-probe 检测 iCloud Hide My Email 的别名创建密度与速率表现。
//
// 两种模式:
//
//	-analyze  只读:拉取别名列表,按创建时间统计任意时间窗口内的最大创建数
//	-probe    写入:按固定间隔连续创建别名,直到尝试次数用尽或触发 iCloud 限制
//
// 用法:
//
//	go run ./cmd/hme-probe -data ./data -analyze
//	go run ./cmd/hme-probe -data ./data -analyze -windows 10s,1m,10m,1h
//	go run ./cmd/hme-probe -data ./data -account acc_1234 -probe -count 10 -interval 3s
//	go run ./cmd/hme-probe -data ./data -probe -count 5 -interval 2s -cleanup
//
// 注意:probe 会真实创建别名并占用 iCloud 配额;analyze 不产生任何写入。
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"icloud-hme/internal/account"
	"icloud-hme/internal/hme"
)

// probeHardLimit 是尝试次数的硬上限,避免误用参数把配额打满。
const probeHardLimit = 50

var (
	dataDir     = flag.String("data", "./data", "数据目录(accounts.json 所在位置)")
	accountID   = flag.String("account", "", "账号 ID,缺省使用列表中的第一个账号")
	analyzeMode = flag.Bool("analyze", false, "只读模式:统计现有别名的创建时间分布")
	probeMode   = flag.Bool("probe", false, "写入模式:连续创建别名以探测速率上限")
	attempts    = flag.Int("count", 5, fmt.Sprintf("probe 模式最多尝试创建几次(上限 %d)", probeHardLimit))
	interval    = flag.Duration("interval", 3*time.Second, "probe 模式两次尝试之间的等待时间")
	cleanup     = flag.Bool("cleanup", false, "probe 结束后删除本次创建的别名")
	windowList  = flag.String("windows", "1m,5m,30m,1h,24h", "analyze 模式的统计窗口,逗号分隔(30m 是 Apple 侧的经验节流窗口)")
	labelPrefix = flag.String("label-prefix", "probe", "probe 模式创建别名的标签前缀,便于识别")
	capacity    = flag.Int("capacity", 700, "账号别名总量参考上限(经验值,非 Apple 官方数字;设为 0 则不显示剩余额度)")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "hme-probe:检测 iCloud 别名创建密度与速率\n\n用法:\n")
		fmt.Fprintf(os.Stderr, "  hme-probe -data ./data -analyze [-windows 1m,5m,1h,24h]\n")
		fmt.Fprintf(os.Stderr, "  hme-probe -data ./data -probe -count 10 -interval 3s [-cleanup]\n\n参数:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *analyzeMode == *probeMode {
		fmt.Fprintln(os.Stderr, "请二选一:-analyze(只读统计) 或 -probe(主动创建探测)")
		flag.Usage()
		os.Exit(2)
	}
	if *probeMode {
		if err := validateProbeFlags(); err != nil {
			fmt.Fprintf(os.Stderr, "参数错误: %v\n", err)
			os.Exit(2)
		}
	}

	manager, err := account.NewManager(mustAbsDataDir())
	if err != nil {
		log.Fatalf("加载账号失败: %v", err)
	}
	defer manager.Close()

	id, name, err := pickAccount(manager)
	if err != nil {
		log.Fatalf("%v", err)
	}

	client, err := manager.HMEClient(id, false)
	if err != nil {
		log.Fatalf("创建 HME 客户端失败: %v", err)
	}

	fmt.Printf("账号: %s (%s)\n\n", name, id)

	if *analyzeMode {
		if err := runAnalyze(client); err != nil {
			log.Fatalf("统计失败: %v", err)
		}
		return
	}

	created, err := runProbe(client)
	// HME 调用可能刷新 Cookie,与 API 路径一致地落盘
	_ = manager.SaveCookies(id, client.Cookies)
	if len(created) > 0 && *cleanup {
		cleanupCreated(client, created)
	}
	if err != nil {
		log.Fatalf("探测中止: %v", err)
	}
}

func mustAbsDataDir() string {
	abs, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatalf("数据目录路径错误: %v", err)
	}
	return abs
}

func pickAccount(manager *account.Manager) (id, name string, err error) {
	summaries := manager.ListSummaries()
	if len(summaries) == 0 {
		return "", "", fmt.Errorf("没有可用账号,请先在管理界面添加账号并配置 Cookie")
	}
	if *accountID == "" {
		return summaries[0].ID, summaries[0].Name, nil
	}
	for _, summary := range summaries {
		if summary.ID == *accountID {
			return summary.ID, summary.Name, nil
		}
	}
	return "", "", fmt.Errorf("账号不存在: %s", *accountID)
}

// ---------------------------------------------------------------------------
// analyze:统计历史创建密度
// ---------------------------------------------------------------------------

func runAnalyze(client *hme.Client) error {
	fmt.Println("模式: 只读统计(不会创建任何别名)")
	aliases, err := client.ListAliases()
	if err != nil {
		return err
	}
	reportDensity(os.Stdout, aliases)
	return nil
}

// reportDensity 输出创建时间密度统计,与网络调用分离以便测试。
func reportDensity(out io.Writer, aliases []hme.Alias) {
	times := make([]time.Time, 0, len(aliases))
	missing := 0
	for _, alias := range aliases {
		parsed, ok := parseAliasTime(alias.CreatedAt)
		if !ok {
			missing++
			continue
		}
		times = append(times, parsed.Local())
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })

	fmt.Fprintf(out, "别名总数: %d", len(aliases))
	if missing > 0 {
		fmt.Fprintf(out, "(其中 %d 条缺少可解析的创建时间,已跳过)", missing)
	}
	fmt.Fprintln(out)
	if len(times) == 0 {
		fmt.Fprintln(out, "没有可用于统计的创建时间:iCloud 未返回 createTimestamp/createdAt")
		return
	}
	fmt.Fprintf(out, "时间范围: %s → %s\n\n", times[0].Format("2006-01-02 15:04:05"), times[len(times)-1].Format("2006-01-02 15:04:05"))

	windows, err := parseWindows(*windowList)
	if err != nil {
		fmt.Fprintf(out, "窗口参数无效: %v\n", err)
		return
	}

	fmt.Fprintf(out, "%-8s %-8s %s\n", "窗口", "最大创建数", "出现区间(窗口内最早一次 → 最晚一次)")
	for _, window := range windows {
		count, start, end := maxInWindow(times, window)
		fmt.Fprintf(out, "%-8s %-8d %s → %s\n",
			formatWindow(window), count,
			start.Format("2006-01-02 15:04:05"), end.Format("2006-01-02 15:04:05"))
	}

	if *capacity > 0 {
		remaining := *capacity - len(aliases)
		if remaining < 0 {
			remaining = 0
		}
		fmt.Fprintf(out, "\n额度参考: 已用 %d/%d,剩余约 %d\n", len(aliases), *capacity, remaining)
		fmt.Fprintf(out, "(上限 %d 为社区实测经验值,非 Apple 官方数字;你的账号可能不同,可用 -capacity 调整)\n", *capacity)
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "说明: 这里是历史峰值,不代表 iCloud 的当前上限。想测当前限制请用 -probe。")
}

func parseWindows(raw string) ([]time.Duration, error) {
	parts := strings.Split(raw, ",")
	out := make([]time.Duration, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		window, err := time.ParseDuration(part)
		if err != nil {
			return nil, fmt.Errorf("非法窗口 %q: %v", part, err)
		}
		if window <= 0 {
			return nil, fmt.Errorf("窗口必须为正数: %q", part)
		}
		out = append(out, window)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("没有有效窗口")
	}
	return out, nil
}

// maxInWindow 返回升序时间序列中,任意长度为 window 的区间内包含的最大元素数及该区间端点。
//
// 窗口按闭区间处理:首尾两次创建的时间差恰好等于 window 时也算落在同一窗口内,
// 因此输出行里的"最早一次 → 最晚一次"两端差值不会超过 window。
func maxInWindow(times []time.Time, window time.Duration) (int, time.Time, time.Time) {
	if len(times) == 0 {
		return 0, time.Time{}, time.Time{}
	}
	best, bestStart, bestEnd := 0, times[0], times[0]
	left := 0
	for right := range times {
		for times[right].Sub(times[left]) > window {
			left++
		}
		if count := right - left + 1; count > best {
			best, bestStart, bestEnd = count, times[left], times[right]
		}
	}
	return best, bestStart, bestEnd
}

// parseAliasTime 解析 iCloud 返回的创建时间。
// 可能是秒/毫秒/微秒时间戳字符串,也可能是 RFC3339 时间。
func parseAliasTime(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}

	if numeric, err := strconv.ParseInt(value, 10, 64); err == nil {
		magnitude := numeric
		if magnitude < 0 {
			magnitude = -magnitude
		}
		switch {
		case magnitude < 1e11: // 秒
			return time.Unix(numeric, 0), true
		case magnitude < 1e14: // 毫秒
			return time.UnixMilli(numeric), true
		case magnitude < 1e17: // 微秒
			return time.UnixMicro(numeric), true
		default: // 纳秒
			return time.Unix(0, numeric), true
		}
	}

	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func formatWindow(window time.Duration) string {
	switch {
	case window%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", int(window.Hours()/24))
	case window%time.Hour == 0:
		return fmt.Sprintf("%dh", int(window.Hours()))
	case window%time.Minute == 0:
		return fmt.Sprintf("%dm", int(window.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(window.Seconds()))
	}
}

// ---------------------------------------------------------------------------
// probe:主动创建以探测速率限制
// ---------------------------------------------------------------------------

type attemptRecord struct {
	index     int
	startedAt time.Time
	elapsed   time.Duration
	email     string
	err       error
}

// shouldStop 判断是否应停止探测:配额耗尽无意义重试,限流继续下去只会加长冷却。
func shouldStop(err error) bool {
	return hme.IsQuotaExhausted(err) || hme.IsThrottled(err)
}

// validateProbeFlags 在触碰网络与配额之前拦下非法参数。
func validateProbeFlags() error {
	if *attempts <= 0 {
		return fmt.Errorf("count 必须为正数")
	}
	if *attempts > probeHardLimit {
		return fmt.Errorf("count 超过硬上限 %d,请分多次探测", probeHardLimit)
	}
	if *interval < 0 {
		return fmt.Errorf("interval 不能为负数")
	}
	return nil
}

func runProbe(client *hme.Client) ([]string, error) {
	fmt.Printf("模式: 主动探测(会真实创建别名)\n")
	fmt.Printf("计划: 最多尝试 %d 次,间隔 %s,标签前缀 %q\n", *attempts, *interval, *labelPrefix)
	if !*cleanup {
		fmt.Println("提示: 未开启 -cleanup,创建出的别名会保留")
	}
	fmt.Println()

	var records []attemptRecord
	var created []string
	var stopped error

	for i := 0; i < *attempts; i++ {
		if i > 0 && *interval > 0 {
			time.Sleep(*interval)
		}

		label := fmt.Sprintf("%s-%d", *labelPrefix, i+1)
		startedAt := time.Now()
		result, err := client.CreateAlias(label, 1)
		elapsed := time.Since(startedAt)

		record := attemptRecord{index: i + 1, startedAt: startedAt, elapsed: elapsed}
		if err != nil {
			record.err = err
			records = append(records, record)
			fmt.Printf("[%d/%d] %s 失败(%s): %v\n", i+1, *attempts, startedAt.Format("15:04:05"), elapsed.Round(time.Millisecond), err)
			if shouldStop(err) {
				stopped = err
				if hme.IsQuotaExhausted(err) {
					fmt.Println("       ↑ iCloud 报告别名总量已达上限,继续重试无用")
				} else {
					fmt.Println("       ↑ 疑似触发 iCloud 限流,已停止探测以免加重限制")
				}
				break
			}
			continue
		}

		record.email = result.Email
		records = append(records, record)
		created = append(created, result.Email)
		fmt.Printf("[%d/%d] %s 成功(%s): %s\n", i+1, *attempts, startedAt.Format("15:04:05"), elapsed.Round(time.Millisecond), result.Email)
	}

	reportProbe(os.Stdout, records, created, stopped)
	return created, nil
}

func reportProbe(out io.Writer, records []attemptRecord, created []string, stopped error) {
	succeeded := 0
	var durs []time.Duration
	var successTimes []time.Time
	for _, record := range records {
		if record.err == nil {
			succeeded++
			durs = append(durs, record.elapsed)
			successTimes = append(successTimes, record.startedAt)
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "尝试 %d 次:成功 %d,失败 %d\n", len(records), succeeded, len(records)-succeeded)
	if len(durs) > 0 {
		var total time.Duration
		slowest := durs[0]
		for _, d := range durs {
			total += d
			if d > slowest {
				slowest = d
			}
		}
		fmt.Fprintf(out, "创建耗时:平均 %s,最慢 %s\n", (total / time.Duration(len(durs))).Round(time.Millisecond), slowest.Round(time.Millisecond))
	}

	if len(successTimes) > 0 {
		sort.Slice(successTimes, func(i, j int) bool { return successTimes[i].Before(successTimes[j]) })
		fmt.Fprintln(out, "本次探测中,各窗口内的最大成功数:")
		for _, window := range []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute} {
			count, start, end := maxInWindow(successTimes, window)
			fmt.Fprintf(out, "  %-6s %d 个 (%s → %s)\n", formatWindow(window), count, start.Format("15:04:05"), end.Format("15:04:05"))
		}
	}

	if len(created) > 0 {
		fmt.Fprintf(out, "本次创建: %s\n", strings.Join(created, ", "))
		if !*cleanup {
			fmt.Fprintln(out, "如需删除,可加 -cleanup 重跑,或在管理界面的别名列表中手动删除")
		}
	}
	if stopped != nil {
		fmt.Fprintf(out, "\n触发限制的原始错误: %v\n", stopped)
		fmt.Fprintln(out, "这说明当前配额/节流窗口已被用满,本次探测到上限。")
		fmt.Fprintln(out, "建议: 等待窗口重置(约 30 分钟)后重试,或把 -interval 调大。")
	} else if len(records) > 0 {
		fmt.Fprintln(out, "\n本次未触发任何限制:说明当前节流窗口内仍有余额。")
		fmt.Fprintln(out, "想逼近真实上限,可提高 -count(上限 50)或缩短 -interval 再试一次。")
	}
}

func cleanupCreated(client *hme.Client, created []string) {
	fmt.Println()
	fmt.Println("正在删除本次创建的别名...")
	aliases, err := client.ListAliases()
	if err != nil {
		fmt.Fprintf(os.Stderr, "拉取别名列表失败,无法清理: %v\n", err)
		return
	}
	byEmail := make(map[string]string, len(aliases))
	for _, alias := range aliases {
		byEmail[strings.ToLower(alias.Email)] = alias.AnonymousID
	}

	deleted := 0
	for _, email := range created {
		id := byEmail[strings.ToLower(email)]
		if id == "" {
			fmt.Printf("  跳过 %s(列表中未找到)\n", email)
			continue
		}
		if err := client.Delete(id); err != nil {
			fmt.Printf("  删除 %s 失败: %v\n", email, err)
			continue
		}
		deleted++
		fmt.Printf("  已删除 %s\n", email)
	}
	fmt.Printf("清理完成:%d/%d\n", deleted, len(created))
}
