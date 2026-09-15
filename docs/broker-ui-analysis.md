# Broker UI analysis — Mofid Easy Trader

Status: **Phase 2 in progress — selectors not established yet**

This document records what was observed on the real brokerage site. It is the
only source for the selector values in `internal/broker/mofid/selectors.go`.
Nothing here may be guessed: every selector must be copied from the live DOM of
`https://d.easytrader.ir/` and every entry must say how it was verified.

**No order is ever placed while filling in this document.**

## Brokerage

| Field | Value | Source |
| --- | --- | --- |
| Name | Mofid Easy Trader | account owner |
| Base URL | `https://d.easytrader.ir/` | account owner |
| Trading panel URL | `https://d.easytrader.ir/` (the panel is the app itself) | account owner |
| Login URL | _not established — the adapter looks for the login form on the base URL_ | |
| CAPTCHA on login | **no** | account owner |
| OTP on login | **yes — SMS one-time code** | account owner |
| Device confirmation | _unknown_ | |

### Consequence for the login flow

Because a fresh login always requires an SMS code, `goroker login` can fill in
the username and password at most, and then **must** hand the browser to the
account owner for the code. The persistent Chromium profile is what makes this
bearable: the session is reused across runs, so the SMS step happens only when
the session has actually expired.

Goroker never attempts to read, intercept, guess or bypass the SMS code.

## Order flow, as described by the account owner

```text
search for the instrument by name
  → click خرید (buy)
  → fill in the order form
  → click ارسال خرید (send buy order)
```

Mapped onto the adapter:

| Step | Adapter method | Selectors needed |
| --- | --- | --- |
| search for the instrument | `ResolveSymbol` | `symbol_search`, `symbol_result`, `symbol_result_name` |
| click خرید | `PrepareBuyOrder` | `buy_tab` |
| fill in the form | `PrepareBuyOrder` | `price_input`, `quantity_input` |
| read the form back | `ReadPreparedOrder` | `order_symbol_field`, `order_side_mark`, `price_input`, `quantity_input`, `estimated_cost` |
| click ارسال خرید | `SubmitPreparedOrder` | `submit_button`, and `submit_confirm_dialog`/`submit_confirm_accept` if Easy Trader shows its own confirmation |
| read the response | `SubmitPreparedOrder` | `result_message`, `result_accepted_text`, `result_rejected_text` |

## Discovery procedure

Run on the account owner's own machine, signed in to their own account:

```bash
goroker inspect
```

`inspect` opens `https://d.easytrader.ir/` in a visible Chromium with the
persistent profile and then only reads the page. It never clicks and never
submits. In the browser window:

1. Sign in, including the SMS code.
2. In the terminal: `snap logged-in` — captures the signed-in shell, which is
   where `logged_in_marker` and `market_status_indicator` come from.
3. Search for a symbol (e.g. فولاد) and, before clicking anything,
   `snap symbol-search` — the search box and the result list.
4. Open the instrument and `snap quote-panel` — last price, best ask, best bid.
5. Click خرید and `snap buy-form` — **fill nothing in yet**.
6. Type a price and quantity by hand, then `snap buy-form-filled`, and **do not
   click ارسال خرید**.
7. Optionally `snap order-confirm` if Easy Trader shows its own confirmation
   dialog before sending, and `snap order-result` after a real order placed
   manually, at the owner's discretion.
8. `quit`.

Snapshots land in `~/.goroker/debug/inspect/` and are sanitised: scripts and
inline styles are dropped, form values are replaced, and account numbers, phone
numbers, e-mail addresses and long tokens are redacted. Sanitising is best
effort — read a snapshot before sharing it.

`text CSS` and `count CSS` in the same session verify a candidate selector
before it is written down.

## Login flow

_To be filled from `snap logged-in` and from the signed-out state: what exists
on an authenticated page that does not exist on the login page, what the SMS
step looks like, and whether a "remember this device" option exists._

## Pages

_To be filled: the URLs Goroker needs, and whether Easy Trader is a
single-page app whose URL does not change between views (this decides whether
the adapter can navigate or must click)._

## Symbol search

_To be filled: how the search box behaves, what a result row contains, whether
two rows can show the same displayed symbol, and how an exact match is
recognised. Note that Goroker compares symbols exactly after normalising
Arabic yeh/kaf and zero-width characters, and aborts on more than one match._

## Quote DOM

_To be filled: where last price, best ask and best bid live, how they update
(WebSocket, polling, full reload), what is shown when a value is missing, and
whether the digits are Persian._

## Market status

_To be filled: the element that states whether the market is open, and the exact
texts it can contain. Any other text is treated as UNKNOWN, which is never
traded into._

## BUY form

_To be filled: the خرید tab, the instrument field, the price input, the quantity
input, the estimated cost, the daily price band, and the ارسال خرید button.
Also whether the inputs are plain `<input>` elements or a component that
reformats what is typed — the adapter re-reads every field from the DOM, so a
field that rewrites "3310" into "۳,۳۱۰" must still parse back to 3310._

## Confirmation UI

_To be filled: whether Easy Trader shows its own confirmation dialog after
ارسال خرید, and what accepting it looks like._

## Order result UI

_To be filled: where acceptance and rejection are reported, the exact texts, and
where an order ID appears._

## Selectors

| Field | Selector | Strategy | Verified how |
| --- | --- | --- | --- |
| _none established yet_ | | | |

Fragile (positional) selectors, if any, go here with an explanation of why no
stabler selector exists and what to re-check when the UI changes.

## Edge cases

_To be filled: session expiry mid-session, modal dialogs, market pause and
auction states, instrument suspension, price band changes, insufficient credit,
and anything else that must map onto an ABORT._
