package security_test

import (
	"strings"
	"testing"

	"github.com/takshd15/laptop-ai/internal/security"
)

func TestSecretDetector_APIKey(t *testing.T) {
	// Value must be ≥ 20 chars to trigger the generic API_KEY pattern.
	text := "API_KEY=verylongapikeythatisfakeandhas40chars1234"
	if matches := security.ScanForSecrets(text); len(matches) == 0 {
		t.Error("API_KEY= with long value should be detected, got no matches")
	}
}

func TestSecretDetector_OpenAIKey(t *testing.T) {
	text := "sk-" + strings.Repeat("A", 48)
	if matches := security.ScanForSecrets(text); len(matches) == 0 {
		t.Error("OpenAI-style API key (sk-<48 chars>) should be detected, got no matches")
	}
}

func TestSecretDetector_GitHubPAT(t *testing.T) {
	text := "ghp_" + strings.Repeat("a", 36)
	if matches := security.ScanForSecrets(text); len(matches) == 0 {
		t.Error("GitHub PAT (ghp_<36 chars>) should be detected, got no matches")
	}
}

func TestSecretDetector_AWSAccessKeyID(t *testing.T) {
	text := "AKIA" + strings.Repeat("A", 16)
	if matches := security.ScanForSecrets(text); len(matches) == 0 {
		t.Error("AWS Access Key ID (AKIA<16 chars>) should be detected, got no matches")
	}
}

func TestSecretDetector_AnthropicKey(t *testing.T) {
	text := "sk-ant-" + strings.Repeat("x", 40)
	if matches := security.ScanForSecrets(text); len(matches) == 0 {
		t.Error("Anthropic API key (sk-ant-<40+>) should be detected, got no matches")
	}
}

func TestSecretDetector_DatabaseURL(t *testing.T) {
	text := "postgres://admin:supersecretpassword@db.example.com:5432/mydb"
	if matches := security.ScanForSecrets(text); len(matches) == 0 {
		t.Error("database URL with embedded credentials should be detected, got no matches")
	}
}

func TestSecretDetector_CleanText(t *testing.T) {
	text := "This is a normal README. It talks about programming. There are no secrets here."
	if matches := security.ScanForSecrets(text); len(matches) != 0 {
		var names []string
		for _, m := range matches {
			names = append(names, m.Type)
		}
		t.Errorf("clean text should produce no matches, got: %v", names)
	}
}

func TestSecretDetector_MultiplePatterns(t *testing.T) {
	text := "ghp_" + strings.Repeat("b", 36) + "\n" + "AKIA" + strings.Repeat("B", 16)
	matches := security.ScanForSecrets(text)
	if len(matches) < 2 {
		t.Errorf("expected at least 2 distinct secret types, got %d", len(matches))
	}
}

// TestRedactSecrets verifies that the secret value is removed from text and
// replaced with a safe placeholder.
func TestRedactSecrets_RemovesValue(t *testing.T) {
	secret := "sk-" + strings.Repeat("Z", 48)
	text := "Here is my key: " + secret + " — keep it safe."
	redacted := security.RedactSecrets(text)

	if strings.Contains(redacted, secret) {
		t.Error("RedactSecrets left the secret value in the output")
	}
	if !strings.Contains(redacted, "[REDACTED:") {
		t.Errorf("RedactSecrets should include [REDACTED:...] placeholder, got: %q", redacted)
	}
}

func TestRedactSecrets_CleanText_Unchanged(t *testing.T) {
	text := "No secrets in this line at all."
	if got := security.RedactSecrets(text); got != text {
		t.Errorf("RedactSecrets changed clean text: got %q, want %q", got, text)
	}
}
