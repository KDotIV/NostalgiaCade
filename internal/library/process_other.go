//go:build !windows

package library

func startNative(string, []string, string) (runningProcess, error) {
	return nil, fail("unsupported_platform", "Dolphin session management currently targets Windows")
}
func recoverNative(int, string) (runningProcess, error) {
	return nil, fail("unsupported_platform", "session recovery currently targets Windows")
}
