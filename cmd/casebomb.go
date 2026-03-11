package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/supermodeltools/uncompact/internal/casedata"
	"github.com/supermodeltools/uncompact/internal/template"
)

var (
	caseBombCaseID       string
	caseBombDBDir        string
	caseBombMaxTokens    int
	caseBombLastMessages int
)

var caseBombCmd = &cobra.Command{
	Use:   "case-bomb",
	Short: "Generate a case context bomb from local SQLite databases",
	Long: `Reads litigation case data from case_registry.db and ops.db, then
renders a token-budgeted Markdown context bomb for injection into an LLM
system prompt.

Output goes to stdout. Exit 0 on success, non-zero on error.
Designed to be called by cc-api via subprocess.`,
	SilenceUsage: true,
	RunE:         caseBombHandler,
	// Skip the global auth check — case-bomb doesn't need Supermodel API
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
}

func init() {
	caseBombCmd.Flags().StringVar(&caseBombCaseID, "case-id", "", "Case file number (required)")
	caseBombCmd.Flags().StringVar(&caseBombDBDir, "db-dir", "", "Directory containing SQLite databases (default: ~/.gastown)")
	caseBombCmd.Flags().IntVar(&caseBombMaxTokens, "max-tokens", 5000, "Maximum tokens in output")
	caseBombCmd.Flags().IntVar(&caseBombLastMessages, "last-messages", 10, "Number of recent chat messages to include")
	_ = caseBombCmd.MarkFlagRequired("case-id")
	rootCmd.AddCommand(caseBombCmd)
}

func caseBombHandler(cmd *cobra.Command, args []string) error {
	if caseBombCaseID == "" {
		return fmt.Errorf("--case-id is required")
	}

	// Resolve DB directory
	dbDir := caseBombDBDir
	if dbDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		dbDir = filepath.Join(home, ".gastown")
	}

	// Verify case_registry.db exists
	regDB := filepath.Join(dbDir, "case_registry.db")
	if _, err := os.Stat(regDB); os.IsNotExist(err) {
		return fmt.Errorf("case_registry.db not found at %s", regDB)
	}

	// Load timeline data
	timeline, err := casedata.LoadTimeline(dbDir, caseBombCaseID, caseBombLastMessages)
	if err != nil {
		return fmt.Errorf("load timeline: %w", err)
	}

	// Render case bomb
	output, tokens := template.RenderCaseBomb(timeline, caseBombMaxTokens)

	if debug {
		fmt.Fprintf(os.Stderr, "[case-bomb] case=%s tokens=%d\n", caseBombCaseID, tokens)
	}

	fmt.Print(output)
	return nil
}
