# SUP-910: owned metrics FIFO lifecycle

This prerequisite changes ownership of the existing legacy metrics reader. It
does not configure the correlated producer, final cutoff, spool delivery, or
complete measurement; those remain SUP-907. Journal configuration remains opt-in,
and all journal closures retain `complete=false`.

`Process.startMetricsReader` synchronously opens the journal and both FIFO
descriptors before returning. Nonblocking, no-follow opens validate the FIFO type
and that both descriptors refer to the same inode. Errors close acquired resources
and return to Create/Resume. The process prevents starting another reader after
Stop or startup abort. This is local resource readiness, not producer baseline
or correlated persistence acknowledgment.

One owned session runs the parser and periodic flusher. Existing OTEL and balloon
accumulation consume complete legacy frames. A trailing fragment without newline
is rejected and recorded as `partial_metrics_frame`, even if it is valid JSON.
The bounded parser accepts one MiB of JSON plus its newline. Parser, flush, and
journal-close failures are returned by the join; error memory retains the first
failure rather than growing per malformed frame.

Create and Resume defer startup abort on every error, independently of the
process Exit channel. Abort closes the reader to wake a blocked FIFO read and
records an incomplete startup. Stop cancels the flusher and prevents new reader
startup. Process exit drops the keeper descriptor so buffered data can drain.
Sandbox doStop attempts process and cgroup termination before JoinMetrics, so a
surviving FIFO writer cannot prevent the cgroup kill. The join precedes ordinary
cleanup and asynchronous network-slot return. Snapshot behavior stays unchanged.

A successful join guarantees both workers have ended and journal Close finished.
Cancellation or the five-second join deadline during reading forces descriptor
closure and latches an interrupted-stream gap before sealing. It returns an
explicitly incomplete error and does **not** claim a completed durable join. The
reader remains the owner of eventual gap/Close if persistence is stalled; Go
filesystem sync has no cancellation contract. Once input and flushing have ended,
the session enters sealing. Cancellation then interrupts only the caller's wait;
it cannot change already-drained evidence or append a new gap during Close.
A later successful join truthfully observes completed persistence, still without
terminal coverage. Completed results are immutable and win over canceled callers.
Resource cleanup proceeds after an interrupted wait.

The reader waits for the canceled flusher before closing the journal. A successful
join therefore excludes late writes. No HTTP request waits while holding the
reader's persistence lock. Failed joins never authorize deleting evidence or
claiming full terminal coverage.

Linux tests in `fc/metrics_reader_test.go` exercise real FIFO opens, valid queued
frames, blocking consumption, surviving writers, abort without process exit,
cancellation, flusher completion, full-size frames, oversized frames, partial
tails, Close failures, and an actual durable journal's incomplete terminal record.
Run the fc and networkusage packages with race detection, then the sandbox package
to compile/test the shutdown integration. No KVM is required for these FIFO tests.
