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

Private protocol v2 also identifies a dedicated inherited response-pipe handle
in that request. The parent explicitly inherits only the response write end in
addition to standard streams and closes its local write end after process start.
The child validates the pipe handle, clears its inheritance flag and owns its
closure. Standard output and standard error are separately drained and discarded
within fixed budgets; neither stream is accepted as the protocol response.
A native cancellation produced a valid reply preceded by an unrelated warning
on stdout. Separating the channel preserves strict JSON and nonce validation
without accepting arbitrary prefixes. The warning's originating component has
not been established, and this channel separation is not a security boundary
against code already running inside the child.

The parent creates a Job with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`. It starts the
child waiting for input, assigns the Job, and only then writes the request. If the
parent exits before assignment, closing the input pipe prevents picker dialog admission;
after assignment, closing the Job terminates its child processes. Failure to assign
the Job fails the request without opening an in-process fallback dialog.

The parent validates the bounded reply and nonce, waits for actual child exit and
closes its process/pipe/Job resources before publishing a selected path. A reply
alone is not completion. Nonzero exit, a malformed response, an oversized response
or standard stream, deadline or
host shutdown rejects the path and triggers child cleanup. The hard interaction
deadline is ten minutes; parent shutdown cancels sooner. Cancellation is distinct
from launch, protocol and child failures. Existing recovery returns the stored
result rather than starting a second child or opening the media twice.

The owner is the unique visible, unowned top-level window of the parent process,
never another application's foreground window. It must be enabled when passed
to IFileDialog. The parent does not pre-disable it and leaves modality to the
native dialog. A diagnostic reproduction observed WebView2 focus restoration
while the owner was disabled, before the parent's cleanup could run. Removing
the pre-disable is intended to avoid this ordering; real native cancellation
and selection must validate that behavior. The parent retains an identity-checked enabled-state
recovery on success, cancellation and failure, including a child terminated
before modal cleanup. Host close cancels the picker context
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
They also cover valid replies alongside stdout noise, malformed data on the
dedicated response pipe, bounded stdout/stderr, invalid response handles, and
pipe cleanup when child descendants retain inherited handles.

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
cancel attempt. That run did not complete native acceptance and remains a failed
historical observation; it is not replaced by later results.

The [2026-10-16 Windows follow-up](../desktop-windows-stability-20261016.md)
separates the historical exit timing, disabled-owner focus reproduction and
stdout contamination. It records the fixes, native results and deviations from
the registered acceptance protocol. The prototype remains opt-in; these local
results do not approve default rollout.
