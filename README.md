# Goroker

Goroker is a local command-line assistant that drives **Mofid Easy Trader**
(`https://d.easytrader.ir/`) through Chromium, because the brokerage has no
official API. It can sign in, restore a session, read live quotes, watch for a target
price, prepare a BUY order, validate it against the page, and submit it **only
after you type an explicit confirmation**.

> **Read this before using it**
>
> * Goroker interacts with a **real brokerage account** and can place **real
>   orders** with real money.
> * Broker UI changes can break browser automation at any time. Goroker fails
>   closed when that happens, but you are responsible for what it does.
> * **Every real order requires an explicit manual confirmation.** There is no
>   flag, no configuration key and no code path that skips it.
> * Never commit credentials or the browser profile. Both are git-ignored.

## Current status

| Phase | Scope | State |
| --- | --- | --- |
| 1 | Module, CLI, config, logging, domain model, broker/browser abstractions, state machine, tests | **done** |
| 2 | Broker discovery: inspect the real site, write `docs/broker-ui-analysis.md`, fill in selectors | **in progress** — `goroker inspect` is ready, selectors pending |
| 3 | `login` / `status` against the real site | blocked on Phase 2 |
| 4 | `quote` / `watch` against the real site | blocked on Phase 2 |
| 5 | `buy --dry-run` against the real order form | blocked on Phase 2 |
| 6 | Manual-confirmation submission | blocked on Phase 5 |

Until Phase 2 fills in the selectors, every trading command aborts with
`selector missing`, which is the intended fail-closed behaviour: Goroker never
guesses which element on the page is the price input.

Real submission is additionally gated by `submissionEnabled` in
[`cmd/buy.go`](cmd/buy.go), a compiled-in constant that Phase 6 turns on. It is
deliberately not a flag: it can only ever make Goroker safer, and it never
replaces your confirmation.

## Installation

Requires Go 1.25+ and a Chromium (or Chrome) installation.

```bash
git clone https://github.com/30nap/goroker.git
cd goroker
go build -o goroker .
./goroker --help
```

Rod downloads a Chromium build on first use if it cannot find one. To use an
existing browser, set `browser.bin_path` in the configuration.

## Configuration

Goroker keeps everything under `~/.goroker/`:

```text
~/.goroker/config.yaml        configuration
~/.goroker/selectors.yaml     optional selector overrides (see "Troubleshooting")
~/.goroker/browser-profile/   persistent Chromium profile (your session)
~/.goroker/debug/             failure screenshots
```

Copy [`config.example.yaml`](config.example.yaml) to `~/.goroker/config.yaml`
and edit it. Every value can be overridden by an environment variable of the
form `GOROKER_<SECTION>_<KEY>`:

```bash
GOROKER_WATCH_INTERVAL=5s goroker watch فولاد --min-price 3250 --max-price 3350
```

The configuration file must never contain a username, password, OTP, cookie or
token.

## Login

```bash
goroker login
```

This launches Chromium with the persistent profile, opens the brokerage, and:

1. reuses the restored session if it is still valid;
2. otherwise opens the login page and fills in your username and password **if**
   they are configured locally;
3. **pauses** when the SMS one-time code (Easy Trader always asks for one on a
   fresh login), a device confirmation or any other security challenge appears,
   so you can complete it yourself in the browser window;
4. continues once the site reports a signed-in session.

Goroker never tries to solve or bypass a security challenge, and never works
around rate limits.

### Credentials

Credentials are read only at the moment the login form is filled, and are never
logged, printed, or written to the configuration file.

* **OS keyring (preferred):** stored under service `goroker`, account =
  the broker name from your configuration (`mofid`).
* **Environment variables (development):** `GOROKER_USERNAME` and
  `GOROKER_PASSWORD`, optionally through a git-ignored `.env`
  (see [`.env.example`](.env.example)).
* **Nothing configured:** log in by hand in the browser window; Goroker waits.

Never pass credentials on the command line — command lines are visible to other
processes and land in your shell history.

## Persistent sessions

The Chromium profile in `~/.goroker/browser-profile/` is the session store.
Goroker never reads cookies or tokens out of it; it only launches Chromium with
it, so the brokerage sees the same browser it saw last time. Deleting the
directory signs you out.

`goroker status` reports whether a profile exists, but authentication itself is
always re-checked against the live site.

```bash
goroker status
```

```text
Goroker Status

Authentication: authenticated
Browser session: available
Market: open
Broker: <broker name>
```

Market is `open`, `closed` or `unknown`. **`unknown` is never treated as open.**

## Quote

```bash
goroker quote فولاد
```

