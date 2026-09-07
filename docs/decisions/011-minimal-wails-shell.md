# ADR-011: An isolated Wails host and bounded desktop UI

Status: Accepted

## Context

ADR-010 provides the desktop transport without a window. The next increment
needs an actual file picker and React UI without introducing native build
requirements into the CLI or exposing the scanner's full index.

## Decision

Use the reviewed Wails v2.14.0 with React/TypeScript in a separate `desktop/`
Go module (`github.com/sevico/muxray/desktop`). It requires the parent module
through a relative replace; no workspace file or root GUI dependency is added.
Root `go test ./...` and CGO-disabled builds remain independent. Desktop checks
run explicitly in their own CI job. This increment targets Windows; other
desktop platforms require separate native build and interaction validation.

Bind the unchanged six-method adapter plus a separate `Native` object with
only `OpenFLV()`. The latter serializes native dialogs, opens the selected path
through the adapter, and returns a basename and session envelope (never the
full path or OS error). Picker cancellation is not a scan failure. Startup,
window-close cancellation, and shutdown remain unbound host callbacks. Closing
the window immediately stops admission and cancels work; final shutdown waits
at most three seconds for cleanup. A timeout does not prove blocked I/O ended;
the process then exits and the OS reclaims its resources.

Production uses embedded assets only, no remote content, API server, arbitrary
URL opener, updater, persistence of selected paths, or default file drop.
Keep Wails' default binding-origin restriction; do not add allowed origins.
Apply a restrictive Content Security Policy compatible with Wails' local
runtime scripts. Render all file/diagnostic strings as React text, not HTML.
Do not bundle a WebView2 installer: use the `error` runtime strategy so a
missing runtime is reported without an automatic download/install. The
WebView's own OS-managed profile is not an application results database.

The UI owns one session and one visible page of at most 100 rows. Poll only
scalar session status, sequentially at 200 ms intervals after each response,
and stop after terminal status (unless waiting for closed work to terminate).
Pages are available only after an actual scan is terminal. Cancel retains
partial results; close clears them and waits for slot release before allowing
another open. Session IDs and local request generations reject stale replies;
there is no unbounded request queue, page cache, or event history. Keep exact
decimal strings for offsets, DTS, IDs and totals, using BigInt for pagination
and scaled progress arithmetic before converting a bounded percentage.

Do not add details, hex, summaries, charts, export, drag/drop, a second session,
or live index pages. Those need subsequent adapter/lifecycle decisions.

## Validation

Test native picker cancellation/failure/concurrency and close-during-dialog
with injected functions, the unbound lifecycle surface, exact numeric UI
helpers, stale page/poll responses, transport failures, and bounded sequential
polling. Generate real Wails bindings; type-check, test and build the frontend;
build a production Windows executable and exercise its real window and picker.
Run core tests/build/vet/race independently. Neither generated binaries nor
private media are committed or distributed. Public release and third-party
license/security review remain separate from this local development build.
