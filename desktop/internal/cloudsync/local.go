package cloudsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/woodchen-ink/go-web-utils/syncsdk"

	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
	"github.com/woodchen-ink/czlterm/desktop/internal/space"
)

// wireVersion 是线上明文外壳的格式版本。改了格式就加 1: 游标随之归零, 旧格式的文档重新拉一遍。
const wireVersion = 1

// envelope 是线上明文外壳 (SDK 再整体加密)。Data 是连接或脚本的 JSON。
type envelope struct {
	V    int             `json:"v"`
	Data json.RawMessage `json:"data"`
}

func keyDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// conflictSuffix 加在冲突副本名称后。
const conflictSuffix = " (conflict copy)"

// adapter 把本地空间接到同步 SDK: 出站队列、游标、拉取落地与冲突回调。
// SDK 传进来的空间都是服务端 id, 这里换成本地标识。user 在组装引擎时定下:
// 退出登录与换账号时旧引擎可能还有一页在落地, 不能落到新账号的空间里。
type adapter struct {
	s    *Service
	user string
}

var (
	_ syncsdk.Outbox      = adapter{}
	_ syncsdk.CursorStore = adapter{}
	_ syncsdk.Applier     = adapter{}
)

func (a adapter) space(remote string) (*space.Space, error) {
	sp := a.s.opts.Spaces.Get(LocalKey(a.user, remote))
	if sp == nil || !sp.Synced() {
		return nil, errcode.ErrSpaceUnknown
	}
	return sp, nil
}

func itemOf(remote string, p space.Pending) syncsdk.OutboxItem {
	return syncsdk.OutboxItem{Seq: p.Dirty, Space: remote, Collection: p.Collection, ID: p.ID, BaseRev: p.Rev, Deleted: p.Deleted}
}

// ─── Outbox ───────────────────────────────────────────────────────────────

// Enqueue: 本地写入时 Space 已经记下出站序号, 这里只回报它。SDK 的 Save (本地写入后唤醒推送)
// 与冲突另存副本都走到这里, 内容以本地文件为准。
func (a adapter) Enqueue(_ context.Context, item syncsdk.OutboxItem) (syncsdk.OutboxItem, error) {
	sp, err := a.space(item.Space)
	if err != nil {
		return item, err
	}
	if st := sp.State(item.Collection, item.ID); st != nil && st.Dirty > 0 {
		item.Seq, item.BaseRev, item.Deleted = st.Dirty, st.Rev, st.Deleted
	}
	return item, nil
}

// Pending 按出站序号取待推送的修改, 序号与内容在空间写锁里一并读出 (Space.Outgoing)。
// 文件已不在 (保存失败留下的记录) 且从没上传过的放弃; 内容读不出的记日志跳过, 不阻塞其它。
func (a adapter) Pending(_ context.Context, remote string, limit int) ([]syncsdk.OutboxItem, error) {
	sp, err := a.space(remote)
	if err != nil {
		return nil, err
	}
	all := sp.Outgoing(0)
	out := make([]syncsdk.OutboxItem, 0, min(limit, len(all)))
	for _, o := range all {
		if len(out) >= limit {
			break
		}
		item := itemOf(remote, o.Pending)
		if !o.Deleted {
			if o.Missing || o.Err != nil {
				a.s.docFailed(sp.Key, o.Collection, o.ID, o.Err)
				if o.Missing && o.Rev == 0 {
					_ = sp.Drop(o.Collection, o.ID, o.Dirty)
				}
				continue
			}
			item.Value, _ = json.Marshal(envelope{V: wireVersion, Data: o.Data})
		}
		out = append(out, item)
	}
	return out, nil
}

// Lookup 只供 SDK 判断「本地还有没推送的修改」, 不带内容。
func (a adapter) Lookup(_ context.Context, remote, collection, id string) (*syncsdk.OutboxItem, error) {
	sp, err := a.space(remote)
	if err != nil {
		return nil, err
	}
	st := sp.State(collection, id)
	if st == nil || st.Dirty == 0 {
		return nil, nil
	}
	item := itemOf(remote, space.Pending{Collection: collection, ID: id, DocState: *st})
	return &item, nil
}

func (a adapter) Ack(_ context.Context, item syncsdk.OutboxItem, newRev int64) error {
	sp, err := a.space(item.Space)
	if err != nil {
		return err
	}
	return sp.Ack(item.Collection, item.ID, item.Seq, item.BaseRev, newRev)
}

func (a adapter) Rebase(_ context.Context, item syncsdk.OutboxItem, newBaseRev int64) error {
	sp, err := a.space(item.Space)
	if err != nil {
		return err
	}
	return sp.Rebase(item.Collection, item.ID, item.Seq, item.BaseRev, newBaseRev)
}

func (a adapter) Drop(_ context.Context, item syncsdk.OutboxItem) error {
	sp, err := a.space(item.Space)
	if err != nil {
		return err
	}
	return sp.Drop(item.Collection, item.ID, item.Seq)
}

