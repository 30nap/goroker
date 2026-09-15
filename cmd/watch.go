package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/30nap/goroker/internal/application"
	"github.com/30nap/goroker/internal/domain"
)

func newWatchCommand() *cobra.Command {
	var minPrice, maxPrice int64

	cmd := &cobra.Command{
		Use:   "watch SYMBOL",
		Short: "Watch a symbol until its best ask enters a price range",
		Long: `Monitors the live best ask for SYMBOL and reports when it enters the
requested range.

watch never prepares and never submits an order.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if minPrice <= 0 || maxPrice <= 0 {
				return fmt.Errorf("%w: --min-price and --max-price are required", domain.ErrAbort)
			}
			if minPrice > maxPrice {
				return fmt.Errorf("%w: --min-price (%d) is greater than --max-price (%d)", domain.ErrAbort, minPrice, maxPrice)
			}

			return withBrowser(cmd, func(ctx context.Context, app *App) error {
				if err := application.RequireAuthenticated(ctx, app.Adapter); err != nil {
					return err
				}
				sym, err := app.quoteService().Resolve(ctx, args[0])
				if err != nil {
					return err
				}

				watcher := application.NewWatchService(
					app.Adapter,
					app.Cfg.Watch.Interval,
					app.Cfg.Watch.MaxQuoteAge,
					app.Log,
					nil,
				)
				target := application.Target{
					MinPrice: minPrice,
					MaxPrice: maxPrice,
					Source:   domain.PriceSourceBestAsk,
				}

				fmt.Printf("Watching %s for best ask in %s .. %s (every %s). Ctrl+C to stop.\n",
					sym.Name,
					application.Thousands(minPrice),
					application.Thousands(maxPrice),
					watcher.Interval())

				obs, err := watcher.Wait(ctx, sym, target, func(o application.Observation) {
					fmt.Printf("  %s  best ask %s  last %s\n",
						o.Quote.Timestamp.Format("15:04:05"),
						application.Thousands(o.Price),
						application.Thousands(o.Quote.LastPrice))
				})
				if err != nil {
					return err
				}

				fmt.Printf("\nTARGET REACHED: %s best ask %s is within %s .. %s at %s\n",
					sym.Name,
					application.Thousands(obs.Price),
					application.Thousands(minPrice),
					application.Thousands(maxPrice),
					obs.Quote.Timestamp.Format("15:04:05"))
				fmt.Println("No order was prepared or submitted: watch never trades.")
				return nil
			})
		},
	}

	cmd.Flags().Int64Var(&minPrice, "min-price", 0, "lower bound of the target best-ask range")
	cmd.Flags().Int64Var(&maxPrice, "max-price", 0, "upper bound of the target best-ask range")
	return cmd
}
