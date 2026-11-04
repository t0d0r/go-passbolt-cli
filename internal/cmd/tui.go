package cmd

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/passbolt/go-passbolt-cli/internal/tui"
	"github.com/passbolt/go-passbolt-cli/internal/util"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Browse folders and resources in a terminal UI",
	Long: `Opens a full-screen terminal UI to browse folders and resources, search
across all resources and copy a username, URI, password or TOTP code.
Copied passwords and TOTP codes are cleared from the clipboard after 30
seconds and on exit. Press ? inside the UI for the keys.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !term.IsTerminal(int(os.Stdout.Fd())) {
			return fmt.Errorf("tui needs an interactive terminal")
		}

		loginCtx, cancel := util.GetContext()
		client, err := util.GetClient(loginCtx)
		cancel()
		if err != nil {
			return err
		}
		cmd.SilenceUsage = true
		defer func() {
			ctx, cancel := util.GetContext()
			defer cancel()
			util.SaveSessionKeysAndLogout(ctx, client)
		}()

		store := tui.NewPassboltStore(client, int(viper.GetUint("workers")))
		clip := tui.NewSystemClipboard(os.Stderr)
		model := tui.New(store, clip, viper.GetString("serverAddress"), viper.GetDuration("timeout"))

		_, runErr := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(context.Background())).Run()
		model.ClearClipboard()
		if n := store.Skipped(); n > 0 {
			fmt.Fprintf(os.Stderr, "Note: %d resources of unsupported types were not shown\n", n)
		}
		return runErr
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
