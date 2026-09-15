package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/30nap/goroker/internal/application"
)

func newLoginCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Sign in to the brokerage and store the session in the browser profile",
		Long: `Launches Chromium with Goroker's persistent profile and opens the brokerage.

If the restored session is still valid, nothing else happens. Otherwise the
login page is opened and, when credentials are configured locally (OS keyring
or GOROKER_USERNAME/GOROKER_PASSWORD), the form is filled in.

If the site presents a CAPTCHA, an OTP, an SMS code, a device confirmation or
any other security challenge, Goroker stops and waits for you to complete it in
the browser window. It never tries to solve or bypass such a challenge.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBrowser(cmd, func(ctx context.Context, app *App) error {
				svc := application.NewLoginService(app.Adapter, app.Log)
				if err := svc.Login(ctx); err != nil {
					return err
				}
				fmt.Println("Authenticated. The session is stored in the browser profile at")
				fmt.Printf("  %s\n", app.Cfg.Browser.ProfileDir)
				return nil
			})
		},
	}
}
