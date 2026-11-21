// Package orgs 是 CZL Connect 组织接口 (/api/oauth2/orgs, 用户令牌) 的客户端。
//
// 组织 (团队) 与成员、邀请、组织动态都由 Connect 维护, czlterm 只调接口展示。
// 团队的数据空间由同步服务按组织提供 (internal/cloudsync), 这里不碰业务数据。
package orgs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
)

// DefaultBaseURL 是正式环境的组织接口根地址。
const DefaultBaseURL = "https://connect.czl.net/api/oauth2/orgs"

// 角色, 由高到低。
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
	RoleViewer = "viewer"
)

// Org 是组织概要。Role 为本人在组织里的角色; Status 为 active / readonly。
type Org struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Role        string    `json:"role"`
	MemberCount int64     `json:"memberCount"`
	Seats       int       `json:"seats"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Member 是组织成员。ID 是成员记录的标识 (改角色、移除、转让用), 不是用户 id;
// UserID 是用户标识 (与同步服务 presence 的 userId 同源), 服务端提供时才有。Email 已打码。
type Member struct {
	ID       string    `json:"id"`
	UserID   string    `json:"userId,omitempty"`
	Name     string    `json:"name"`
	Avatar   string    `json:"avatar"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joinedAt"`
	IsSelf   bool      `json:"isSelf"`
}

// Invite 是待处理的邮箱邀请 (admin 以上可见)。Status 为 pending / expired / declined。
type Invite struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	InvitedAt time.Time `json:"invitedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// InviteCode 是邀请码 (明文只在生成时返回一次, 列表里没有)。
type InviteCode struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	MaxUses   int       `json:"maxUses"`
	UsedCount int       `json:"usedCount"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
}

// NewInviteCode 是刚生成的邀请码, Code 与 JoinURL 只此一次。
type NewInviteCode struct {
	InviteCode
	Code    string `json:"code"`
	JoinURL string `json:"joinUrl"`
}

// Detail 是组织详情。Invites 与 InviteCodes 只对 admin 以上下发 (其余为空)。
type Detail struct {
	Org
	Members     []Member     `json:"members"`
	Invites     []Invite     `json:"invites"`
	InviteCodes []InviteCode `json:"inviteCodes"`
}

// Joined 是凭码加入的结果。已是成员时不消耗邀请码。
type Joined struct {
	Org           Org  `json:"org"`
	AlreadyMember bool `json:"alreadyMember"`
}

// Activity 是一条组织动态。Action 见文档 (create / invite_member / remove_member ...),
// Description 是服务端落库时的中文描述, 界面按 Action 出文案, 认不出时用它兜底。
type Activity struct {
	Action      string         `json:"action"`
	Actor       string         `json:"actor"`
	ActorAvatar string         `json:"actorAvatar"`
	Target      string         `json:"target"`
	Params      map[string]any `json:"params"`
	Description string         `json:"description"`
	CreatedAt   time.Time      `json:"createdAt"`
}

// TokenSource 提供用户访问令牌 (由 internal/account 实现)。
type TokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

// Client 是组织接口客户端。并发安全。
type Client struct {
	base   string
	tokens TokenSource
	hc     *http.Client
}

// New 创建客户端。base 为空用正式地址; hc 为空用 15 秒超时的默认客户端。
func New(base string, tokens TokenSource, hc *http.Client) *Client {
	if base == "" {
		base = DefaultBaseURL
	}
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{base: strings.TrimRight(base, "/"), tokens: tokens, hc: hc}
}