// ─── CursorStore ──────────────────────────────────────────────────────────

// Get 在每轮拉取开始时调用, 顺带清掉上一轮记下的「停在失败文档之前」。
func (a adapter) Get(_ context.Context, remote string) (int64, error) {
	sp, err := a.space(remote)
	if err != nil {
		return 0, err
	}
	a.s.clearHold(sp.Key)
	return sp.Cursor(wireVersion), nil
}

// Set 记下游标。本轮有文档没处理成 (解不开、密钥没到、格式比本机新) 时停在第一条失败之前,
// 下次从那里重拉 (已落地的按 rev 跳过)。
func (a adapter) Set(_ context.Context, remote string, cursor int64) error {
	sp, err := a.space(remote)
	if err != nil {
		return err
	}
	if hold, ok := a.s.hold(sp.Key); ok && hold < cursor {
		cursor = hold
	}
	return sp.SetCursor(cursor, wireVersion)
}

// ─── Applier ──────────────────────────────────────────────────────────────

// Apply 让拉到的一页变更落地。本地有未推送修改的不覆盖 (推送时以 409 走冲突回调)。
func (a adapter) Apply(_ context.Context, remote string, changes []syncsdk.Change) error {
	sp, err := a.space(remote)
	if err != nil {
		return err
	}
	applied := 0
	for _, c := range changes {
		if c.Err != nil {
			a.s.docFailed(sp.Key, c.Collection, c.ID, errcode.ErrSyncPayload.Wrap(c.Err))
			a.s.holdBefore(sp.Key, c.Rev)
			continue
		}
		if c.Pending {
			continue
		}
		var data json.RawMessage
		if !c.Deleted {
			var env envelope
			if err := json.Unmarshal(c.Value, &env); err != nil || env.V == 0 {
				a.s.docFailed(sp.Key, c.Collection, c.ID, errcode.ErrSyncPayload.Wrap(fmt.Errorf("bad envelope: %v", err)))
				continue
			}
			if env.V > wireVersion {
				// 别的设备装了更新的版本: 停在这里, 本机升级后游标归零重拉
				a.s.docFailed(sp.Key, c.Collection, c.ID, fmt.Errorf("wire version %d is newer than %d", env.V, wireVersion))
				a.s.holdBefore(sp.Key, c.Rev)
				continue
			}
			data = env.Data
		}
		err := sp.Apply(c.Collection, c.ID, c.Rev, c.Deleted, data)
		switch {
		case errors.Is(err, space.ErrLocalChanged):
			continue
		case errors.Is(err, errcode.ErrSyncPayload):
			a.s.docFailed(sp.Key, c.Collection, c.ID, err)
			continue
		case err != nil:
			return err
		}
		if a.s.opts.OnApplied != nil {
			a.s.opts.OnApplied(sp.Key, c.Collection, c.ID)
		}
		applied++
	}
	if applied > 0 {
		a.s.emit(EventChanged, map[string]any{"space": sp.Key})
	}
	return nil
}

func (a adapter) Committed(_ context.Context, c syncsdk.Commit) error {
	sp, err := a.space(c.Space)
	if err != nil {
		return err
	}
	return sp.Commit(c.Collection, c.ID, c.Rev)
}

func (a adapter) ResetSpace(_ context.Context, remote string) error {
	sp, err := a.space(remote)
	if err != nil {
		return err
	}
	return sp.Reset()
}

// ─── 冲突 ─────────────────────────────────────────────────────────────────

// resolve 是冲突回调: 服务端版本落地, 本地版本另存为新 id 的「(冲突副本)」并推上去。
// 本地是删除、或服务端已删除时以服务端为准 (删除方的意图与修改方的意图都保住: 修改方另存副本)。
func (a adapter) resolve(_ context.Context, c syncsdk.Conflict) (syncsdk.Resolution, error) {
	sp, err := a.space(c.Local.Space)
	if err != nil {
		return syncsdk.Resolution{}, err
	}
	if c.Local.Deleted {
		return syncsdk.Resolution{Kind: syncsdk.KeepRemote}, nil
	}
	newID, err := sp.SaveConflictCopy(c.Local.Collection, c.Local.ID, conflictSuffix)
	if err != nil {
		// 本地内容读不出: 留在队列里下次再问, 不拿服务端版本盖掉它
		return syncsdk.Resolution{}, err
	}
	a.s.emit(EventConflict, ConflictInfo{Space: sp.Key, Collection: c.Local.Collection, ID: c.Local.ID, CopyID: newID})
	return syncsdk.Resolution{Kind: syncsdk.SaveAsCopy, CopyID: newID, CopyValue: json.RawMessage("null")}, nil
}

// ConflictInfo 是推给界面的冲突提示: 服务端版本已落地到 ID, 本地版本另存为 CopyID。
type ConflictInfo struct {
	Space      string `json:"space"`
	Collection string `json:"collection"`
	ID         string `json:"id"`
	CopyID     string `json:"copyId"`
}
