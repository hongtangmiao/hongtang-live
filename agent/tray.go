package main

import (
	"fmt"
	"time"

	"fyne.io/systray"
)

func runTray(a *Agent, ui *UI) {
	systray.Run(func() { trayReady(a, ui) }, func() {})
}

func trayReady(a *Agent, ui *UI) {
	systray.SetIcon(iconFor("off"))
	systray.SetTooltip("红糖似了吗？")
	mStatus := systray.AddMenuItem("启动中…", "")
	mStatus.Disable()
	mOpen := systray.AddMenuItem("打开状态页 / 设置…", "")
	mSite := systray.AddMenuItem("打开网站", "")
	systray.AddSeparator()
	mPause := systray.AddMenuItem("暂停上报", "")
	mP30 := mPause.AddSubMenuItem("30 分钟", "")
	mP120 := mPause.AddSubMenuItem("2 小时", "")
	mPNight := mPause.AddSubMenuItem("到明天早上", "")
	mResume := systray.AddMenuItem("继续上报", "")
	mAW := systray.AddMenuItem("立即上传今天的 ActivityWatch", "")
	mAuto := systray.AddMenuItemCheckbox("开机自动启动", "", autostartEnabled())
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "")

	refresh := func() {
		cfg, st := a.snapshot()
		state, line := "ok", ""
		paused := time.Now().Before(cfg.PausedUntil)
		switch {
		case !cfg.paired():
			state, line = "off", "还没配对"
		case st.NeedPair:
			state, line = "err", "需要重新配对"
		case paused:
			state, line = "pause", "已暂停到 "+cfg.PausedUntil.Format("01-02 15:04")
		case !st.Online && st.LastErr != "":
			state, line = "err", "连不上服务器"
		case st.LastReport.IsZero():
			line = "已连接"
		default:
			line = fmt.Sprintf("● %s · %s", or(cut(st.Activity, 24), "已连接"), agoText(st.LastReport))
		}
		systray.SetIcon(iconFor(state))
		systray.SetTooltip("红糖似了吗？ · " + line)
		mStatus.SetTitle(line)
		if paused {
			mPause.Hide()
			mResume.Show()
		} else {
			mPause.Show()
			mResume.Hide()
		}
		if cfg.paired() && cfg.hasScope("aw") && cfg.Report.AW {
			mAW.Enable()
		} else {
			mAW.Disable()
		}
		if cfg.paired() {
			mSite.Enable()
		} else {
			mSite.Disable()
		}
	}
	refresh()
	tick := time.NewTicker(15 * time.Second)
	go func() {
		for {
			select {
			case <-a.changed:
				refresh()
			case <-tick.C:
				refresh()
			case <-mOpen.ClickedCh:
				if ui != nil {
					openURL(ui.URL())
				}
			case <-mSite.ClickedCh:
				cfg, _ := a.snapshot()
				if cfg.Server != "" {
					openURL(cfg.Server + "/#now")
				}
			case <-mP30.ClickedCh:
				a.setPause(time.Now().Add(30 * time.Minute))
			case <-mP120.ClickedCh:
				a.setPause(time.Now().Add(2 * time.Hour))
			case <-mPNight.ClickedCh:
				n := time.Now()
				a.setPause(time.Date(n.Year(), n.Month(), n.Day()+1, 6, 0, 0, 0, time.Local))
			case <-mResume.ClickedCh:
				a.setPause(time.Time{})
			case <-mAW.ClickedCh:
				go func() {
					if err := a.UploadToday(); err != nil {
						logf("立即上传失败：%v", err)
					}
				}()
			case <-mAuto.ClickedCh:
				want := !mAuto.Checked()
				if err := setAutostart(want); err != nil {
					logf("开机自启设置失败：%v", err)
				} else if want {
					mAuto.Check()
				} else {
					mAuto.Uncheck()
				}
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func agoText(t time.Time) string {
	s := int(time.Since(t).Seconds())
	switch {
	case s < 60:
		return fmt.Sprintf("%d 秒前", s)
	case s < 3600:
		return fmt.Sprintf("%d 分钟前", s/60)
	}
	return t.Format("15:04")
}
