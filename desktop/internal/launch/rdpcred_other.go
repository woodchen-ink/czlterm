//go:build !windows

package launch

func storeRDPCredential(string, string, string) (func(), error) { return func() {}, nil }
