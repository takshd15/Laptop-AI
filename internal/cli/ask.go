package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/takshd15/laptop-ai/internal/audit"
	"github.com/takshd15/laptop-ai/internal/config"
	"github.com/takshd15/laptop-ai/internal/llm"
)

var askCmd = &cobra.Command{
	Use:   "ask <question>",
	Short: "Ask a question using indexed local files",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		question := strings.TrimSpace(strings.Join(args, " "))
		if question == "" {
			return fmt.Errorf("question cannot be empty")
		}

		chunks, err := searchChunks(cfg.DataDir, question, defaultTopK)
		if err != nil {
			return err
		}
		if len(chunks) == 0 {
			fmt.Println("No indexed context found. Run: laptop-ai index <folder>")
			return nil
		}

		auditLog, err := audit.Open(cfg.DataDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: cannot open audit log: %v\n", err)
			auditLog = audit.Nop()
		}
		defer auditLog.Close()
		auditLog.LogQuery("local", uniqueSourcePaths(chunks))

		model := llm.NewOllama()
		fmt.Println("Answer:")
		var answer strings.Builder
		if err := model.Stream(context.Background(), question, chunks, func(token string) {
			fmt.Print(token)
			answer.WriteString(token)
		}); err != nil {
			return err
		}
		fmt.Print("\n")
		fmt.Print(llm.FormatCitedSources(answer.String(), chunks))
		return nil
	},
}
