# ADR-021: Isolated Windows file picker prototype

Status: Accepted for an opt-in prototype; production default unchanged

## Context

The [collection investigation](../desktop-gdi-collection-20261007.md) reproduced
Shell image-list retention after dialog cancellation, interface release, COM
cleanup and actual STA thread exit in independent native hosts. The remaining
two CDefCollection references have not been attributed. Altering undocumented
reference counts or deleting internal GDI objects is not justified.

We need to test a resource boundary that the application controls while keeping
ADR-012's request fencing and recovery and ADR-013's source ownership intact.

## Decision

On Windows, the `pickerprocess` build tag opts into one child process for each
accepted FLV open request. The ordinary build continues using Wails's picker;
non-Windows hosts and the diagnostic JSON save dialog are outside this prototype.
`desktop/build-picker-prototype.ps1` produces a separately named executable.

The child is the same executable, entered by one exact internal argument before
`wails.Run`. Go dependency initializers still run: Wails/winc can initialize GDI+
and common controls and create a default font. This is not a minimal native-only
binary or a claim of zero GDI activity before reading the request.
It reads a bounded, versioned UTF-8 JSON request from an anonymous
pipe. It does not initialise a WebView or scan the chosen media. The request
includes a nonce, the owning HWND and the parent process identity. The child
validates these before creating a single IFileOpenDialog on a locked STA thread.
Only an absolute filesystem path, cancellation or a generic error is returned.
Paths are not placed in command-line arguments, temporary files or diagnostic logs.

The parent creates a Job with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`. It starts the
child waiting for input, assigns the Job, and only then writes the request. If the
parent exits before assignment, closing the input pipe prevents picker dialog admission;
after assignment, closing the Job terminates its child processes. Failure to assign
the Job fails the request without opening an in-process fallback dialog.

The parent validates the bounded reply and nonce, waits for actual child exit and
closes its process/pipe/Job resources before publishing a selected path. A reply
alone is not completion. Nonzero exit, malformed or oversized output, deadline or
host shutdown rejects the path and triggers child cleanup. The hard interaction
deadline is ten minutes; parent shutdown cancels sooner. Cancellation is distinct
from launch, protocol and child failures. Existing recovery returns the stored
result rather than starting a second child or opening the media twice.

The owner is the unique visible, unowned top-level window of the parent process,
never another application's foreground window. The parent restores its enabled
state on success, cancellation and failure. Host close cancels the picker context
and performs bounded cleanup waiting. A parent crash is handled by the Job, not
by relying on an OnShutdown callback.

The main process opens and owns the selected source only after picker completion;
the child cannot create a document session. The public JS methods, request IDs,
DTOs, source-change protection and lost-response recovery remain unchanged.

## Validation and limits

Process tests must cover successful/cancelled results, malformed and oversized
messages, wrong nonce, nonzero exit, a reply followed by a still-running child,
timeout/cancellation, and Job lifetime when the parent exits. They use controlled
non-UI children; no automated test should unexpectedly open a dialog.

Native acceptance uses the real Shell dialog and the same FLV for three cancel
and three successful opens in one parent process. It records exact process
generations, actual child exit, parent recovery, source closure, and fixed quiet
windows of GDI/USER/handle counts; the final idle window is extended. Sampling
cannot prove every GDI destruction call, and process-tree polling can miss very
short-lived descendants. The evidence must distinguish those limits from actual
observed child exit and any parent growth.

The prototype is resource isolation, not a security sandbox or a claim to repair
the unknown Shell reference source. Signing, installation/packaging, default
rollout, save-dialog isolation and non-Windows parity require separate decisions.
Actual results and any unmet acceptance criteria belong in the validation report.

The [2026-10-07 validation report](../desktop-picker-process-20261007.md) records
passing automated checks but an abnormal parent exit during the first native
cancel attempt. Native acceptance is incomplete; the prototype is not ready for
default rollout.
