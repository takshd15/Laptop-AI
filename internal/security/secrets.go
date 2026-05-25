package security

import (
	"regexp"
	"strings"
)

// SecretMatch records a detected secret type without including the actual secret value.
// Never log the matched text — that defeats the entire purpose.
type SecretMatch struct {
	Type string // e.g. "OpenAI API key" — safe to display
}

// secretPattern pairs a human-readable name with a detection regex.
type secretPattern struct {
	name    string
	pattern *regexp.Regexp
}

// secretPatterns is the list of patterns we scan for.
// Each pattern is compiled once at startup. Adding a pattern here is the only
// change needed to extend secret detection.
//
// Design principle: prefer precision over recall. A false positive means a useful
// chunk is silently dropped. A false negative means a secret is indexed — bad,
// but the user chose which folders to index, so the blast radius is limited.
var secretPatterns = []secretPattern{
	{
		"Private key header",
		regexp.MustCompile(`-----BEGIN\s+(RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY-----`),
	},
	{
		"OpenAI API key",
		regexp.MustCompile(`sk-[A-Za-z0-9]{48}`),
	},
	{
		"Anthropic API key",
		regexp.MustCompile(`sk-ant-[A-Za-z0-9\-_]{40,}`),
	},
	{
		"GitHub personal access token",
		regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`),
	},
	{
		"GitHub fine-grained PAT",
		regexp.MustCompile(`github_pat_[A-Za-z0-9_]{82}`),
	},
	{
		"AWS Access Key ID",
		regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	},
	{
		"Google API key",
		regexp.MustCompile(`AIza[0-9A-Za-z\-_]{35}`),
	},
	{
		"Stripe secret/publishable key",
		regexp.MustCompile(`(sk|pk)_(test|live)_[0-9a-zA-Z]{24,}`),
	},
	{
		"Database URL with embedded credentials",
		// matches postgres://user:pass@host — the :pass@ part distinguishes it from bare URLs
		regexp.MustCompile(`(?i)(mysql|postgres|postgresql|mongodb|redis|mariadb)://[^:/?#\s]+:[^@\s]+@`),
	},
	{
		"Password assignment",
		// matches lines like:  password = "hunter2"  or  passwd: mysecret
		// anchored to line start to reduce false positives
		regexp.MustCompile(`(?im)^(password|passwd|pwd)\s*[:=]\s*\S{6,}`),
	},
	{
		"Generic secret/token/key assignment",
		// matches: SECRET_KEY = "abc123..."  or  api_key: "xyz..."  (>= 20 chars value)
		regexp.MustCompile(`(?im)^[A-Z_]*(SECRET|TOKEN|API_KEY|PRIVATE_KEY)[A-Z_]*\s*[:=]\s*['"]?[A-Za-z0-9+/._\-]{20,}['"]?`),
	},
}

// ScanForSecrets checks text for patterns that look like secrets or credentials.
// Returns a slice of SecretMatch — one per detected pattern type.
// The matched text is never included in the result.
func ScanForSecrets(text string) []SecretMatch {
	var matches []SecretMatch
	seen := make(map[string]bool)

	for _, sp := range secretPatterns {
		if sp.pattern.MatchString(text) && !seen[sp.name] {
			matches = append(matches, SecretMatch{Type: sp.name})
			seen[sp.name] = true
		}
	}
	return matches
}

// ContainsSecrets is a convenience wrapper that returns true if any secret is found.
func ContainsSecrets(text string) bool {
	return len(ScanForSecrets(text)) > 0
}

// DescribeMatches returns a human-readable summary of detected secret types.
// Safe to log — never includes the actual secret values.
func DescribeMatches(matches []SecretMatch) string {
	types := make([]string, len(matches))
	for i, m := range matches {
		types[i] = m.Type
	}
	return strings.Join(types, ", ")
}
