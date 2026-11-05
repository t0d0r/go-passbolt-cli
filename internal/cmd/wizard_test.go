package cmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/spf13/viper"
)

// wizardKeyFile writes an armored, passphrase-locked private key to a temp
// file and returns its path and content.
func wizardKeyFile(t *testing.T) (string, string) {
	t.Helper()
	pgp := crypto.PGP()
	key, err := pgp.KeyGeneration().AddUserId("Wizard Test", "wizard@example.com").New().GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	locked, err := pgp.LockKey(key, []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	armored, err := locked.Armor()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "private.asc")
	if err := os.WriteFile(path, []byte(armored), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, armored
}

type wizardRun struct {
	out       bytes.Buffer
	saved     string
	loginRuns int
	checkErr  error
	loginErr  error
}

func (r *wizardRun) run(input string) error {
	w := &wizardIO{in: bufio.NewReader(strings.NewReader(input)), out: &r.out}
	deps := wizardDeps{
		checkServer: func(context.Context, string) error { return r.checkErr },
		testLogin: func(context.Context) error {
			r.loginRuns++
			return r.loginErr
		},
		save:       func(path string) error { r.saved = path; return nil },
		configPath: func() (string, error) { return "/tmp/test-config.toml", nil },
	}
	return runWizard(context.Background(), w, deps)
}

func lines(answers ...string) string { return strings.Join(answers, "\n") + "\n" }

func resetWizardViper(t *testing.T) {
	t.Helper()
	viper.Reset()
	viper.SetDefault("mfaMode", "interactive-totp")
	viper.SetDefault("mfaRemember", false)
	viper.SetDefault("passwordStore", "none")
	t.Cleanup(viper.Reset)
}

func TestWizard_EmptyConfig(t *testing.T) {
	resetWizardViper(t)
	keyPath, armored := wizardKeyFile(t)
	r := &wizardRun{}

	err := r.run(lines(
		"https://pb.example.com/", // server, trailing slash removed
		"",                        // self-signed: default no
		keyPath,                   // key file
		"",                        // password store: default keyring
		"",                        // MFA mode: default interactive-totp
		"",                        // remember: default yes
		"n",                       // test login: no
		"",                        // save: default yes
	))
	if err != nil {
		t.Fatalf("wizard: %v\n%s", err, r.out.String())
	}

	checks := map[string]string{
		"serverAddress":  "https://pb.example.com",
		"userPrivateKey": armored,
		"passwordStore":  "none",
		"mfaMode":        "interactive-totp",
	}
	for k, want := range checks {
		if got := viper.GetString(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if viper.GetBool("tlsSkipVerify") || viper.GetBool("mfaRemember") {
		t.Errorf("tlsSkipVerify=%v mfaRemember=%v, want false/false (opt-in)", viper.GetBool("tlsSkipVerify"), viper.GetBool("mfaRemember"))
	}
	if r.saved == "" || r.loginRuns != 0 {
		t.Errorf("saved=%q loginRuns=%d, want saved and no login", r.saved, r.loginRuns)
	}
	if strings.Contains(r.out.String(), "PRIVATE KEY") {
		t.Error("output contains key material")
	}
}

func TestWizard_PartialConfigKeepsValues(t *testing.T) {
	resetWizardViper(t)
	_, armored := wizardKeyFile(t)
	viper.Set("serverAddress", "https://existing.example.com")
	viper.Set("userPrivateKey", armored)
	viper.Set("userPassword", "plain")
	r := &wizardRun{}

	err := r.run(lines(
		"",  // keep server
		"",  // self-signed: no
		"",  // keep key
		"",  // keyring
		"",  // remove plain-text password: yes
		"",  // interactive-totp
		"n", // remember: no
		"",  // test login: yes
		"",  // save
	))
	if err != nil {
		t.Fatalf("wizard: %v\n%s", err, r.out.String())
	}
	if got := viper.GetString("serverAddress"); got != "https://existing.example.com" {
		t.Errorf("serverAddress = %q", got)
	}
	if viper.GetString("userPassword") != "" {
		t.Error("plain-text password not removed")
	}
	if viper.GetBool("mfaRemember") {
		t.Error("mfaRemember should be false")
	}
	if r.loginRuns != 1 || r.saved == "" {
		t.Errorf("loginRuns=%d saved=%q", r.loginRuns, r.saved)
	}
	if strings.Contains(r.out.String(), "plain") && !strings.Contains(r.out.String(), "plain text") {
		t.Error("output may contain the password")
	}
}

func TestWizard_InvalidServerAborts(t *testing.T) {
	resetWizardViper(t)
	r := &wizardRun{}
	err := r.run(lines("nonsense", "ftp://x", "https://"))
	if !errors.Is(err, errWizardAborted) {
		t.Fatalf("err = %v, want errWizardAborted", err)
	}
	if r.saved != "" {
		t.Error("config saved after abort")
	}
}

func TestWizard_HTTPNeedsConfirmation(t *testing.T) {
	resetWizardViper(t)
	keyPath, _ := wizardKeyFile(t)
	r := &wizardRun{}
	err := r.run(lines(
		"http://pb.local", "n", // refuse http
		"https://pb.local", "", // then https
		keyPath, "", "", "", "n", "",
	))
	if err != nil {
		t.Fatalf("wizard: %v\n%s", err, r.out.String())
	}
	if got := viper.GetString("serverAddress"); got != "https://pb.local" {
		t.Errorf("serverAddress = %q", got)
	}
}

func TestWizard_ServerCheckFailureStops(t *testing.T) {
	resetWizardViper(t)
	r := &wizardRun{checkErr: errors.New("connection refused")}
	err := r.run(lines("https://pb.example.com", "", "")) // continue anyway: default no
	if !errors.Is(err, errWizardAborted) {
		t.Fatalf("err = %v, want errWizardAborted", err)
	}
}

func TestWizard_LoginFailureNotSaved(t *testing.T) {
	resetWizardViper(t)
	keyPath, _ := wizardKeyFile(t)
	r := &wizardRun{loginErr: errors.New("unlock failed")}
	err := r.run(lines("https://pb.example.com", "", keyPath, "", "", "", "", "")) // test yes, save anyway: no
	if !errors.Is(err, errWizardAborted) {
		t.Fatalf("err = %v, want errWizardAborted", err)
	}
	if r.saved != "" {
		t.Error("config saved after failed login")
	}
}

func TestWizard_RejectsPublicKeyAndBadPath(t *testing.T) {
	resetWizardViper(t)
	keyPath, armored := wizardKeyFile(t)
	key, err := crypto.NewKeyFromArmored(armored)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := key.GetArmoredPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	pubPath := filepath.Join(t.TempDir(), "public.asc")
	if err := os.WriteFile(pubPath, []byte(pub), 0o600); err != nil {
		t.Fatal(err)
	}

	r := &wizardRun{}
	err = r.run(lines("https://pb.example.com", "",
		"/does/not/exist", pubPath, keyPath, // two bad answers, then good
		"", "", "", "n", ""))
	if err != nil {
		t.Fatalf("wizard: %v\n%s", err, r.out.String())
	}
	if !strings.Contains(r.out.String(), "public key") {
		t.Error("public key was not rejected with a message")
	}
}

func TestWizard_EOFIsError(t *testing.T) {
	resetWizardViper(t)
	r := &wizardRun{}
	if err := r.run(""); err == nil {
		t.Fatal("expected error on empty input")
	}
}

func TestWizardSave_WritesFileWith0600(t *testing.T) {
	resetWizardViper(t)
	viper.Set("serverAddress", "https://pb.example.com")
	viper.Set("passwordStore", "keyring")
	path := filepath.Join(t.TempDir(), "sub", "go-passbolt-cli.toml")

	if err := wizardSave(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}

	viper.Reset()
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	if got := viper.GetString("serverAddress"); got != "https://pb.example.com" {
		t.Errorf("serverAddress = %q after reload", got)
	}
	if got := viper.GetString("passwordStore"); got != "keyring" {
		t.Errorf("passwordStore = %q after reload", got)
	}
}

func TestWizard_OptInChoices(t *testing.T) {
	resetWizardViper(t)
	keyPath, _ := wizardKeyFile(t)
	r := &wizardRun{}
	err := r.run(lines("https://pb.example.com", "", keyPath, "keyring", "", "y", "n", ""))
	if err != nil {
		t.Fatalf("wizard: %v\n%s", err, r.out.String())
	}
	if viper.GetString("passwordStore") != "keyring" || !viper.GetBool("mfaRemember") {
		t.Error("explicit opt-in not applied")
	}
	for _, want := range []string{"Any program running as you", "without the second factor"} {
		if !strings.Contains(r.out.String(), want) {
			t.Errorf("warning %q not shown", want)
		}
	}
}

func TestWizard_NoRememberOverHTTP(t *testing.T) {
	resetWizardViper(t)
	viper.Set("mfaRemember", true)
	keyPath, _ := wizardKeyFile(t)
	r := &wizardRun{}
	// http needs confirmation; then no remember question is asked
	err := r.run(lines("http://pb.local", "y", "", keyPath, "", "", "n", ""))
	if err != nil {
		t.Fatalf("wizard: %v\n%s", err, r.out.String())
	}
	if viper.GetBool("mfaRemember") || !strings.Contains(r.out.String(), "only offered for https") {
		t.Error("remember-me allowed over http")
	}
}

func TestWizardSettings_Allowlist(t *testing.T) {
	resetWizardViper(t)
	viper.Set("serverAddress", "https://pb.example.com")
	viper.Set("mfaMode", "interactive-totp")
	viper.Set("mfaTotpToken", "SEED-FROM-ENV") // e.g. MFATOTPTOKEN in the environment
	viper.Set("somethingElse", "from a flag")
	got := wizardSettings()
	if _, ok := got["mfaTotpToken"]; ok {
		t.Error("TOTP seed written in interactive mode")
	}
	if _, ok := got["somethingElse"]; ok {
		t.Error("non-allowlisted key written")
	}
	if _, ok := got["userPassword"]; ok {
		t.Error("empty password written")
	}
	if got["serverAddress"] != "https://pb.example.com" {
		t.Error("server address missing")
	}

	viper.Set("mfaMode", "noninteractive-totp")
	if wizardSettings()["mfaTotpToken"] != "SEED-FROM-ENV" {
		t.Error("TOTP seed dropped although noninteractive mode needs it")
	}

	viper.Set("userPassword", "kept-by-user")
	if wizardSettings()["userPassword"] != "kept-by-user" {
		t.Error("password the user chose to keep was dropped")
	}
}

func TestWizardTestLogin_StoresNothing(t *testing.T) {
	resetWizardViper(t)
	viper.Set("passwordStore", "keyring")
	viper.Set("mfaRemember", true)
	viper.Set("serverAddress", "") // makes GetClient fail before any network call
	if err := wizardTestLogin(context.Background()); err == nil {
		t.Fatal("expected an error without server address")
	}
	if viper.GetString("passwordStore") != "keyring" || !viper.GetBool("mfaRemember") {
		t.Error("settings not restored after the test login")
	}
}
