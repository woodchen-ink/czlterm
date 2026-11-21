// Package deeplink 解析 czlterm:// 协议链接 (Windows 经第二个进程的命令行、macOS 经系统 URL 事件送达)。
//
// 两类链接并存, 按 host/path 区分:
//   - czlterm://oauth/callback?code=&state=  登录回调 (internal/account 处理)
//   - czlterm://team/join?code=XXXX          加入团队: 打开确认框, 用户确认后才凭码加入
//   - czlterm://billing/done                 托管结账页付完款「返回应用」: 只当作「去查一下权益」的信号,
//     不信任链接上的任何参数, 到没到账只看 entitlements
package deeplink

import (
	"net/url"
	"regexp"
	"strings"
)

// Scheme 是协议前缀。
const Scheme = SchemeName + "://"

// SchemeName 是协议名, 登记在系统 (Windows HKCU、macOS Info.plist) 与 Connect 的回调地址里。
const SchemeName = "czlterm"

// 链接类型。
const (
	KindOAuth       = "oauth"
	KindTeamJoin    = "team_join"
	KindBillingDone = "billing_done"
)

// BillingDoneURL 是托管结账页的回跳地址 (结账地址的 return 参数), 已登记在 Connect「收款 → 设置 → 回跳地址」。
const BillingDoneURL = Scheme + "billing/done"

// Link 是解析后的链接。
type Link struct {
	Kind string
	// Code 是加入团队的邀请码 (KindTeamJoin)
	Code string
	// Raw 是原始链接, 登录回调原样交给 auth
	Raw string
}

// 邀请码只收字母数字与连字符, 防止把奇怪的内容带进确认框与请求
var codePattern = regexp.MustCompile(`^[A-Za-z0-9-]{4,64}$`)

// IsLink 判断参数是否是本协议的链接 (开发版据此在经协议拉起时切回工程目录)。
func IsLink(arg string) bool {
	return len(arg) >= len(Scheme) && strings.EqualFold(arg[:len(Scheme)], Scheme)
}

// Parse 解析一个链接, 认不出的返回 false。
func Parse(arg string) (Link, bool) {
	if !IsLink(arg) {
		return Link{}, false
	}
	u, err := url.Parse(arg)
	if err != nil {
		return Link{}, false
	}
	path := strings.Trim(u.Path, "/")
	switch {
	case strings.EqualFold(u.Host, "oauth") && path == "callback":
		return Link{Kind: KindOAuth, Raw: arg}, true
	case strings.EqualFold(u.Host, "team") && path == "join":
		code := strings.TrimSpace(u.Query().Get("code"))
		if !codePattern.MatchString(code) {
			return Link{}, false
		}
		return Link{Kind: KindTeamJoin, Code: code, Raw: arg}, true
	case strings.EqualFold(u.Host, "billing") && path == "done":
		return Link{Kind: KindBillingDone}, true
	}
	return Link{}, false
}

// TeamJoinURL 生成加入团队的应用内链接。
func TeamJoinURL(code string) string {
	return Scheme + "team/join?" + url.Values{"code": {code}}.Encode()
}