```text
Symbol: فولاد
Last: 3310
Best Ask: 3310
Best Bid: 3305
Observed: 10:42:31
```

Symbol matching is exact after Persian/Arabic letter normalisation (`ي`→`ی`,
`ك`→`ک`, zero-width characters dropped). If several instruments match, Goroker
aborts instead of guessing.

## Watch

```bash
goroker watch فولاد --min-price 3250 --max-price 3350
```

Monitors the live **best ask** and reports when it enters the range. `watch`
never prepares and never submits an order. Ctrl+C stops it cleanly.

## Buy

```bash
goroker buy فولاد --min-price 3250 --max-price 3350 --quantity 10000
```

The flow is:

```text
RESTORE_BROWSER_SESSION → CHECK_AUTHENTICATION → CHECK_MARKET_STATUS
→ RESOLVE_SYMBOL → READ_LIVE_QUOTE → WATCH_PRICE → TARGET_RANGE_REACHED
→ PREPARE_BUY_ORDER → READ_ORDER_BACK_FROM_DOM → VALIDATE_ORDER
→ SHOW_CONFIRMATION → WAIT_FOR_EXPLICIT_USER_CONFIRMATION
→ FINAL_VALIDATION → SUBMIT_ORDER → VERIFY_BROKER_RESPONSE
```

Anything unexpected at any step means `ABORT`.

The order price is the **best ask** observed inside your range. The price source
is stated explicitly in the confirmation screen and in the logs; Goroker never
silently switches between price definitions.

You are shown:

```text
================================
GOROKER ORDER CONFIRMATION
================================

Symbol: فولاد
Side: BUY
Quantity: 10,000
Price: 3,310
Price source: BEST_ASK
Estimated Value: 33,100,000
Current Best Ask: 3,310
Allowed range: 3,250 .. 3,350
Quote age: 412ms

This submits a real order to your brokerage account.
Confirmation expires in 15s.

Type BUY to submit:
```

Only the exact word `BUY` approves the order. `buy`, `y`, `yes`, an empty line,
or anything else aborts. The prompt expires after
`order.confirmation_timeout` (default 15s).

Immediately after your confirmation and before the click, Goroker re-reads and
re-checks: market is still open, symbol, side, quantity and price still match
the confirmed order, the price is still inside your range, and the quote is
still fresh. Any failure aborts without submitting.

### Exact-price buy

```bash
goroker buy فولاد --price 3310 --quantity 10000
```

Waits for the best ask to reach exactly 3,310, fills that price, reads it back
from the page, and asks for confirmation. The requested price is never altered.
`--price` cannot be combined with `--min-price`/`--max-price`.

### Dry run

```bash
goroker buy فولاد --min-price 3250 --max-price 3350 --quantity 10000 --dry-run
```

A dry run does everything up to and including validation, and then stops. It
**cannot** submit: the submission guard refuses while dry-run is set, and the
flow never reaches the confirmation prompt. Use it while developing.

## Broker discovery (`goroker inspect`)

Goroker cannot know which element on the page is the price input, and it never
guesses. `internal/broker/mofid/selectors.go` ships **empty**, and every DOM
access goes through a check that aborts when a selector is unset.

To establish them, run on your own machine, signed in to your own account:

```bash
goroker inspect
```

This opens the brokerage in a visible Chromium with your persistent profile and
then **only reads** the page — it never clicks, never fills a form and never
submits. You drive the site; the terminal takes snapshots on request:

```text
snap LABEL [CSS]   save a sanitised snapshot of the page or of matching elements
text CSS           print the text of the first few matching elements
count CSS          count matching elements
url                print the current URL
quit               close the browser and exit
```

Snapshots land in `~/.goroker/debug/inspect/` and are sanitised first: scripts
and inline styles are dropped, form values are replaced, and account numbers,
phone numbers, e-mail addresses and long opaque tokens are redacted. Class
names, ids, `data-*` and `aria-*` attributes and prices survive, because that is
what selectors are written from. Sanitising is best effort — **read a snapshot
before you share it**.

The step-by-step recipe, and the table of which selector comes from which
snapshot, are in [`docs/broker-ui-analysis.md`](docs/broker-ui-analysis.md).

Once the selectors are known they go into `internal/broker/mofid/selectors.go`,
or into `~/.goroker/selectors.yaml` to fix a UI change without rebuilding:

```yaml
best_ask: "#best-ask"
price_input: "#order-price"
market_open_text:
  - "بازار باز است"
```

## Security

* No credentials in source code, README, tests, examples, config files, command
  lines or git history.
* Credentials live in the OS keyring or in the environment, are held in memory
  only while the login form is filled, and never reach the log: the slog handler
  redacts any attribute whose key looks like a secret (password, OTP, cookie,
  token, authorization, …), at any nesting depth.
