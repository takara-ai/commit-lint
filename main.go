package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Config holds the linter's configuration.
type Config struct {
	AllowedTypes        []string
	AllowedScopes       []string
	RequireScope        bool
	RequireScopeExcept  []string
	AllowCapitalSubject bool
	MaxSubjectLength    int
}

// Linter is the main struct for linting operations.
type Linter struct {
	Config *Config
}

// NewLinter creates a new Linter with configuration from environment variables.
func NewLinter() *Linter {
	maxSubject, err := strconv.Atoi(os.Getenv("MAX_SUBJECT"))
	if err != nil {
		maxSubject = 72
	}

	return &Linter{
		Config: &Config{
			AllowedTypes:        readEnvList("TYPES", "feat,fix,docs,style,refactor,perf,test,build,ci,chore,revert"),
			AllowedScopes:       readEnvList("SCOPES", ""),
			RequireScope:        getEnvBool("REQUIRE_SCOPE", false),
			RequireScopeExcept:  readEnvList("REQUIRE_SCOPE_EXCEPT_TYPES", "revert"),
			AllowCapitalSubject: getEnvBool("ALLOW_CAPITAL_SUBJECT", false),
			MaxSubjectLength:    maxSubject,
		},
	}
}

// LintSubject checks a single commit subject against the linter's rules.
func (l *Linter) LintSubject(subject string) []string {
	var errors []string
	re := regexp.MustCompile(`^([a-z]+)(?:\(([a-z0-9][a-z0-9./-]*?)\))?(!)?: (.*)$`)
	matches := re.FindStringSubmatch(subject)

	if matches == nil {
		errors = append(errors, "format must be 'type(scope)?: subject' with lowercase type and a space after colon")
		return errors
	}

	ctype, scope, _, sub := matches[1], matches[2], matches[3], strings.TrimSpace(matches[4])

	if l.Config.AllowedTypes != nil && !contains(l.Config.AllowedTypes, ctype) {
		errors = append(errors, fmt.Sprintf("type '%s' is not allowed. Allowed: %s", ctype, strings.Join(l.Config.AllowedTypes, ", ")))
	}

	if l.Config.RequireScope && scope == "" && !contains(l.Config.RequireScopeExcept, ctype) {
		errors = append(errors, "scope is required but missing")
	}

	if scope != "" && l.Config.AllowedScopes != nil && !contains(l.Config.AllowedScopes, scope) {
		errors = append(errors, fmt.Sprintf("scope '%s' is not in allowed list: %s", scope, strings.Join(l.Config.AllowedScopes, ", ")))
	}

	if strings.TrimSpace(sub) == "" {
		errors = append(errors, "subject must not be empty")
	}

	if len(sub) > l.Config.MaxSubjectLength {
		errors = append(errors, fmt.Sprintf("subject too long (%d > %d)", len(sub), l.Config.MaxSubjectLength))
	}

	if strings.HasSuffix(sub, ".") {
		errors = append(errors, "subject must not end with a period")
	}

	if !l.Config.AllowCapitalSubject && len(sub) > 0 && sub[0] >= 'A' && sub[0] <= 'Z' {
		errors = append(errors, "subject should start lowercase (imperative mood)")
	}

	if len(errors) == 0 {
		return nil
	}
	return errors
}

func readEnvList(name, defaultValue string) []string {
	raw := os.Getenv(name)
	if strings.TrimSpace(raw) == "" {
		raw = defaultValue
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func getEnvBool(name string, defaultValue bool) bool {
	val := strings.ToLower(os.Getenv(name))
	if val == "" {
		return defaultValue
	}
	return val == "1" || val == "true" || val == "yes" || val == "on"
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// GetGitCommits fetches commits from the git repository.
func GetGitCommits(rangeSpec string, limit int) ([][2]string, error) {
	format := "%H\x00%s"
	args := []string{"log", "--no-merges", fmt.Sprintf("--pretty=format:%s", format)}
	if rangeSpec != "" {
		args = append(args, rangeSpec)
	}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}

	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "::warning title=commitlint::Could not read git log (%v). Falling back to HEAD\n", err)
		cmd = exec.Command("git", "log", "--no-merges", fmt.Sprintf("--pretty=format:%s", format), "-n", "1")
		out, err = cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("failed to get git commits: %w", err)
		}
	}

	var commits [][2]string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 2)
		if len(parts) == 2 {
			commits = append(commits, [2]string{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])})
		}
	}
	return commits, scanner.Err()
}

func inferRangeFromGithubEnv() string {
	eventName := os.Getenv("GITHUB_EVENT_NAME")
	baseRef := os.Getenv("GITHUB_BASE_REF")
	if strings.HasPrefix(eventName, "pull_request") && baseRef != "" {
		return fmt.Sprintf("origin/%s..HEAD", baseRef)
	}
	return ""
}

func main() {
	if getEnvBool("SKIP_FOR_BOT", true) {
		if os.Getenv("GITHUB_ACTOR") == "release-please[bot]" {
			fmt.Println("::notice title=commitlint::Skipping for release-please[bot].")
			os.Exit(0)
		}
	}

	linter := NewLinter()

	var rangeSpec string
	if len(os.Args) > 2 && os.Args[1] == "--range" {
		rangeSpec = os.Args[2]
	} else {
		rangeSpec = inferRangeFromGithubEnv()
	}

	commits, err := GetGitCommits(rangeSpec, 200)
	if err != nil {
		fmt.Fprintf(os.Stderr, "::error title=commitlint::Failed to get git commits: %v\n", err)
		os.Exit(1)
	}

	if len(commits) == 0 {
		fmt.Println("::warning title=commitlint::No commits found to lint.")
		os.Exit(0)
	}

	errorCount := 0
	for _, commit := range commits {
		sha, subj := commit[0], commit[1]
		errs := linter.LintSubject(subj)
		if len(errs) > 0 {
			errorCount += len(errs)
			shortSha := sha
			if len(sha) > 7 {
				shortSha = sha[:7]
			}
			for _, msg := range errs {
				fmt.Printf("::error title=commit %s::%s | '%s'\n", shortSha, msg, subj)
			}
		}
	}

	if errorCount > 0 {
		fmt.Println("::group::Commit lint summary")
		fmt.Printf("Found %d errors across %d commit(s).\n", errorCount, len(commits))
		fmt.Println("::endgroup::")
		os.Exit(1)
	}

	fmt.Println("All commit subjects comply with rules.")
} 