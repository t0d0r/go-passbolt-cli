package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/passbolt/go-passbolt-cli/internal/util"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// wizardMaxAttempts is how often an invalid answer is asked again.
const wizardMaxAttempts = 3

var errWizardAborted = errors.New("wizard aborted, nothing saved")

// wizardIO reads answers line by line and writes prompts.
type wizardIO struct {
	in  *bufio.Reader
	out io.Writer
}

func (w *wizardIO) printf(format string, a ...any) {
	_, _ = fmt.Fprintf(w.out, format, a...)
}

// ask shows label with def and returns the answer, or def on an empty line.
func (w *wizardIO) ask(label, def string) (string, error) {
	if def != "" {
		w.printf("%s [%s]: ", label, def)
	} else {
		w.printf("%s: ", label)
	}
	line, err := w.in.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		w.printf("\n")
		return "", fmt.Errorf("reading answer: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

// confirm asks a yes/no question.
func (w *wizardIO) confirm(label string, def bool) (bool, error) {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	for range wizardMaxAttempts {
		w.printf("%s [%s]: ", label, hint)
		line, err := w.in.ReadString('\n')
		if err != nil && (!errors.Is(err, io.EOF) || line == "") {
			w.printf("\n")
			return false, fmt.Errorf("reading answer: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		w.printf("Please answer y or n.\n")
	}
	return false, errWizardAborted
}

// choose asks for one of options.
func (w *wizardIO) choose(label string, options []string, def string) (string, error) {
	for range wizardMaxAttempts {
		answer, err := w.ask(fmt.Sprintf("%s (%s)", label, strings.Join(options, "/")), def)
		if err != nil {
			return "", err
		}
		if slices.Contains(options, answer) {
			return answer, nil
		}
		w.printf("Please choose one of: %s\n", strings.Join(options, ", "))
	}
	return "", errWizardAborted
}

// wizardDeps are the steps that need the network or a real login; tests
// replace them.
type wizardDeps struct {
	checkServer func(ctx context.Context, serverAddress string) error
	testLogin   func(ctx context.Context) error
	save        func(path string) error
	configPath  func() (string, error)
}

// keyInfo describes an armored private key without printing key material.
func keyInfo(armored string) (string, error) {
	key, err := crypto.NewKeyFromArmored(armored)
	if err != nil {
		return "", fmt.Errorf("not an OpenPGP key: %w", err)
	}
	if !key.IsPrivate() {
		return "", errors.New("this is a public key, a private key is needed")
	}
	info := strings.ToUpper(key.GetFingerprint())
	if _, id := key.GetEntity().PrimaryIdentity(time.Now(), nil); id != nil {
		info += " " + id.Name
	}
	return info, nil
}

func expandHome(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
	}
	return path, nil
}

func validateServerAddress(s string) (string, error) {
	s = strings.TrimRight(s, "/")
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", errors.New("expected a URL like https://passbolt.example.com")
	}
	return s, nil
}

func wizardServer(ctx context.Context, w *wizardIO, deps wizardDeps) error {
	server := ""
	for attempt := 0; server == ""; attempt++ {
		if attempt == wizardMaxAttempts {
			return errWizardAborted
		}
		answer, err := w.ask("Passbolt server address", viper.GetString("serverAddress"))
		if err != nil {
			return err
		}
		s, err := validateServerAddress(answer)
		if err != nil {
			w.printf("%v\n", err)
			continue
		}
		if strings.HasPrefix(s, "http://") {
			ok, err := w.confirm("This address is not encrypted (http). Use it anyway?", false)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
		}
		server = s
	}
	viper.Set("serverAddress", server)

	skip, err := w.confirm("Allow self-signed TLS certificates?", viper.GetBool("tlsSkipVerify"))
	if err != nil {
		return err
	}
	viper.Set("tlsSkipVerify", skip)

	if err := deps.checkServer(ctx, server); err != nil {
		w.printf("Server check failed: %v\n", err)
		ok, err := w.confirm("Continue anyway?", false)
		if err != nil {
			return err
		}
		if !ok {
			return errWizardAborted
		}
	} else {
		w.printf("Server reachable.\n")
	}
	return nil
}

func wizardKey(w *wizardIO) error {
	if current := viper.GetString("userPrivateKey"); current != "" {
		if info, err := keyInfo(current); err == nil {
			w.printf("Current private key: %s\n", info)
			keep, err := w.confirm("Keep this key?", true)
			if err != nil {
				return err
			}
			if keep {
				return nil
			}
		} else {
			w.printf("The configured private key is not usable: %v\n", err)
		}
	}

	for range wizardMaxAttempts {
		answer, err := w.ask("Path to your Passbolt private key file (.asc)", "")
		if err != nil {
			return err
		}
		if answer == "" {
			continue
		}
		path, err := expandHome(answer)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			w.printf("Cannot read the file: %v\n", err)
			continue
		}
		info, err := keyInfo(string(content))
		if err != nil {
			w.printf("%v\n", err)
			continue
		}
		viper.Set("userPrivateKey", string(content))
		w.printf("Private key: %s\n", info)
		w.printf("The key is copied into the config; you can delete %s afterwards.\n", path)
		return nil
	}
	return errWizardAborted
}

func wizardPassword(w *wizardIO) error {
	def := util.PasswordStoreKeyring
	if viper.InConfig("passwordstore") && viper.GetString("passwordStore") == "none" {
		def = "none"
	}
	store, err := w.choose("Store the key password in the OS keyring", []string{util.PasswordStoreKeyring, "none"}, def)
	if err != nil {
		return err
	}
	viper.Set("passwordStore", store)

	if viper.GetString("userPassword") != "" {
		w.printf("The config contains the key password in plain text.\n")
		remove, err := w.confirm("Remove it from the config?", true)
		if err != nil {
			return err
		}
		if remove {
			viper.Set("userPassword", "")
		}
	}
	return nil
}

func wizardMFA(w *wizardIO) error {
	options := []string{"interactive-totp", "none"}
	current := viper.GetString("mfaMode")
	if current == "noninteractive-totp" {
		options = append(options, current)
	}
	def := current
	if !slices.Contains(options, def) {
		def = "interactive-totp"
	}
	mode, err := w.choose("MFA mode", options, def)
	if err != nil {
		return err
	}
	viper.Set("mfaMode", mode)

	if mode == "interactive-totp" {
		remember, err := w.confirm("Remember MFA on this device for 30 days (if the server allows)?", viper.GetBool("mfaRemember"))
		if err != nil {
			return err
		}
		viper.Set("mfaRemember", remember)
	}
	return nil
}

func wizardSummary(w *wizardIO) {
	key, err := keyInfo(viper.GetString("userPrivateKey"))
	if err != nil {
		key = "(invalid)"
	}
	w.printf("\nSummary\n")
	w.printf("  Server:            %s\n", viper.GetString("serverAddress"))
	w.printf("  Self-signed TLS:   %v\n", viper.GetBool("tlsSkipVerify"))
	w.printf("  Private key:       %s\n", key)
	w.printf("  Password storage:  %s\n", viper.GetString("passwordStore"))
	w.printf("  MFA mode:          %s\n", viper.GetString("mfaMode"))
	if viper.GetString("mfaMode") == "interactive-totp" {
		w.printf("  MFA remember:      %v\n", viper.GetBool("mfaRemember"))
	}
	w.printf("\n")
}

// runWizard asks every step, tests the login if wanted and saves the config.
func runWizard(ctx context.Context, w *wizardIO, deps wizardDeps) error {
	w.printf("Passbolt CLI setup. Press Enter to keep the value in [brackets].\n\n")

	steps := []func() error{
		func() error { return wizardServer(ctx, w, deps) },
		func() error { return wizardKey(w) },
		func() error { return wizardPassword(w) },
		func() error { return wizardMFA(w) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	wizardSummary(w)

	test, err := w.confirm("Test the login now?", true)
	if err != nil {
		return err
	}
	if test {
		if err := deps.testLogin(ctx); err != nil {
			w.printf("Login failed: %v\n", err)
			ok, err := w.confirm("Save anyway?", false)
			if err != nil {
				return err
			}
			if !ok {
				return errWizardAborted
			}
		} else {
			w.printf("Login OK.\n")
		}
	}

	path, err := deps.configPath()
	if err != nil {
		return err
	}
	save, err := w.confirm(fmt.Sprintf("Save to %s?", path), true)
	if err != nil {
		return err
	}
	if !save {
		return errWizardAborted
	}
	if err := deps.save(path); err != nil {
		return fmt.Errorf("writing Config: %w", err)
	}
	w.printf("Saved.\n")

	if viper.GetString("serverVerifyToken") == "" {
		w.printf("Tip: run 'passbolt verify' once to enable server verification.\n")
	}
	return nil
}

// wizardConfigPath is the file in use, or the default config file.
func wizardConfigPath() (string, error) {
	if used := viper.ConfigFileUsed(); used != "" {
		return used, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "go-passbolt-cli", "go-passbolt-cli.toml"), nil
}

func wizardSave(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	viper.SetConfigPermissions(0o600)
	if err := viper.WriteConfigAs(path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func wizardCheckServer(ctx context.Context, serverAddress string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	httpClient, err := util.GetHTTPClient()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverAddress+"/healthcheck/status.json", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP %d", req.URL, res.StatusCode)
	}
	return nil
}

func wizardTestLogin(_ context.Context) error {
	ctx, cancel := util.GetContext()
	defer cancel()
	client, err := util.GetClient(ctx)
	if err != nil {
		return err
	}
	util.SaveSessionKeysAndLogout(ctx, client)
	return nil
}

var wizardCmd = &cobra.Command{
	Use:   "wizard",
	Short: "Set up the configuration step by step",
	Long: `Walks through the configuration: server address, TLS, private key,
password storage and MFA. Existing settings are offered as defaults, so a
partial configuration is completed. Optionally tests the login, then saves
the config file (mode 0600). No password or TOTP seed is written to it.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		w := &wizardIO{in: bufio.NewReader(os.Stdin), out: cmd.OutOrStdout()}
		deps := wizardDeps{
			checkServer: wizardCheckServer,
			testLogin:   wizardTestLogin,
			save:        wizardSave,
			configPath:  wizardConfigPath,
		}
		cmd.SilenceUsage = true
		return runWizard(context.Background(), w, deps)
	},
}

func init() {
	rootCmd.AddCommand(wizardCmd)
}
