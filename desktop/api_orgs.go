package main

// 团队 (CZL Connect 组织) 绑定。成员、邀请、动态由 Connect 维护, 这里只转发;
// 建团队、加入后刷新同步空间, 离开、解散后清掉本机该团队的数据。

import (
	"context"
	"time"

	"github.com/woodchen-ink/czlterm/desktop/internal/cloudsync"
	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
	"github.com/woodchen-ink/czlterm/desktop/internal/orgs"
	"github.com/woodchen-ink/czlterm/desktop/internal/space"
)

func (a *App) orgCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 20*time.Second)
}

// refreshSpacesLater 组织变了以后在后台刷新同步空间 (新团队出现在空间切换器), 不拖慢界面。
func (a *App) refreshSpacesLater() {
	if a.ready() == nil {
		go a.CloudRefresh()
	}
}

// OrgList 我在本应用的团队。
func (a *App) OrgList() ([]orgs.Org, error) {
	ctx, cancel := a.orgCtx()
	defer cancel()
	out, err := a.orgs.List(ctx)
	a.logErr("org.list", err)
	return out, err
}

// OrgCreate 创建团队, 本人为所有者。
func (a *App) OrgCreate(name string) (*orgs.Org, error) {
	ctx, cancel := a.orgCtx()
	defer cancel()
	out, err := a.orgs.Create(ctx, name)
	a.logErr("org.create", err)
	if err == nil {
		a.refreshSpacesLater()
	}
	return out, err
}

// OrgDetail 团队详情: 成员; 管理员以上另有待处理邀请与邀请码。
func (a *App) OrgDetail(orgID string) (*orgs.Detail, error) {
	ctx, cancel := a.orgCtx()
	defer cancel()
	out, err := a.orgs.Detail(ctx, orgID)
	a.logErr("org.detail", err)
	return out, err
}

// OrgRename 改团队名。
func (a *App) OrgRename(orgID, name string) error {
	ctx, cancel := a.orgCtx()
	defer cancel()
	err := a.orgs.Rename(ctx, orgID, name)
	a.logErr("org.rename", err)
	if err == nil {
		a.refreshSpacesLater()
	}
	return err
}

// OrgInviteEmail 按邮箱邀请。
func (a *App) OrgInviteEmail(orgID, email, role string) error {
	ctx, cancel := a.orgCtx()
	defer cancel()
	err := a.orgs.InviteEmail(ctx, orgID, email, role)
	a.logErr("org.invite", err)
	return err
}

// OrgRevokeInvite 撤回邮箱邀请。
func (a *App) OrgRevokeInvite(orgID, inviteID string) error {
	ctx, cancel := a.orgCtx()
	defer cancel()
	err := a.orgs.RevokeInvite(ctx, orgID, inviteID)
	a.logErr("org.revoke_invite", err)
	return err
}

// OrgCreateInviteCode 生成邀请码, 明文只返回这一次。
func (a *App) OrgCreateInviteCode(orgID, role string, maxUses, ttlDays int) (*orgs.NewInviteCode, error) {
	ctx, cancel := a.orgCtx()
	defer cancel()
	out, err := a.orgs.CreateInviteCode(ctx, orgID, role, maxUses, ttlDays)
	a.logErr("org.invite_code", err)
	return out, err
}

// OrgRevokeInviteCode 撤回邀请码。
func (a *App) OrgRevokeInviteCode(orgID, codeID string) error {
	ctx, cancel := a.orgCtx()
	defer cancel()
	err := a.orgs.RevokeInviteCode(ctx, orgID, codeID)
	a.logErr("org.revoke_code", err)
	return err
}

// OrgJoin 凭邀请码加入团队。
func (a *App) OrgJoin(code string) (*orgs.Joined, error) {
	ctx, cancel := a.orgCtx()
	defer cancel()
	out, err := a.orgs.Join(ctx, code)
	a.logErr("org.join", err)
	if err == nil {
		a.refreshSpacesLater()
	}
	return out, err
}

// OrgSetRole 改成员角色。
func (a *App) OrgSetRole(orgID, memberID, role string) error {
	ctx, cancel := a.orgCtx()
	defer cancel()
	err := a.orgs.SetRole(ctx, orgID, memberID, role)
	a.logErr("org.set_role", err)
	return err
}

// OrgRemoveMember 移出成员。被移出者本机已同步的副本无法收回 (之后的修改他拉不到)。
func (a *App) OrgRemoveMember(orgID, memberID string) error {
	ctx, cancel := a.orgCtx()
	defer cancel()
	err := a.orgs.RemoveMember(ctx, orgID, memberID)
	a.logErr("org.remove_member", err)
	return err
}

// OrgTransfer 把所有权转给另一位成员。
func (a *App) OrgTransfer(orgID, memberID string) error {
	ctx, cancel := a.orgCtx()
	defer cancel()
	err := a.orgs.Transfer(ctx, orgID, memberID)
	a.logErr("org.transfer", err)
	if err == nil {
		a.refreshSpacesLater()
	}
	return err
}

// OrgActivity 团队动态, 新的在前。before 为上一页最后一条的 createdAt (RFC3339), 空取最新。
func (a *App) OrgActivity(orgID, before string) ([]orgs.Activity, error) {
	ctx, cancel := a.orgCtx()
	defer cancel()
	var t time.Time
	if before != "" {
		var err error
		if t, err = time.Parse(time.RFC3339Nano, before); err != nil {
			return nil, errcode.ErrOrgRejected.Wrap(err)
		}
	}
	out, err := a.orgs.ActivityPage(ctx, orgID, t, 30)
	a.logErr("org.activity", err)
	return out, err
}

// OrgPendingCount 团队空间里还没推送的修改数, 离开或解散前提示用。
func (a *App) OrgPendingCount(orgID string) int {
	if sp := a.spaces.Get(cloudsync.OrgKey(a.cloud.User(), orgID)); sp != nil {
		return sp.PendingCount()
	}
	return 0
}

// OrgLeave 退出团队并清掉本机该团队的数据。还有没推送的修改时, force 为 false 返回 6406 由界面确认。
func (a *App) OrgLeave(orgID string, force bool) error {
	return a.leaveOrg(orgID, force, a.orgs.Leave)
}

// OrgClose 解散团队 (所有者) 并清掉本机该团队的数据, 规则同 OrgLeave。
func (a *App) OrgClose(orgID string, force bool) error {
	return a.leaveOrg(orgID, force, a.orgs.Close)
}

func (a *App) leaveOrg(orgID string, force bool, call func(context.Context, string) error) error {
	key := cloudsync.OrgKey(a.cloud.User(), orgID)
	if !force && a.OrgPendingCount(orgID) > 0 {
		return errcode.ErrSpaceHasPending
	}
	ctx, cancel := a.orgCtx()
	defer cancel()
	if err := call(ctx, orgID); err != nil {
		a.logErr("org.leave", err)
		return err
	}
	if a.currentSpace().Key == key {
		if _, err := a.SwitchSpace(space.Local); err != nil {
			a.logErr("org.leave_switch", err)
		}
	}
	// 本机从没同步过这个团队时也照样调用: 没有数据可删, 不报错
	if err := a.cloud.ForgetSpace(ctx, key); err != nil {
		a.logErr("org.leave_forget", err)
		return err
	}
	return nil
}

// logErr 记下绑定调用失败, err 为 nil 时什么也不做。
func (a *App) logErr(op string, err error) {
	if err != nil {
		a.log.Warn(op, "err", err)
	}
}
