package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestLintSubject(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		config  *Config
		errors  []string
	}{
		// Valid cases
		{
			name:    "valid: simple",
			subject: "feat: add new feature",
			config:  &Config{AllowedTypes: []string{"feat"}, MaxSubjectLength: 50},
			errors:  nil,
		},
		{
			name:    "valid: with scope",
			subject: "fix(api): correct a bug",
			config:  &Config{AllowedTypes: []string{"fix"}, MaxSubjectLength: 50},
			errors:  nil,
		},
		{
			name:    "valid: with breaking change",
			subject: "refactor(parser)!: simplify the logic",
			config:  &Config{AllowedTypes: []string{"refactor"}, MaxSubjectLength: 50},
			errors:  nil,
		},
		{
			name:    "valid: capital subject allowed",
			subject: "docs: Add documentation for API",
			config:  &Config{AllowedTypes: []string{"docs"}, MaxSubjectLength: 50, AllowCapitalSubject: true},
			errors:  nil,
		},
		{
			name:    "valid: scope required and present",
			subject: "test(auth): add more tests",
			config:  &Config{AllowedTypes: []string{"test"}, MaxSubjectLength: 50, RequireScope: true},
			errors:  nil,
		},
		{
			name:    "merge commit: github style",
			subject: "Merge pull request #123 from feature/branch",
			config:  &Config{},
			errors:  nil,
		},
		{
			name:    "merge commit: branch style",
			subject: "Merge branch 'feature/foo'",
			config:  &Config{},
			errors:  nil,
		},
		{
			name:    "merge commit: hash style",
			subject: "Merge 9d7b7c932575348d7a2768fc781960128d9b16f2 into 15a00c61be9c996611064f3cb94a388cbe40c3a2",
			config:  &Config{},
			errors:  nil,
		},
		// Invalid cases
		{
			name:    "invalid: wrong format",
			subject: "missing colon",
			config:  &Config{},
			errors:  []string{"format must be 'type(scope)?: subject' with lowercase type and a space after colon"},
		},
		{
			name:    "invalid: unknown type",
			subject: "unknown: some message",
			config:  &Config{AllowedTypes: []string{"feat", "fix"}},
			errors:  []string{"type 'unknown' is not allowed. Allowed: feat, fix"},
		},
		{
			name:    "invalid: scope required but missing",
			subject: "feat: missing scope",
			config:  &Config{AllowedTypes: []string{"feat"}, RequireScope: true},
			errors:  []string{"scope is required but missing"},
		},
		{
			name:    "invalid: scope not in allowed list",
			subject: "feat(invalid): scope not allowed",
			config:  &Config{AllowedTypes: []string{"feat"}, AllowedScopes: []string{"api", "ui"}},
			errors:  []string{"scope 'invalid' is not in allowed list: api, ui"},
		},
		{
			name:    "invalid: subject too long",
			subject: "fix: this subject is definitely way too long for the linter to accept",
			config:  &Config{AllowedTypes: []string{"fix"}, MaxSubjectLength: 20},
			errors:  []string{"subject too long (64 > 20)"},
		},
		{
			name:    "invalid: subject ends with period",
			subject: "docs: add some documentation.",
			config:  &Config{AllowedTypes: []string{"docs"}},
			errors:  []string{"subject must not end with a period"},
		},
		{
			name:    "invalid: subject starts with capital letter",
			subject: "style: Format the code",
			config:  &Config{AllowedTypes: []string{"style"}},
			errors:  []string{"subject should start lowercase (imperative mood)"},
		},
		{
			name:    "invalid: empty subject",
			subject: "chore: ",
			config:  &Config{AllowedTypes: []string{"chore"}},
			errors:  []string{"subject must not be empty"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			linter := &Linter{Config: tc.config}
			// Fill in default values for nil slices to avoid panics
			if linter.Config.AllowedTypes == nil {
				linter.Config.AllowedTypes = readEnvList("TYPES", "feat,fix,docs,style,refactor,perf,test,build,ci,chore,revert")
			}
			if linter.Config.MaxSubjectLength == 0 {
				linter.Config.MaxSubjectLength = 72
			}

			errs := linter.LintSubject(tc.subject)
			if len(errs) == 0 && len(tc.errors) == 0 {
				return
			}
			if !reflect.DeepEqual(errs, tc.errors) {
				t.Errorf("expected errors:\n%v\ngot:\n%v", tc.errors, errs)
			}
		})
	}
}

func TestReadEnvList(t *testing.T) {
	cases := []struct {
		name         string
		envValue     string
		defaultValue string
		expected     []string
	}{
		{"empty env, with default", "", "a,b,c", []string{"a", "b", "c"}},
		{"env set", "x, y, z", "a,b,c", []string{"x", "y", "z"}},
		{"env with extra spaces", "  one,  two  ", "", []string{"one", "two"}},
		{"empty env, empty default", "", "", nil},
		{"env is just whitespace", "   ", "a,b", []string{"a", "b"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("TEST_ENV_VAR", tc.envValue)
			defer os.Unsetenv("TEST_ENV_VAR")

			result := readEnvList("TEST_ENV_VAR", tc.defaultValue)
			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestGetEnvBool(t *testing.T) {
	cases := []struct {
		name         string
		value        string
		defaultValue bool
		expected     bool
	}{
		{"env not set", "", true, true},
		{"env not set, default false", "", false, false},
		{"true value", "true", false, true},
		{"yes value", "yes", false, true},
		{"on value", "on", false, true},
		{"1 value", "1", false, true},
		{"false value", "false", true, false},
		{"other value", "other", true, false},
		{"mixed case", "True", false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value != "" {
				// To test case-insensitivity, we alternate casing
				val := tc.value
				if len(val) > 1 {
					if tc.name[0]%2 == 0 {
						val = strings.ToUpper(val)
					} else {
						val = strings.ToLower(val)
					}
				}
				os.Setenv("TEST_BOOL_VAR", val)
				defer os.Unsetenv("TEST_BOOL_VAR")
			} else {
				os.Unsetenv("TEST_BOOL_VAR")
			}

			result := getEnvBool("TEST_BOOL_VAR", tc.defaultValue)
			if result != tc.expected {
				t.Errorf("For value '%s', expected %v, got %v", tc.value, tc.expected, result)
			}
		})
	}
}
