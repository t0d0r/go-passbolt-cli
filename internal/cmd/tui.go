package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/passbolt/go-passbolt-cli/internal/tui"
	"github.com/passbolt/go-passbolt-cli/internal/util"
	"github.com/passbolt/go-passbolt/api"
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
		// bubbletea draws on the raw terminal, not through util.Out, so the
		// check has to look at the same stream.
		if !term.IsTerminal(int(os.Stdout.Fd())) { //nolint:forbidigo
			return fmt.Errorf("tui needs an interactive terminal")
		}
		if demo, _ := cmd.Flags().GetBool("demo"); demo {
			cmd.SilenceUsage = true
			return runTUI(tui.NewDemoStore(), "https://passbolt.example.com (demo data)", nil)
		}

		loginCtx, cancel := util.GetContext()
		client, err := util.GetClient(loginCtx)
		if err == nil {
			// The MFA challenge comes with the first API call after login;
			// trigger it now, while the terminal is still in normal mode.
			_, err = client.GetMe(loginCtx)
		}
		cancel()
		if err != nil {
			return err
		}
		// Inside the UI nothing may prompt on the terminal: a renewed MFA
		// challenge (after a session timeout) fails with a clear message.
		client.MFACallback = func(context.Context, *api.Client, *api.APIResponse) (http.Cookie, error) {
			return http.Cookie{}, errors.New("the server asks for MFA again, quit and restart passbolt tui")
		}
		// Debug output would print requests and cookies into the UI.
		client.Debug = false
		cmd.SilenceUsage = true
		defer func() {
			ctx, cancel := util.GetContext()
			defer cancel()
			util.SaveSessionKeysAndLogout(ctx, client)
		}()

		store := tui.NewPassboltStore(client, int(viper.GetUint("workers")))
		err = runTUI(store, viper.GetString("serverAddress"), store.Skipped)
		return err
	},
}

func init() {
	tuiCmd.Flags().Bool("demo", false, "run on built-in made-up data, without a server (for screenshots and trying the UI)")
	_ = tuiCmd.Flags().MarkHidden("demo")
	rootCmd.AddCommand(tuiCmd)
}

// runTUI runs the UI on store until it quits or the terminal goes away,
// then clears a pending secret from the clipboard.
func runTUI(store tui.Store, server string, skipped func() int) error {
	// OSC 52 goes to the controlling terminal only, never to a possibly
	// redirected stdout/stderr.
	var tty io.Writer
	if f := tui.OpenTTY(); f != nil {
		defer func() { _ = f.Close() }()
		tty = f
	}
	clip := tui.NewSystemClipboard(tty)
	model := tui.New(store, clip, server, viper.GetDuration("timeout"))

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(context.Background()))
	if len(tuiExitSignals) > 0 {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, tuiExitSignals...)
		defer signal.Stop(sig)
		go func() {
			if _, ok := <-sig; ok {
				program.Kill()
			}
		}()
	}
	_, runErr := program.Run()
	if msg := model.ClearClipboard(); msg != "" {
		util.Eprintln("Warning:", msg, "- clear your clipboard manually")
	}
	if errors.Is(runErr, tea.ErrProgramKilled) {
		runErr = nil // ended by a signal; clipboard and logout handled
	}
	if skipped != nil && skipped() > 0 {
		n := skipped()
		util.Eprintf("Note: %d resources of unsupported types were not shown\n", n)
	}
	return runErr
}
