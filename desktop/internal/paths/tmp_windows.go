package paths

// PrivateTemp 在 Windows 上就是 cache/launch: %LOCALAPPDATA% 本身仅当前用户可访问, 路径也不受 socket 长度限制。
func PrivateTemp() (string, error) {
	return Cache("launch")
}
