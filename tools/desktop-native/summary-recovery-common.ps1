# Uses UTC conversion and identity helpers from common.ps1. No GUI automation.
function Assert-SummaryRecoveryOrder($Run, $Entered, $Command, $Release, $Released, $Closed, $Observation) {
    if ($Run.gateMode -ne 'Summary' -or $Command.action -ne 'MarkSummaryCancel') { throw 'Summary recovery requires a Summary gate and MarkSummaryCancel.' }
    if ($Observation.format -ne 'muxray-summary-recovery-observation-v1' -or $Observation.runId -ne $Run.runId) { throw 'Summary observation does not identify this run.' }
    foreach ($item in @($Command, $Release, $Released, $Closed, $Observation)) { Assert-SameProcess $Entered $item }
    if ($Observation.observations.Count -ne 4) { throw 'Supply canceling, canceled, ready and empty GUI observations in order.' }
    $expected = @('canceling', 'canceled', 'ready', 'empty')
    for ($i = 0; $i -lt 4; $i++) {
        $item = $Observation.observations[$i]
        if ($item.state -ne $expected[$i] -or @($item.evidenceFiles).Count -lt 1) { throw "Missing $($expected[$i]) GUI observation or evidence files." }
    }
    $ready = $Observation.observations[2]
    if ($ready.processed -ne $Run.committedTags -or $ready.total -ne $Run.committedTags -or $ready.indexedTags -ne $Run.committedTags) { throw 'The ready GUI observation must cover all retained Tags.' }
    $times = @($Entered.timeUTC, $Command.timeUTC, $Observation.observations[0].timeUTC,
        $Release.timeUTC, $Released.timeUTC, $Observation.observations[1].timeUTC,
        $Observation.observations[2].timeUTC, $Closed.timeUTC, $Observation.observations[3].timeUTC)
    for ($i = 1; $i -lt $times.Count; $i++) {
        if ((Convert-ReceiptTime $times[$i - 1]) -ge (Convert-ReceiptTime $times[$i])) {
            throw 'Expected entered < cancel marker < canceling UI < release < released < canceled UI < ready UI < closed < empty UI.'
        }
    }
    if ($Closed.PSObject.Properties['closeError'] -and $Closed.closeError) { throw "Source Close failed: $($Closed.closeError)" }
}
