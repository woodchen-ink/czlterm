package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/woodchen-ink/czlterm/desktop/internal/update"
)

// 在线更新: 启动 30 秒后与之后每 6 小时静默检查一次, 有新版本时通知界面; 安装由用户确认。
// 安装包来自本仓库的 GitHub Releases, 下载后按 SHA256SUMS 校验才运行。

const (
	eventUpdateAvailable = "update:available"
	eventUpdateProgress  = "update:progress"

	updateInterval = 6 * time.Hour
)

// 项目链接, 显示在「关于」页。ForumURL 为空时界面不显示论坛入口。
const (
	projectURL = "https://github.com/" + update.Repo
	forumURL   = "https://www.sunai.net/t/topic/1533"
)

// UpdateInfo 是检查结果。
type UpdateInfo struct {
	Current   string          `json:"current"`
	Available bool            `json:"available"`
	Release   *update.Release `json:"release"`
	// Message 在没有可用版本时说明原因。
	Message string `json:"message"`
}

type updateState struct {
	installing sync.Mutex
	mu         sync.Mutex
	pending    *UpdateInfo
}

// AboutInfo 是「关于」页的数据。
type AboutInfo struct {
	Version     string `json:"version"`
	ProjectURL  string `json:"projectUrl"`
	ReleasesURL string `json:"releasesUrl"`
	IssuesURL   string `json:"issuesUrl"`
	ForumURL    string `json:"forumUrl"`
}

// About 返回版本与项目链接。
func (a *App) About() AboutInfo {
	return AboutInfo{
		Version:     version,
		ProjectURL:  projectURL,
		ReleasesURL: projectURL + "/releases",
		IssuesURL:   projectURL + "/issues",
		ForumURL:    forumURL,
	}
}

// GetPendingUpdate 返回已发现但尚未安装的新版本, 没有时返回 nil。界面启动较晚错过事件时用它取回。
func (a *App) GetPendingUpdate() *UpdateInfo {
	a.updates.mu.Lock()
	defer a.updates.mu.Unlock()
	return a.updates.pending
}

// CheckForUpdate 立即检查更新。
func (a *App) CheckForUpdate() (UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	info := UpdateInfo{Current: version}
	rel, err := update.Latest(ctx)
	switch {
	case errors.Is(err, update.ErrNoRelease):
		info.Message = "暂无发布版本"
		return info, nil
	case err != nil:
		return info, fmt.Errorf("9001 check for update: %w", err)
	}
	info.Release = rel
	info.Available = update.Newer(rel.Version, version)
	if !info.Available {
		info.Message = "已是最新版本"
	}
	return info, nil
}

// InstallUpdate 下载并校验最新安装包, 安装后退出并重新打开。
// Windows 静默运行 NSIS 安装程序; macOS 解压 .app 的 zip 包原地替换。
func (a *App) InstallUpdate() error {
	if !a.updates.installing.TryLock() {
		return errors.New("9002 an update is already in progress")
	}
	defer a.updates.installing.Unlock()

	info, err := a.CheckForUpdate()
	if err != nil {
		return err
	}
	if !info.Available {
		return errors.New("9003 no newer version available")
	}
	path, err := update.Download(a.ctx, info.Release, func(done, total int64) {
		wruntime.EventsEmit(a.ctx, eventUpdateProgress, map[string]int64{"done": done, "total": total})
	})
	if err != nil {
		return fmt.Errorf("9004 download update: %w", err)
	}
	if runtime.GOOS == "darwin" {
		if err := update.InstallApp(path); err != nil {
			if errors.Is(err, update.ErrAppDirNotWritable) {
				return errors.New("9005 app folder is not writable, the new version is shown in Finder, drag it into Applications")
			}
			return fmt.Errorf("9006 install update: %w", err)
		}
	} else if err := exec.Command(path, "/S").Start(); err != nil {
		return fmt.Errorf("9006 start installer: %w", err)
	}
	a.log.Info("installing update", "version", info.Release.Version)
	// 留一点时间让界面收到响应。
	time.AfterFunc(300*time.Millisecond, a.quit)
	return nil
}

// runUpdateChecks 周期性检查更新。开发版本 (version 不是 vX.Y.Z) 不检查。
func (a *App) runUpdateChecks(ctx context.Context) {
	if !update.Newer("v999.0.0", version) {
		return
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if info, err := a.CheckForUpdate(); err == nil && info.Available {
			a.updates.mu.Lock()
			a.updates.pending = &info
			a.updates.mu.Unlock()
			wruntime.EventsEmit(a.ctx, eventUpdateAvailable, info)
		} else if err != nil {
			a.log.Warn("check for update", "err", err)
		}
		timer.Reset(updateInterval)
	}
}