* `~/.goroker/` and the browser profile are created with `0700`.
* The browser profile and `.env` are git-ignored.
* Security challenges are always handed to you; Goroker never attempts to solve
  a CAPTCHA, bypass an OTP, defeat anti-bot measures or evade rate limits.
* Failure screenshots are written only on automation failures and never
  deliberately during login, OTP or CAPTCHA screens.

### Fail-closed behaviour

Every one of these aborts: missing selector, ambiguous symbol, unknown symbol,
uncertain authentication, disconnected browser, unknown market state, stale
quote, price outside the requested range, unexpected popup, changed DOM, wrong
quantity, wrong symbol, wrong price, wrong side, unknown order state, expired
confirmation, rejected confirmation, and cancellation (Ctrl+C).

There is no `--yes`, `--force`, `--auto`, `--auto-confirm`,
`--skip-confirmation` or `--non-interactive-submit`, and there is no code path
that submits without an interactive confirmation.

## Troubleshooting

**`ABORT: required UI selector not found: … is not configured`**
Broker discovery has not filled that selector in yet, or the brokerage changed
its UI. Inspect the page in a visible Chromium window, update
`docs/broker-ui-analysis.md`, and set the selector either in
`internal/broker/mofid/selectors.go` or, without rebuilding, in
`~/.goroker/selectors.yaml`.

**`ABORT: authentication state uncertain`**
Goroker could see neither the signed-in marker nor the login form. Run
`goroker login` and watch the browser window.

**`ABORT: market status unknown`**
The market status element did not contain one of the known open/closed texts.
This is intentional: an unknown market is never traded into.

**`ABORT: quote is stale`**
The page stopped updating, or `watch.max_quote_age` is too tight for your
connection. Check the browser window first — a frozen page is a real problem,
not a configuration nuisance.

**Screenshots** of failures land in `~/.goroker/debug/`.

## Architecture

```text
cmd/                     Cobra commands: parse input, print results
internal/domain/         types and rules: orders, quotes, market, state machine
internal/application/    use cases: login, quote, watch, order lifecycle
internal/broker/         brokerage abstraction + registry + submit guard
internal/broker/mofid/   the one place DOM selectors are allowed to exist
internal/inspect/        sanitiser for broker-discovery snapshots
internal/browser/        Rod/Chromium wrapper, persistent profile, screenshots
internal/config/         ~/.goroker/config.yaml + environment overrides
internal/storage/        OS keyring credential access
internal/logging/        slog setup, event names, secret redaction
```

Dependencies point inwards: `cmd` → `application` → `broker`/`domain`. No
brokerage-specific selector appears outside `internal/broker/mofid`, and no
domain or application code knows what a CSS selector is.

Submission safety is enforced three times over:

1. the **state machine** only reaches `SUBMITTING` through
   `AWAITING_CONFIRMATION → CONFIRMED → FINAL_VALIDATION`;
2. the **submit authority** in the order service requires confirmation, a
   passing final validation, a non-dry run and the right state;
3. `broker.Guard` refuses `SubmitPreparedOrder` at the adapter boundary unless
   the authority agrees — and denies everything by default.

## Adding another broker adapter

1. Create `internal/broker/yourbroker/`.
2. Implement `broker.Adapter` (`Login`, `IsAuthenticated`, `MarketStatus`,
   `ResolveSymbol`, `Quote`, `PrepareBuyOrder`, `ReadPreparedOrder`,
   `SubmitPreparedOrder`, `Close`).
3. Keep every selector in that package's `selectors.go`, and route every DOM
   access through a `Require`-style helper so a missing selector aborts.
4. Register the adapter from `init()`:
   `broker.Register("yourbroker", func(d broker.Deps) (broker.Adapter, error) { … })`.
5. Import it for its side effect in `cmd/root.go`, and set `broker.name` in the
   configuration.
6. Document the UI in `docs/` the way `docs/broker-ui-analysis.md` does, and
   never invent a selector.

Return the sentinel errors from `internal/domain` (`ErrSymbolAmbiguous`,
`ErrMarketUnknown`, `ErrSelectorMissing`, …) so the fail-closed behaviour and
the tests apply unchanged.

## Testing

```bash
go test ./...
```

The suite covers price parsing with Persian and Arabic-Indic digits, symbol
matching, quote freshness, the state machine, configuration, credential
redaction, and the full order lifecycle against a fake broker adapter —
including the safety properties: an order cannot be submitted without the exact
confirmation word, a DOM mismatch aborts, a price leaving the range aborts, an
unknown market aborts, credentials never appear in logs, and a dry run can never
submit.
