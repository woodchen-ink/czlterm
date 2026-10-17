package vault

import (
	"context"
	"fmt"
	"time"
)

// refreshFlight 合并同一会话代次的并发刷新; 完成前不影响已有条目读取。
type refreshFlight struct {
	gen  uint64
	done chan struct{}
	err  error
}

// Refresh 同步并加载最新条目; 同一会话的并发请求共用一次CLI同步。
func (v *Vault) Refresh(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	v.mu.Lock()
	if v.session == "" {
		v.mu.Unlock()
		return ErrLocked
	}
	job := v.refreshJob
	if job == nil || job.gen != v.sessionGen {
		job = &refreshFlight{gen: v.sessionGen, done: make(chan struct{})}
		v.refreshJob = job
		path, session := v.bwPathLocked(), v.session
		go v.refreshItems(job, path, session)
	}
	v.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-job.done:
		return job.err
	}
}

// refreshItems 在后台执行共享刷新, 只有原会话仍有效时才替换条目。
func (v *Vault) refreshItems(job *refreshFlight, path, session string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var items map[string]item
	_, err := v.run(ctx, path, session, nil, "sync")
	if err != nil {
		err = fmt.Errorf("3005 bw sync failed: %w", err)
	} else {
		items, err = v.loadItems(ctx, path, session)
	}
	v.mu.Lock()
	if job.gen != v.sessionGen {
		err = ErrLocked
	} else if err == nil {
		v.items = items
	}
	job.err = err
	if v.refreshJob == job {
		v.refreshJob = nil
	}
	close(job.done)
	v.mu.Unlock()
}
