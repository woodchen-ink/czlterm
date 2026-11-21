// Package errcode 定义账号、团队、收款与云同步模块的对外错误。
//
// 与程序其它部分一致, Error() 是「四位数字码 + 英文短语」, 前端按码映射成界面语言并展示码。
// 原始异常 (HTTP 响应体、文件路径) 只经 Unwrap 进日志, 不随 Error() 外泄。
//
// 号段: 61xx 账号登录, 62xx 团队, 63xx 收款与订阅, 64xx 云同步与端到端加密。
// 6001–6010 曾是已移除的 git 同步, 不复用。
package errcode

import "fmt"

// Error 是对外错误。Code 稳定, Phrase 是补充说明, cause 只用于日志。
type Error struct {
	Code   int    `json:"code"`
	Phrase string `json:"phrase"`
	cause  error
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s", e.Code, e.Phrase) }

func (e *Error) Unwrap() error { return e.cause }

// Is 按错误码判等, 包装过的错误同样认得出。
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// New 创建错误。
func New(code int, phrase string) *Error { return &Error{Code: code, Phrase: phrase} }

// Wrap 附上原始异常, Error() 仍只有码与短语。
func (e *Error) Wrap(cause error) *Error { return &Error{Code: e.Code, Phrase: e.Phrase, cause: cause} }

// ErrPayloadCodec 是请求体编解码失败 (程序缺陷, 不该出现)。
var ErrPayloadCodec = New(6400, "payload_codec_failed")

// ─── 61xx 账号登录 ─────────────────────────────────────────────────────────

var (
	// 没有进行中的登录或已超时 (回调来得太晚、重复回调)
	ErrAuthNotPending = New(6101, "auth_not_pending")
	// 回调 state 与发起时不一致
	ErrAuthState = New(6102, "auth_state_mismatch")
	// 用户拒绝授权或授权码无效
	ErrAuthDenied = New(6103, "auth_denied")
	// 未登录, 或登录已失效需重新登录
	ErrAuthExpired = New(6104, "auth_session_expired")
	// 联系不上登录服务
	ErrAuthRequest = New(6105, "auth_request_failed")
	// 登录服务返回了认不出的内容
	ErrAuthResponse = New(6106, "auth_response_invalid")
	// 本机登录记录读写失败 (系统钥匙串不可用)
	ErrAuthVault = New(6107, "auth_vault_failed")
	// 退出登录时还有未上传的修改
	ErrAuthLogoutUnpushed = New(6108, "logout_blocked_by_unpushed")
	// 本版本没有配置 Connect 客户端 id, 登录未开通
	ErrAuthNotConfigured = New(6109, "auth_not_configured")
)

// ─── 62xx 团队 ─────────────────────────────────────────────────────────────

var (
	// 团队接口拒绝请求 (参数不对、邀请码无效或已用完)
	ErrOrgRejected = New(6201, "org_request_rejected")
	// 角色不够
	ErrOrgForbidden = New(6202, "org_forbidden")
	// 团队不存在或已不是成员
	ErrOrgNotFound = New(6203, "org_not_found")
	// 联系不上团队接口
	ErrOrgRequest = New(6204, "org_request_failed")
)

// ─── 63xx 收款与订阅 ───────────────────────────────────────────────────────

var (
	// 收款接口拒绝请求
	ErrBillingRejected = New(6301, "billing_request_rejected")
	// 套餐不支持所选年限
	ErrBillingPeriods = New(6302, "billing_periods_unsupported")
	// 角色不够 (团队只有 owner / admin 能买)
	ErrBillingForbidden = New(6303, "billing_forbidden")
	// 应用没开通收款
	ErrBillingDisabled = New(6304, "billing_disabled")
	// 联系不上收款接口
	ErrBillingRequest = New(6305, "billing_request_failed")
)

// ─── 64xx 云同步与端到端加密 ───────────────────────────────────────────────

var (
	// 同步服务连不上
	ErrSyncUnavailable = New(6401, "sync_unavailable")
	// 文档内容认不出或解不开
	ErrSyncPayload = New(6402, "sync_payload_invalid")
	// 同步请求失败
	ErrSyncRequest = New(6403, "sync_request_failed")
	// 本机不认识这个空间
	ErrSpaceUnknown = New(6404, "space_unknown")
	// 空间只读 (查看者、订阅到期、已被移出团队)
	ErrSyncReadOnly = New(6405, "sync_space_read_only")
	// 空间里还有未上传的修改
	ErrSpaceHasPending = New(6406, "space_has_unpushed_changes")
	// 应用没开通数据同步
	ErrSyncNotEnabled = New(6407, "sync_not_enabled")
	// 账号已被封禁
	ErrSyncAccountDisabled = New(6408, "sync_account_disabled")
	// 本机没有解密密钥: 需要恢复码或已登录设备批准
	ErrSyncLocked = New(6409, "sync_locked")
	// 账号已在别的设备开通过端到端加密
	ErrSyncEnrolled = New(6410, "sync_already_enrolled")
	// 恢复码不对
	ErrSyncRecoveryCode = New(6411, "recovery_code_invalid")
	// 设备授权请求不存在、已过期或已被拒绝
	ErrDeviceRequest = New(6412, "device_request_invalid")
	// 目标空间与来源相同
	ErrSpaceSame = New(6413, "space_same_as_source")
	// 同一 id 已在本机另一个空间
	ErrSpaceMismatch = New(6414, "id_in_other_space")
	// 要复制的文件夹里没有连接
	ErrGroupEmpty = New(6417, "group_empty")
)
