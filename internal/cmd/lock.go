package cmd

import (
	"fmt"
	"os"

	"github.com/passbolt/go-passbolt-cli/internal/util"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// lockCmd removes the locally cached credentials for the configured server
// and user key.
var lockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Forget the stored password and the remembered MFA cookie",
	Long: `Removes the private key password from the OS keyring and the remembered
MFA cookie from the user cache directory, for the configured server and user
key (--all: for every server and key). The next command asks for the
password and the TOTP code again.

This only deletes the local copies. A remembered MFA token stays valid on the
server until it expires (30 days); if the cookie may have been copied, reset
MFA for the user in Passbolt to revoke it.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if all, _ := cmd.Flags().GetBool("all"); all {
			if err := util.ForgetAll(); err != nil {
				return err
			}
			fmt.Println("All stored passwords and MFA cookies of this CLI removed")
			return nil
		}
		serverAddress := viper.GetString("serverAddress")
		if serverAddress == "" {
			return fmt.Errorf("serverAddress is not defined")
		}
		userPrivateKey := viper.GetString("userPrivateKey")
		if userPrivateKey == "" {
			return fmt.Errorf("userPrivateKey is not defined")
		}

		removed, err := util.ForgetPassword(serverAddress, userPrivateKey)
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "Warning: keyring: %v\n", err)
		case removed:
			fmt.Println("Password removed from the keyring")
		default:
			fmt.Println("No password stored in the keyring")
		}

		mfaCache, err := util.NewMFACookieCache(serverAddress, userPrivateKey)
		if err != nil {
			return err
		}
		if err := mfaCache.Clear(); err != nil {
			return fmt.Errorf("removing MFA cookie: %w", err)
		}
		fmt.Println("MFA cookie removed")
		return nil
	},
}

func init() {
	lockCmd.Flags().Bool("all", false, "remove the stored passwords and MFA cookies of every server and key")
	rootCmd.AddCommand(lockCmd)
}
