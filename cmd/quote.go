package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/30nap/goroker/internal/application"
)

func newQuoteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "quote SYMBOL",
		Short: "Read a live quote for one symbol",
		Long: `Reads the live quote for SYMBOL from the brokerage UI.

The symbol must match exactly. If several instruments match, Goroker aborts
rather than guessing which one you meant.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBrowser(cmd, func(ctx context.Context, app *App) error {
				if err := application.RequireAuthenticated(ctx, app.Adapter); err != nil {
					return err
				}
				sym, quote, err := app.quoteService().ResolveAndQuote(ctx, args[0])
				if err != nil {
					return err
				}
				fmt.Printf("Symbol: %s\n", sym.Name)
				fmt.Printf("Last: %d\n", quote.LastPrice)
				fmt.Printf("Best Ask: %d\n", quote.BestAsk)
				fmt.Printf("Best Bid: %d\n", quote.BestBid)
				fmt.Printf("Observed: %s\n", quote.Timestamp.Format("15:04:05"))
				return nil
			})
		},
	}
}
