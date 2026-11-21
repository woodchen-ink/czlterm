package cloudsync

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/woodchen-ink/go-web-utils/syncsdk"

	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
)

// 端到端加密的密钥操作: 开通、恢复码、新设备授权。密码学全在 SDK 里, 这里只编排与换错误码。

func (s *Service) keyManager() (*syncsdk.KeyManager, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keys == nil {
		return nil, errcode.ErrAuthExpired
	}
	return s.keys, nil
}

// Enroll 首次开通同步: 生成密钥对与恢复码。恢复码只此一次, 界面必须让用户保存并确认。
// 账号已在别的设备开通过时返回 ErrSyncEnrolled, 应改走恢复码或旧设备批准。
func (s *Service) Enroll(ctx context.Context) (string, error) {
	keys, err := s.keyManager()
	if err != nil {
		return "", err
	}
	code, err := keys.Enroll(ctx)
	if errors.Is(err, syncsdk.ErrConflict) {
		return "", errcode.ErrSyncEnrolled.Wrap(err)
	}
	if err != nil {
		return "", classify(err)
	}
	defer code.Destroy()
	out := code.String()
	go s.refreshSoon()
	return out, nil
}

// Unlock 在本机没有私钥时用恢复码解锁。
func (s *Service) Unlock(ctx context.Context, recoveryCode string) error {
	keys, err := s.keyManager()
	if err != nil {
		return err
	}
	if err := keys.UnlockWithRecoveryCode(ctx, recoveryCode); err != nil {
		return classify(err)
	}
	go s.refreshSoon()
	return nil
}

// ResetRecoveryCode 生成新恢复码, 旧的作废。只在已解锁的设备上可用。
func (s *Service) ResetRecoveryCode(ctx context.Context) (string, error) {
	keys, err := s.keyManager()
	if err != nil {
		return "", err
	}
	code, err := keys.ResetRecoveryCode(ctx)
	if err != nil {
		return "", classify(err)
	}
	defer code.Destroy()
	return code.String(), nil
}

func (s *Service) refreshSoon() {
	s.mu.Lock()
	root := s.root
	s.mu.Unlock()
	if root == nil {
		return
	}
	ctx, cancel := context.WithTimeout(root, 30*time.Second)
	defer cancel()
	s.Refresh(ctx)
}

// ─── 新设备: 本机等待旧设备批准 ─────────────────────────────────────────────

// DeviceProgress 是本机作为新设备等待批准的进度。Stage: code (显示校验码, 等旧设备确认) /
// approved (已解锁) / failed (过期、被拒、出错, Error 为错误串)。
type DeviceProgress struct {
	Stage string `json:"stage"`
	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
}

// StartDeviceEnrollment 发起新设备授权, 进度经 EventDevice 推送: 先推 6 位校验码,
// 旧设备上确认码一致并批准后推 approved。请求 10 分钟过期。
func (s *Service) StartDeviceEnrollment(ctx context.Context) error {
	keys, err := s.keyManager()
	if err != nil {
		return err
	}
	s.CancelDeviceEnrollment()
	enr, err := keys.StartDeviceEnrollment(ctx, s.opts.DeviceName)
	if err != nil {
		return classify(err)
	}
	s.mu.Lock()
	s.enrolling = enr
	root := s.root
	s.mu.Unlock()

	go func() {
		wctx, cancel := context.WithDeadline(root, enr.ExpiresAt())
		defer cancel()
		fail := func(err error) {
			if wctx.Err() == context.Canceled && root.Err() != nil {
				return // 退出登录
			}
			s.emit(EventDevice, DeviceProgress{Stage: "failed", Error: classify(err).Error()})
		}
		code, err := enr.WaitCode(wctx)
		if err != nil {
			fail(err)
			return
		}
		s.emit(EventDevice, DeviceProgress{Stage: "code", Code: code})
		if err := enr.WaitApproval(wctx); err != nil {
			fail(err)
			return
		}
		s.emit(EventDevice, DeviceProgress{Stage: "approved"})
		s.refreshSoon()
	}()
	return nil
}

// CancelDeviceEnrollment 撤回进行中的新设备授权。
func (s *Service) CancelDeviceEnrollment() {
	s.mu.Lock()
	enr := s.enrolling
	s.enrolling = nil
	s.mu.Unlock()
	if enr != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := enr.Cancel(ctx); err != nil {
			log.Printf("sync.device_cancel: %v", err)
		}
	}
}

// ─── 已解锁的设备: 批准新设备 ───────────────────────────────────────────────

// DeviceRequest 是等待批准的新设备。
type DeviceRequest struct {
	ID         string    `json:"id"`
	DeviceName string    `json:"deviceName"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// PendingDevices 列出等待本账号批准的新设备。
func (s *Service) PendingDevices(ctx context.Context) ([]DeviceRequest, error) {
	keys, err := s.keyManager()
	if err != nil {
		return nil, err
	}
	reqs, err := keys.PendingDeviceRequests(ctx)
	if err != nil {
		return nil, classify(err)
	}
	out := make([]DeviceRequest, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, DeviceRequest{ID: r.ID, DeviceName: r.DeviceName, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt})
	}
	return out, nil
}

// BeginDeviceApproval 开始批准一台新设备, 返回 6 位校验码: 两台设备显示同一个码才可批准。
func (s *Service) BeginDeviceApproval(ctx context.Context, requestID string) (string, error) {
	keys, err := s.keyManager()
	if err != nil {
		return "", err
	}
	reqs, err := keys.PendingDeviceRequests(ctx)
	if err != nil {
		return "", classify(err)
	}
	for _, r := range reqs {
		if r.ID != requestID {
			continue
		}
		ap, err := keys.BeginDeviceApproval(ctx, r)
		if err != nil {
			return "", classify(err)
		}
		wctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		code, err := ap.WaitCode(wctx)
		if err != nil {
			return "", classify(err)
		}
		s.mu.Lock()
		s.approvals[requestID] = ap
		s.mu.Unlock()
		return code, nil
	}
	return "", errcode.ErrDeviceRequest
}

// FinishDeviceApproval 在用户核对校验码后批准 (approve 为 true) 或拒绝。
func (s *Service) FinishDeviceApproval(ctx context.Context, requestID string, approve bool) error {
	s.mu.Lock()
	ap := s.approvals[requestID]
	delete(s.approvals, requestID)
	s.mu.Unlock()
	if ap == nil {
		return errcode.ErrDeviceRequest
	}
	var err error
	if approve {
		err = ap.Approve(ctx)
	} else {
		err = ap.Reject(ctx)
	}
	return classify(err)
}
