# Broker UI analysis

Status: **not started — Phase 2**

This document records what was observed on the real brokerage site, and it is
the only source for the selector values in
`internal/broker/iranbroker/selectors.go`. Nothing here may be guessed: every
selector must be copied from the live DOM after inspecting it in a visible
Chromium window, and every entry should say how it was verified.

No order is ever placed while filling in this document.

## Brokerage

| Field | Value |
| --- | --- |
| Name | _to be filled_ |
| Login URL | _to be filled_ |
| Trading panel URL | _to be filled_ |
| CAPTCHA on login | _unknown_ |
| OTP / SMS on login | _unknown_ |
| Device confirmation | _unknown_ |

## Login flow

_Describe the pages, the redirects, what an authenticated page contains that an
unauthenticated one does not, and what any security challenge looks like._

## Pages

_List the URLs Goroker needs and what each one shows._

## Symbol search

_How the search box behaves, what a result row looks like, whether results can
contain several instruments with the same displayed symbol, and how an exact
match is recognised._

## Quote DOM

_Where last price, best ask and best bid live, how they update (WebSocket,
polling, full page reload), and what is shown when a value is unavailable._

## Market status

_The element that states whether the market is open, and the exact texts it can
contain. Any text that is not positively "open" or "closed" is treated as
UNKNOWN._

## BUY form

_The buy tab, the symbol field, the price input, the quantity input, the
estimated cost, the daily price band, and the submit button._

## Confirmation UI

_Whether the broker shows its own confirmation dialog before an order is sent._

## Order result UI

_Where the broker reports acceptance or rejection, the exact texts involved, and
where an order ID appears._

## Selectors

| Field | Selector | Strategy | Verified how |
| --- | --- | --- | --- |
| _to be filled_ | | | |

Fragile (positional) selectors, if any, go here with an explanation of why no
stabler selector exists and what to check when the UI changes.

## Edge cases

_Session expiry mid-session, modal dialogs, market pauses, symbol suspension,
price band changes, and anything else that must map onto an ABORT._