// List 返回本人在本应用的组织。
func (c *Client) List(ctx context.Context) ([]Org, error) {
	var out struct {
		Items []Org `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "", nil, &out)
	if out.Items == nil {
		out.Items = []Org{}
	}
	return out.Items, err
}

// Create 创建组织, 本人为所有者。
func (c *Client) Create(ctx context.Context, name string) (*Org, error) {
	var out Org
	return &out, c.do(ctx, http.MethodPost, "", map[string]string{"name": name}, &out)
}

// Detail 返回组织详情。
func (c *Client) Detail(ctx context.Context, orgID string) (*Detail, error) {
	var out Detail
	if err := c.do(ctx, http.MethodGet, "/"+url.PathEscape(orgID), nil, &out); err != nil {
		return nil, err
	}
	if out.Members == nil {
		out.Members = []Member{}
	}
	if out.Invites == nil {
		out.Invites = []Invite{}
	}
	if out.InviteCodes == nil {
		out.InviteCodes = []InviteCode{}
	}
	return &out, nil
}

// Rename 改组织名 (admin 以上)。
func (c *Client) Rename(ctx context.Context, orgID, name string) error {
	return c.do(ctx, http.MethodPatch, "/"+url.PathEscape(orgID), map[string]string{"name": name}, nil)
}

// Close 解散组织 (所有者)。
func (c *Client) Close(ctx context.Context, orgID string) error {
	return c.do(ctx, http.MethodDelete, "/"+url.PathEscape(orgID), nil, nil)
}

// Leave 退出组织 (所有者不能退出, 须先转让或解散)。
func (c *Client) Leave(ctx context.Context, orgID string) error {
	return c.do(ctx, http.MethodPost, "/"+url.PathEscape(orgID)+"/leave", nil, nil)
}

// Transfer 把所有权转给另一位成员, 原所有者降为管理员。
func (c *Client) Transfer(ctx context.Context, orgID, memberID string) error {
	return c.do(ctx, http.MethodPost, "/"+url.PathEscape(orgID)+"/transfer", map[string]string{"memberId": memberID}, nil)
}

// InviteEmail 按邮箱邀请。不论邮箱是否注册结果相同。
func (c *Client) InviteEmail(ctx context.Context, orgID, email, role string) error {
	return c.do(ctx, http.MethodPost, "/"+url.PathEscape(orgID)+"/invites",
		map[string]string{"email": email, "role": role}, nil)
}

// RevokeInvite 撤回邮箱邀请。
func (c *Client) RevokeInvite(ctx context.Context, orgID, inviteID string) error {
	return c.do(ctx, http.MethodDelete, "/"+url.PathEscape(orgID)+"/invites/"+url.PathEscape(inviteID), nil, nil)
}

// CreateInviteCode 生成邀请码。maxUses / ttlDays 为 0 时用服务端默认 (1 次、7 天)。
func (c *Client) CreateInviteCode(ctx context.Context, orgID, role string, maxUses, ttlDays int) (*NewInviteCode, error) {
	var out NewInviteCode
	body := map[string]any{"role": role}
	if maxUses > 0 {
		body["maxUses"] = maxUses
	}
	if ttlDays > 0 {
		body["ttlDays"] = ttlDays
	}
	return &out, c.do(ctx, http.MethodPost, "/"+url.PathEscape(orgID)+"/invite-codes", body, &out)
}

// RevokeInviteCode 撤回邀请码。
func (c *Client) RevokeInviteCode(ctx context.Context, orgID, codeID string) error {
	return c.do(ctx, http.MethodDelete, "/"+url.PathEscape(orgID)+"/invite-codes/"+url.PathEscape(codeID), nil, nil)
}

// Join 凭邀请码加入。
func (c *Client) Join(ctx context.Context, code string) (*Joined, error) {
	var out Joined
	return &out, c.do(ctx, http.MethodPost, "/join", map[string]string{"code": strings.TrimSpace(code)}, &out)
}

// SetRole 改成员角色。
func (c *Client) SetRole(ctx context.Context, orgID, memberID, role string) error {
	return c.do(ctx, http.MethodPut, "/"+url.PathEscape(orgID)+"/members/"+url.PathEscape(memberID),
		map[string]string{"role": role}, nil)
}

// RemoveMember 移除成员。被移除者从此拉不到团队空间的新内容, 本机已同步的副本无法收回。
func (c *Client) RemoveMember(ctx context.Context, orgID, memberID string) error {
	return c.do(ctx, http.MethodDelete, "/"+url.PathEscape(orgID)+"/members/"+url.PathEscape(memberID), nil, nil)
}

// ActivityPage 返回组织动态, 新的在前; before 为上一页最后一条的时间, 零值取最新。
func (c *Client) ActivityPage(ctx context.Context, orgID string, before time.Time, limit int) ([]Activity, error) {
	q := url.Values{}
	if !before.IsZero() {
		q.Set("before", before.UTC().Format(time.RFC3339Nano))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	path := "/" + url.PathEscape(orgID) + "/activity"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out struct {
		Items []Activity `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	if out.Items == nil {
		out.Items = []Activity{}
	}
	return out.Items, err
}

// do 发请求并按状态码归类错误: 404 不是成员或组织不存在, 403 角色不够或应用未开通组织,
// 400 校验失败。服务端的英文原因只进日志, 界面按码出文案。
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	tok, err := c.tokens.AccessToken(ctx)
	if err != nil {
		return err
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return errcode.ErrPayloadCodec.Wrap(err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return errcode.ErrOrgRequest.Wrap(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return errcode.ErrOrgRequest.Wrap(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return errcode.ErrOrgRequest.Wrap(err)
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		cause := fmt.Errorf("%s %s: http %d %s", method, path, resp.StatusCode, e.Error)
		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			return errcode.ErrAuthExpired.Wrap(cause)
		case resp.StatusCode == http.StatusForbidden:
			return errcode.ErrOrgForbidden.Wrap(cause)
		case resp.StatusCode == http.StatusNotFound:
			return errcode.ErrOrgNotFound.Wrap(cause)
		case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusConflict:
			return errcode.ErrOrgRejected.Wrap(cause)
		default:
			return errcode.ErrOrgRequest.Wrap(cause)
		}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errcode.ErrOrgRequest.Wrap(errors.Join(errors.New("decode response"), err))
	}
	return nil
}
