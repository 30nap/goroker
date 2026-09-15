package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/spf13/cobra"

	"github.com/30nap/goroker/internal/domain"
	"github.com/30nap/goroker/internal/inspect"
)

// newInspectCommand builds `goroker inspect`, the broker-discovery helper.
//
// It opens the brokerage in a visible Chromium and lets you save sanitised
// snapshots of whatever is on screen, so the selectors in the adapter can be
// established from the real page instead of being guessed.
//
// It only ever reads: it never clicks, never fills a form and never places an
// order. You drive the site yourself in the browser window.
func newInspectCommand() *cobra.Command {
	var startURL string

	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Open the brokerage and save sanitised page snapshots for selector discovery",
		Long: `Opens the brokerage in a visible Chromium window using your persistent profile
and waits for commands. You navigate the site yourself; Goroker only reads the
page when you ask it to.

Commands:

  snap LABEL [CSS]   save a sanitised snapshot of the page, or of the elements
                     matching CSS, to the debug directory
  text CSS           print the text of the first few elements matching CSS
  count CSS          count the elements matching CSS
  url                print the current page URL
  help               show this list
  quit               close the browser and exit

Snapshots are sanitised before they are written: scripts and inline styles are
dropped, form values are replaced, and account numbers, phone numbers, e-mail
addresses and long opaque tokens are redacted. Sanitising is best effort — read
a snapshot before you share it.

inspect never clicks anything and never submits anything.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBrowser(cmd, func(ctx context.Context, app *App) error {
				url := startURL
				if url == "" {
					url = app.Cfg.Broker.BaseURL
				}
				if url == "" {
					return fmt.Errorf("%w: set broker.base_url in config.yaml or pass --url", domain.ErrAbort)
				}

				page, err := app.Browser.Page(ctx, url)
				if err != nil {
					return err
				}
				page = page.CancelTimeout()

				dir := filepath.Join(app.Cfg.Debug.Dir, "inspect")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					return fmt.Errorf("create %s: %w", dir, err)
				}

				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "Opened %s in the browser window.\n", url)
				fmt.Fprintf(out, "Sign in there yourself, including the SMS code. Goroker will not touch the page.\n")
				fmt.Fprintf(out, "Snapshots are written to %s\n", dir)
				fmt.Fprintf(out, "Type `help` for commands, `quit` to finish.\n\n")

				return runInspectLoop(ctx, cmd, page, dir)
			})
		},
	}

	cmd.Flags().StringVar(&startURL, "url", "", "page to open (default broker.base_url)")
	return cmd
}

func runInspectLoop(ctx context.Context, cmd *cobra.Command, page *rod.Page, dir string) error {
	out := cmd.OutOrStdout()
	lines := make(chan string)
	readErr := make(chan error, 1)

	go func() {
		scanner := bufio.NewScanner(cmd.InOrStdin())
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
		readErr <- scanner.Err()
	}()

	for {
		fmt.Fprint(out, "inspect> ")
		select {
		case <-ctx.Done():
			fmt.Fprintln(out, "\nStopped.")
			return nil
		case err := <-readErr:
			fmt.Fprintln(out)
			return err
		case line := <-lines:
			done, err := runInspectCommand(ctx, out, page, dir, line)
			if err != nil {
				fmt.Fprintf(out, "  error: %v\n", err)
			}
			if done {
				return nil
			}
		}
	}
}

func runInspectCommand(ctx context.Context, out interface{ Write([]byte) (int, error) }, page *rod.Page, dir, line string) (bool, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return false, nil
	}

	switch fields[0] {
	case "quit", "exit":
		return true, nil

	case "help":
		fmt.Fprint(out, "  snap LABEL [CSS]   save a sanitised snapshot\n"+
			"  text CSS           print the text of matching elements\n"+
			"  count CSS          count matching elements\n"+
			"  url                print the current URL\n"+
			"  quit               close the browser and exit\n")
		return false, nil

	case "url":
		info, err := page.Context(ctx).Info()
		if err != nil {
			return false, err
		}
		fmt.Fprintf(out, "  %s\n", info.URL)
		return false, nil

	case "snap":
		if len(fields) < 2 {
			return false, fmt.Errorf("usage: snap LABEL [CSS]")
		}
		selector := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "snap "+fields[1]))
		path, err := snapshot(ctx, page, dir, fields[1], selector)
		if err != nil {
			return false, err
		}
		fmt.Fprintf(out, "  saved %s\n", path)
		return false, nil

	case "text":
		if len(fields) < 2 {
			return false, fmt.Errorf("usage: text CSS")
		}
		selector := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "text"))
		elements, err := page.Context(ctx).Elements(selector)
		if err != nil {
			return false, err
		}
		if len(elements) == 0 {
			fmt.Fprintln(out, "  no match")
			return false, nil
		}
		for i, el := range elements {
			if i >= 10 {
				fmt.Fprintf(out, "  ... and %d more\n", len(elements)-i)
				break
			}
			text, err := el.Text()
			if err != nil {
				return false, err
			}
			fmt.Fprintf(out, "  [%d] %s\n", i, inspect.RedactText(strings.TrimSpace(text)))
		}
		return false, nil

	case "count":
		if len(fields) < 2 {
			return false, fmt.Errorf("usage: count CSS")
		}
		selector := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "count"))
		elements, err := page.Context(ctx).Elements(selector)
		if err != nil {
			return false, err
		}
		fmt.Fprintf(out, "  %d element(s)\n", len(elements))
		return false, nil

	default:
		return false, fmt.Errorf("unknown command %q; type `help`", fields[0])
	}
}

// snapshot writes a sanitised copy of the page, or of the elements matching
// selector, into dir.
func snapshot(ctx context.Context, page *rod.Page, dir, label, selector string) (string, error) {
	var raw string

	if selector == "" {
		html, err := page.Context(ctx).HTML()
		if err != nil {
			return "", err
		}
		raw = html
	} else {
		elements, err := page.Context(ctx).Elements(selector)
		if err != nil {
			return "", err
		}
		if len(elements) == 0 {
			return "", fmt.Errorf("%w: nothing matches %s", domain.ErrSelectorMissing, selector)
		}
		var b strings.Builder
		for _, el := range elements {
			html, err := el.HTML()
			if err != nil {
				return "", err
			}
			b.WriteString(html)
			b.WriteString("\n")
		}
		raw = b.String()
	}

	clean, err := inspect.Sanitize(raw)
	if err != nil {
		return "", err
	}

	info, err := page.Context(ctx).Info()
	if err != nil {
		return "", err
	}

	name := fmt.Sprintf("%s-%s.html", time.Now().Format("2006-01-02T150405"), sanitizeLabel(label))
	path := filepath.Join(dir, name)
	header := fmt.Sprintf("<!-- goroker inspect\n     url: %s\n     selector: %s\n     captured: %s\n-->\n",
		inspect.RedactText(info.URL), selector, time.Now().Format(time.RFC3339))

	if err := os.WriteFile(path, []byte(header+clean+"\n"), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func sanitizeLabel(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "snapshot"
	}
	return string(out)
}
