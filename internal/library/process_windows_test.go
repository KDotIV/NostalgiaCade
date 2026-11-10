//go:build windows

package library

import (
	"strings"
	"testing"
	"unicode/utf16"
)

func TestNativeEnvironmentPreservesUnicodeAndOverrides(t *testing.T) {
	block, err := nativeEnvironment([]string{"Path=original", "RPCS3_CONFIG_DIR=C:/old/", "rpcs3_config_dir=C:/Cabinet é/", "=C:=C:/cabinet", "PATH=override"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(utf16.Decode(block))
	if !strings.HasSuffix(text, "\x00\x00") {
		t.Fatal("environment is not double null terminated")
	}
	if strings.Contains(text, "original") || strings.Contains(text, "C:/old/") {
		t.Fatal("duplicate variable did not use last value")
	}
	for _, expected := range []string{"PATH=override\x00", "rpcs3_config_dir=C:/Cabinet é/\x00", "=C:=C:/cabinet\x00"} {
		if !strings.Contains(text, expected) {
			t.Fatal("lost environment entry", expected)
		}
	}
	if _, err = nativeEnvironment([]string{"INVALID=a\x00b"}); err == nil {
		t.Fatal("null accepted")
	}
}
