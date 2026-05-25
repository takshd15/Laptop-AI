package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/takshd15/laptop-ai/internal/audit"
	"github.com/takshd15/laptop-ai/internal/config"
	"github.com/takshd15/laptop-ai/internal/indexer"
	"github.com/takshd15/laptop-ai/internal/security"
)

var rootCmd = &cobra.Command{
	Use:   "laptop-ai",
	Short: "A local AI memory assistant",
	Long:  "laptop-ai indexes your files and lets you ask questions using local models.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(indexCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(askCmd)
}

// — init —

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the local database",
	RunE: func(cmd *cobra.Command, args []string) error {
		return config.Init()
	},
}

// — index —

var indexConfirm bool

func init() {
	indexCmd.Flags().BoolVar(&indexConfirm, "confirm", false,
		"preview which files will be indexed and ask for confirmation before proceeding")
}

var indexCmd = &cobra.Command{
	Use:   "index <folder>",
	Short: "Scan a folder and index its files",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		folder := args[0]

		// Security: validate the folder before adding to allowlist
		if err := security.ValidateFolder(folder); err != nil {
			return fmt.Errorf("security check failed: %w", err)
		}

		if indexConfirm {
			fmt.Printf("Folder:  %s\n", folder)
			fmt.Printf("Allowed: %v\n\n", append(cfg.AllowedFolders, folder))
			fmt.Print("Proceed with indexing? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "y") {
				fmt.Println("Cancelled.")
				return nil
			}
		}

		// Open audit log — records every indexed/skipped file without storing content
		auditLog, err := audit.Open(cfg.DataDir)
		if err != nil {
			// Non-fatal: continue without audit log rather than blocking indexing
			fmt.Fprintf(os.Stderr, "warn: cannot open audit log: %v\n", err)
			auditLog = audit.Nop()
		}
		defer auditLog.Close()

		// Register the folder in the allowlist before running so the checker accepts it
		cfg.AddFolder(folder)

		fmt.Printf("Scanning %s\n\n", folder)

		result, err := indexer.Run(folder, cfg.DataDir, cfg.AllowedFolders, auditLog)
		if err != nil {
			return err
		}

		recordsToIndex := result.Records
		if len(recordsToIndex) == 0 {
			vectorCount, err := vectorRecordCount(cfg.DataDir)
			if err != nil {
				return err
			}
			if vectorCount == 0 {
				recordsToIndex, err = indexer.AllFiles(cfg.DataDir)
				if err != nil {
					return err
				}
				if len(recordsToIndex) > 0 {
					fmt.Println("Vector DB is empty; backfilling chunks from existing index metadata.")
				}
			}
		}

		chunksIndexed, err := indexRecords(cfg.DataDir, recordsToIndex)
		if err != nil {
			return err
		}

		if err := cfg.Save(); err != nil {
			return fmt.Errorf("cannot update config: %w", err)
		}

		fmt.Printf("\nDone.\n")
		fmt.Printf("  Indexed:         %d\n", result.Indexed)
		fmt.Printf("  Chunks indexed:  %d\n", chunksIndexed)
		fmt.Printf("  Skipped (unchanged):  %d\n", result.Skipped-result.SkippedSecret-result.SkippedDenied)
		if result.SkippedDenied > 0 {
			fmt.Printf("  Skipped (denylist):   %d\n", result.SkippedDenied)
		}
		if result.SkippedSecret > 0 {
			fmt.Printf("  Skipped (secrets):    %d  ← audit log has details\n", result.SkippedSecret)
		}
		fmt.Printf("  Total files seen: %d\n", result.Total)
		return nil
	},
}

// — stats —

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show database statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		fileCount, err := indexer.FileCount(cfg.DataDir)
		if err != nil {
			return err
		}
		cfg.PrintStats(fileCount)
		return nil
	},
}
