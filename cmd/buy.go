package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/30nap/goroker/internal/application"
	"github.com/30nap/goroker/internal/domain"
)

// submissionEnabled is the build-level switch for real order submission.
//
// It is intentionally a constant and not a flag or a configuration key: it is
// how this project's phases are brought up one at a time. It can only make
// Goroker safer — even with it on, an order is submitted solely after the
// confirmation word is typed and final validation passes.
//
// Phase 6 (manual-confirmation submission) turns this on, once the earlier
// phases are reliable against the real brokerage UI.
const submissionEnabled = false

func newBuyCommand() *cobra.Command {
	var (
		minPrice int64
		maxPrice int64
		price    int64
		quantity int64
		dryRun   bool
	)

	cmd := &cobra.Command{
		Use:   "buy SYMBOL",
		Short: "Prepare a BUY order and submit it only after explicit confirmation",
		Long: `Waits for the target price, fills the BUY order form, reads it back from the
page, validates it, and asks you to type BUY to submit.

Two price modes:

  --min-price X --max-price Y   wait for the best ask to enter [X, Y] and use
                                the observed best ask as the order price
  --price P                     wait for the best ask to reach exactly P and
                                use P, unchanged

Nothing is ever submitted without you typing the confirmation word, and the
whole order is re-validated between your confirmation and the click. There is
no flag that skips the confirmation.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := domain.BuyOrderRequest{
				Symbol:   args[0],
				MinPrice: minPrice,
				MaxPrice: maxPrice,
				Price:    price,
				Quantity: quantity,
			}
			if err := req.Validate(); err != nil {
				return err
			}

			return withBrowser(cmd, func(ctx context.Context, app *App) error {
				opts := application.OrderOptions{
					WatchInterval:       app.Cfg.Watch.Interval,
					MaxQuoteAge:         app.Cfg.Watch.MaxQuoteAge,
					ConfirmationTimeout: app.Cfg.Order.ConfirmationTimeout,
					DryRun:              dryRun,
					SubmissionEnabled:   submissionEnabled && !dryRun,
				}
				confirmer := application.NewTerminalConfirmer(cmd.InOrStdin(), cmd.OutOrStdout())
				svc := application.NewOrderService(app.Adapter, confirmer, opts, app.Log, nil)

				if dryRun {
					fmt.Println("DRY RUN: the order form will be filled and validated, and nothing will be submitted.")
				} else if !submissionEnabled {
					fmt.Fprintln(os.Stderr,
						"NOTE: order submission is not enabled in this build; the run stops after validation.")
				}

				min, max := req.Bounds()
				fmt.Printf("Waiting for %s best ask in %s .. %s, quantity %s. Ctrl+C to stop.\n",
					req.Symbol,
					application.Thousands(min),
					application.Thousands(max),
					application.Thousands(req.Quantity))

				outcome, err := svc.Buy(ctx, req, func(o application.Observation) {
					fmt.Printf("  %s  best ask %s\n",
						o.Quote.Timestamp.Format("15:04:05"),
						application.Thousands(o.Price))
				})
				if err != nil {
					return err
				}

				switch {
				case outcome.Submitted:
					fmt.Printf("\nOrder %s by the broker.\n", outcome.Result.Status)
					if outcome.Result.BrokerOrderID != "" {
						fmt.Printf("Broker order ID: %s\n", outcome.Result.BrokerOrderID)
					}
					if outcome.Result.Message != "" {
						fmt.Printf("Broker message: %s\n", outcome.Result.Message)
					}
				case dryRun:
					fmt.Printf("\nDRY RUN complete. Validated order: %s %s x %s at %s\n",
						outcome.Prepared.Side,
						outcome.Prepared.Symbol,
						application.Thousands(outcome.Prepared.Quantity),
						application.Thousands(outcome.Prepared.Price))
					fmt.Println("Nothing was submitted.")
				default:
					fmt.Printf("\nValidated order: %s %s x %s at %s\n",
						outcome.Prepared.Side,
						outcome.Prepared.Symbol,
						application.Thousands(outcome.Prepared.Quantity),
						application.Thousands(outcome.Prepared.Price))
					fmt.Println("Nothing was submitted.")
				}
				return nil
			})
		},
	}

	cmd.Flags().Int64Var(&minPrice, "min-price", 0, "lower bound of the acceptable best-ask range")
	cmd.Flags().Int64Var(&maxPrice, "max-price", 0, "upper bound of the acceptable best-ask range")
	cmd.Flags().Int64Var(&price, "price", 0, "exact order price; cannot be combined with --min-price/--max-price")
	cmd.Flags().Int64Var(&quantity, "quantity", 0, "number of shares to buy")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "prepare and validate the order without ever submitting it")
	_ = cmd.MarkFlagRequired("quantity")
	return cmd
}
