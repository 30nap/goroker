package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/30nap/goroker/internal/application"
	"github.com/30nap/goroker/internal/browser"
)

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication, browser session and market status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBrowser(cmd, func(ctx context.Context, app *App) error {
				session, err := browser.InspectSession(app.Cfg.Browser.ProfileDir)
				if err != nil {
					return err
				}
				svc := application.NewLoginService(app.Adapter, app.Log)
				status := svc.Status(ctx, session)
				fmt.Print(renderStatus(status, app.Cfg.Broker.DisplayName))
				return nil
			})
		},
	}
}

func renderStatus(s application.Status, displayName string) string {
	var b strings.Builder
	b.WriteString("\nGoroker Status\n\n")
	fmt.Fprintf(&b, "Authentication: %s\n", s.Auth)
	if s.AuthDetail != "" {
		fmt.Fprintf(&b, "  reason: %s\n", s.AuthDetail)
	}

	sessionState := "not available"
	if s.Session.Available {
		sessionState = "available"
	}
	fmt.Fprintf(&b, "Browser session: %s\n", sessionState)
	if s.Session.Available && !s.Session.LastUsed.IsZero() {
		fmt.Fprintf(&b, "  last used: %s\n", s.Session.LastUsed.Format("2006-01-02 15:04:05"))
	}

	fmt.Fprintf(&b, "Market: %s\n", strings.ToLower(string(s.Market.State)))
	if s.MarketErr != "" {
		fmt.Fprintf(&b, "  reason: %s\n", s.MarketErr)
	}
	if displayName == "" {
		displayName = s.BrokerName
	}
	fmt.Fprintf(&b, "Broker: %s\n", displayName)

	if len(s.MissingSelectors) > 0 {
		fmt.Fprintf(&b, "\nBroker discovery incomplete: %d selector(s) not configured.\n", len(s.MissingSelectors))
		fmt.Fprintf(&b, "  %s\n", strings.Join(s.MissingSelectors, ", "))
		b.WriteString("  Trading commands fail closed until these are established.\n")
	}
	return b.String()
}
