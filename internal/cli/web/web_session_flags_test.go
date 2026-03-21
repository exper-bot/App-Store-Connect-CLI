package web

import (
	"flag"
	"strings"
	"testing"
)

func TestBindWebSessionFlagsIncludesDeprecatedTwoFactorAlias(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags := bindWebSessionFlags(fs)

	if flags.twoFactorCode == nil {
		t.Fatal("expected deprecated two-factor-code pointer to be populated")
	}

	twoFactorCodeFlag := fs.Lookup(deprecatedTwoFactorCodeFlagName)
	if twoFactorCodeFlag == nil {
		t.Fatalf("expected --%s to be registered", deprecatedTwoFactorCodeFlagName)
	}
	if !strings.Contains(twoFactorCodeFlag.Usage, "Deprecated:") {
		t.Fatalf("expected deprecated help text, got %q", twoFactorCodeFlag.Usage)
	}

	if fs.Lookup("two-factor-code-command") == nil {
		t.Fatal("expected --two-factor-code-command to remain registered")
	}
}
