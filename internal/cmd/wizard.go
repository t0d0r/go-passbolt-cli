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
	_, _ = io.WriteString(w.out, fmt.Sprintf(format, a...))
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
	def := "none"
	if viper.InConfig("passwordstore") && viper.GetString("passwordStore") == util.PasswordStoreKeyring {
		def = util.PasswordStoreKeyring
	}
	w.printf("\nOptional: keep the key password in the OS keyring, so it is not asked on\n" +
		"every run. Any program running as you while the keyring is unlocked may be\n" +
		"able to use it (on macOS only this passbolt binary may read it without a\n" +
		"prompt). With 'none' the password is asked every time.\n")
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

	switch {
	case mode != "interactive-totp":
		viper.Set("mfaRemember", false)
	case !strings.HasPrefix(viper.GetString("serverAddress"), "https://"):
		w.printf("Remembering MFA is only offered for https servers.\n")
		viper.Set("mfaRemember", false)
	default:
		w.printf("\nOptional: remember the TOTP verification on this device for 30 days (only\n" +
			"if the server policy allows it). The cookie is stored in your cache\n" +
			"directory; together with the key and a stored password it allows a login\n" +
			"without the second factor. 'passbolt lock' removes it locally.\n")
		remember, err := w.confirm("Remember MFA on this device?", viper.GetBool("mfaRemember"))
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

// wizardSavedKeys are the settings the wizard writes. Everything else, in
// particular values that came only from flags or environment variables, is
// left out of the file.
var wizardSavedKeys = []string{
	"serverAddress", "userPrivateKey", "passwordStore", "mfaMode", "mfaRemember",
	"mfaRetrys", "mfaDelay", "tlsSkipVerify", "tlsClientCert", "tlsClientPrivateKey",
	"serverVerifyToken", "serverVerifyEncToken",
}

// wizardSettings returns what wizardSave writes: the allowlisted keys, the
// TOTP seed only in noninteractive-totp mode, a plain-text password only if
// the user chose to keep an existing one, and timeout/workers only if they
// were in the config file.
func wizardSettings() map[string]any {
	out := map[string]any{}
	for _, k := range wizardSavedKeys {
		out[k] = viper.Get(k)
	}
	if viper.GetString("mfaMode") == "noninteractive-totp" {
		for _, k := range []string{"mfaTotpToken", "mfaTotpOffset", "totpToken", "totpOffset"} {
			if v := viper.Get(k); v != nil && fmt.Sprint(v) != "" && fmt.Sprint(v) != "0s" {
				out[k] = v
			}
		}
	}
	if pw := viper.GetString("userPassword"); pw != "" {
		out["userPassword"] = pw
	}
	for _, k := range []string{"timeout", "workers"} {
		if viper.InConfig(strings.ToLower(k)) {
			out[k] = viper.Get(k)
		}
	}
	return out
}

func wizardSave(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	v := viper.New()
	v.SetConfigType("toml")
	v.SetConfigPermissions(0o600)
	for k, val := range wizardSettings() {
		v.Set(k, val)
	}
	if err := v.WriteConfigAs(path); err != nil {
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

// wizardTestLogin logs in with the new settings but stores nothing: the
// password is not written to the keyring and the MFA cookie not cached,
// because the user has not agreed to save the configuration yet.
func wizardTestLogin(_ context.Context) error {
	store, remember := viper.GetString("passwordStore"), viper.GetBool("mfaRemember")
	viper.Set("passwordStore", "none")
	viper.Set("mfaRemember", false)
	defer func() {
		viper.Set("passwordStore", store)
		viper.Set("mfaRemember", remember)
	}()
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
partial configuration is completed. Optionally tests the login (without
storing anything), then saves the config file (mode 0600). Storing the
password in the OS keyring and remembering MFA are opt-in. Only the wizard's
settings are written; a TOTP seed only in noninteractive-totp mode and a
plain-text password only if you choose to keep an existing one.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		w := &wizardIO{in: bufio.NewReader(os.Stdin), out: util.Out}
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
