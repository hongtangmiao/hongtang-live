package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

/*
 * hongtang-live recompute-reports [--apply] [--day 2026-11-21]
 *
 * 用 raw/<日期>.json.gz 重新算已有日报的统计，修正原 daylog 的时区 bug（活动区间、空档用的是 UTC）。
 * AI 写的分析一个字都不动，也不调模型。
 * 安全检查：新旧统计只允许 totals.first / totals.last / gaps 不同，其余任何地方不一样就跳过这一天；
 * 旧 Markdown 也必须能由旧统计原样生成，否则跳过（说明那篇是别的方式改过的）。
 * 默认只预览，加 --apply 才写库（UPDATE 时带上旧值做条件，期间被改过的不会被覆盖）。
 */

type recomputeResult struct {
	Day, Status, Detail string
}

func recomputeOne(day string, apply bool) recomputeResult {
	res := recomputeResult{Day: day}
	db := daylogDB()
	var oldStats, oldAnalysis, oldMD string
	if err := db.QueryRow("SELECT stats, analysis, markdown FROM reports WHERE day=?", day).Scan(&oldStats, &oldAnalysis, &oldMD); err != nil {
		res.Status, res.Detail = "跳过", "库里没有这一天"
		return res
	}
	rawPath := dlPath("raw", day+".json.gz")
	if _, err := os.Stat(rawPath); err != nil {
		res.Status, res.Detail = "跳过", "没有 raw/"+day+".json.gz"
		return res
	}
	export, _, err := loadExport(rawPath)
	if err != nil || toStr(export["date"]) != day {
		res.Status, res.Detail = "跳过", "raw 读不了或日期对不上"
		return res
	}
	sv, err1 := pyLoads([]byte(oldStats))
	av, err2 := pyLoads([]byte(oldAnalysis))
	old, ok1 := sv.(*OM)
	an, ok2 := av.(*OM)
	if err1 != nil || err2 != nil || !ok1 || !ok2 || pyDumps(old, -1) != oldStats {
		res.Status, res.Detail = "跳过", "旧统计解析后对不回原文"
		return res
	}
	if toMarkdown(old, an) != oldMD {
		res.Status, res.Detail = "跳过", "旧 Markdown 不是由旧统计生成的（可能被手动改过）"
		return res
	}

	fresh := buildDay(export, dlCfg(), readBlocklist())
	for _, k := range []string{"listened", "health"} { // 当时从库里补进来的，原样保留
		if v, ok := old.Get(k); ok {
			fresh.Set(k, v)
		}
	}
	// 安全检查：把三个允许变化的字段换回旧值，其余必须逐字节相同
	probe, _ := pyLoads([]byte(pyDumps(fresh, -1)))
	p := probe.(*OM)
	ot, pt := old.G("totals").(*OM), p.G("totals").(*OM)
	pt.Set("first", ot.G("first"))
	pt.Set("last", ot.G("last"))
	p.Set("gaps", old.G("gaps"))
	if pyDumps(p, -1) != oldStats {
		res.Status, res.Detail = "跳过", "除了活动区间 / 空档以外还有别的不同，没动"
		return res
	}
	newStats := pyDumps(fresh, -1)
	if newStats == oldStats {
		res.Status, res.Detail = "不用改", ""
		return res
	}
	nt := fresh.G("totals").(*OM)
	res.Detail = fmt.Sprintf("%s → %s  改成  %s → %s", pyStr(ot.G("first")), pyStr(ot.G("last")), pyStr(nt.G("first")), pyStr(nt.G("last")))
	if !apply {
		res.Status = "会改"
		return res
	}
	newMD := toMarkdown(fresh, an)
	dlWriteMu.Lock()
	r, err := db.Exec("UPDATE reports SET stats=?, markdown=? WHERE day=? AND stats=? AND markdown=?", newStats, newMD, day, oldStats, oldMD)
	dlWriteMu.Unlock()
	if err != nil {
		res.Status, res.Detail = "失败", err.Error()
		return res
	}
	if n, _ := r.RowsAffected(); n != 1 {
		res.Status, res.Detail = "跳过", "这期间日报被改过了，没覆盖"
		return res
	}
	if err := writeFileAtomic(dlPath("reports", day+".md"), []byte(newMD), 0o644); err != nil {
		res.Status = "已改库"
		res.Detail += "（reports/" + day + ".md 没写成：" + err.Error() + "）"
		return res
	}
	res.Status = "已改"
	return res
}

func runRecompute(args []string) int {
	fs := flag.NewFlagSet("recompute-reports", flag.ContinueOnError)
	apply := fs.Bool("apply", false, "真的写库（不加就只预览）")
	only := fs.String("day", "", "只处理这一天")
	if fs.Parse(args) != nil {
		return 2
	}
	if dlRoot() == "" {
		fmt.Println("没有 daylog 目录：设置 DAYLOG_ROOT 或配置里的 daylog_root")
		return 2
	}
	c := cloneConfig(conf())
	c.DaylogWriter = *apply // 只在这个进程里生效，不存盘
	setConfig(c)
	db := daylogDB()
	if db == nil {
		fmt.Println("daylog 数据库打不开：", conf().DaylogDB)
		return 2
	}
	var days []string
	rows, err := db.Query("SELECT day FROM reports ORDER BY day")
	if err != nil {
		fmt.Println(err)
		return 2
	}
	for rows.Next() {
		var d string
		if rows.Scan(&d) == nil && (*only == "" || d == *only) {
			days = append(days, d)
		}
	}
	rows.Close()
	count := map[string]int{}
	for _, d := range days {
		r := recomputeOne(d, *apply)
		count[r.Status]++
		fmt.Printf("%s  %-4s %s\n", r.Day, r.Status, r.Detail)
	}
	mode := "预览（加 --apply 才会写）"
	if *apply {
		mode = "已写入"
	}
	fmt.Printf("\n%s：共 %d 篇 %v\n", mode, len(days), count)
	if *apply {
		dlLog(fmt.Sprintf("重算日报统计（修正时区）：%v", count))
	}
	if count["失败"] > 0 {
		return 1
	}
	return 0
}

var (
	_ = sql.ErrNoRows
	_ = filepath.Join
	_ = time.Now
)
