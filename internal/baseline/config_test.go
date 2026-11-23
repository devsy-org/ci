package baseline

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

const (
	allowedLinters = "cyclop,funlen,gocognit,gocyclo,nestif,revive"
	normalConfig   = "version: '2'\n"
	configFunlen   = "funlen"
	blockedSetting = "base config uses"
	stringKeyError = "expected a string YAML mapping key"
)

func TestConfigCheck(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, source, linters, want string }{
		{"normal", normalConfig, allowedLinters, "\n"},
		{"tags", "RUN: {Build-Tags: [integration, MixedCase]}\n", "revive", "integration,MixedCase\n"},
		{"value alias", "tags: &tags [integration]\nRUN: {Build-Tags: *tags}\n", configFunlen, "integration\n"},
		{"disabled linters", "linters: {settings: {goheader: {template-path: test.txt}}}\n", configFunlen, "\n"},
		{"unknown linter", normalConfig, "revive,gosec", "from the allowlist"},
		{"empty linter", normalConfig, "revive,", "from the allowlist"},
		{"custom via alias", "other: &settings {Custom: {}}\nLinters: {Settings: *settings}\n", configFunlen, blockedSetting},
		{"plugins", "other: [{PlUgInS: null}]\n", configFunlen, blockedSetting},
		{"revive config", "Linters: {Settings: {Revive: {Config: rules.toml}}}\n", configFunlen, blockedSetting},
		{"multiple documents", "version: '2'\n---\n{}\n", configFunlen, "expected exactly one YAML document"},
		{"folded collision", "run: {}\nRUN: {}\n", configFunlen, "duplicate case-insensitive config key"},
		{"alias key", "name: &k example\n*k: anything\n", configFunlen, stringKeyError},
		{"merge key", "name: &m {a: b}\nother: {<<: *m}\n", configFunlen, stringKeyError},
		{"numeric key", "other: {123: any}\n", configFunlen, stringKeyError},
		{"tag value type", "run: {build-tags: [1]}\n", configFunlen, "expected run.build-tags to be a list of strings"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertConfigCheck(t, tc.source, tc.linters, tc.want)
		})
	}
}

func TestConfigCheckBinaryKeys(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, source string }{
		{"custom", "linters: {settings: { %s: {} }}"},
		{"settings", "linters: { %s: {} }"},
		{"linters", "{ %s: {} }"},
		{"config", "linters: {settings: {revive: { %s: rules.toml }}}"},
		{"CuStOm", "linters: {settings: { %s: {} }}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			key := "!!binary " + base64.StdEncoding.EncodeToString([]byte(tc.name))
			assertConfigCheck(
				t,
				fmt.Sprintf(tc.source, key),
				configFunlen,
				stringKeyError,
			)
		})
	}
}

func assertConfigCheck(t *testing.T, source, linters, want string) {
	t.Helper()
	file := fixtureFile(t, t.TempDir(), "config.yaml", source)
	var stdout, stderr bytes.Buffer
	code := Run(
		[]string{configCheckCommand, "--config", file, "--linters", linters},
		&stdout,
		&stderr,
	)
	ok := code == 2 && stdout.Len() == 0 && strings.Contains(stderr.String(), want)
	if strings.HasSuffix(want, "\n") {
		ok = code == 0 && stdout.String() == want && stderr.Len() == 0
	}
	if !ok {
		t.Fatalf("exit %d, stdout %q, stderr %q; expected %q", code, &stdout, &stderr, want)
	}
}
