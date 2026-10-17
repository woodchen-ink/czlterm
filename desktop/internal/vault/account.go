package vault

import (
	"context"
	"encoding/json"
)

// loadAccountSummary 只在缺少账户摘要时后台补齐, 不阻塞本地凭据可用状态。
func (v *Vault) loadAccountSummary(gen uint64, path string) {
	out, err := v.run(context.Background(), path, "", nil, "status")
	var st Status
	if err == nil {
		err = json.Unmarshal(lastJSON(out), &st)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.accountPending = false
	if gen != v.sessionGen || err != nil {
		return
	}
	st.BWPath = path
	v.accountStatus = st
}
