# SUP-921: deferred RX at terminal cutoff

This opt-in local fixture extends SUP-920; it does not change the producer or
runtime. Run `TestMeasurementRuntimeDeferredRXCalibration` only with explicit
`SUP921_CALIBRATION=1` and `SUP912_KVM_ACCEPTANCE=1` in the isolated, pinned local
KVM environment. Preparation and launch must remain separate, serialized steps
bound to a reviewed commit archive, immutable inputs and a readiness receipt.
The producer remains SHA256
`bfe0e5276dcdc8614316335887d7c8555eb6544d707b359070e420f67f33f9f7`.

The actual Process boots and establishes positive guest traffic. Capture and
completed-syscall tracing start before an exact durable sample. The fixture sends
the supported `PATCH /network-interfaces/{iface_id}` request with RX operations
`size=1`, `one_time_burst=0`, `refill_time=3600000` milliseconds. It records the
actual request and 204 response. A zero size/refill would disable the bucket and
is not used. TX remains unrestricted. The one-hour refill does not disable the
producer's 100 ms timer polling; acceptance requires the observed RX reads to
remain exactly A then B throughout the measured interval.

Four distinct ICMP payloads bind every controlled frame. A must produce an exact
guest reply before B and C are injected. A held sample must show successful TAP
reads of A and B, no read of C, a positive throttle delta, no RX buffer shortage,
and independent packet capture of A/B/C with only A's reply. No incidental RX is
allowed to ambiguously consume the sole token. TX control frames are classified,
hashed and included exactly. Unknown traffic causes failure, not a tolerance.

B's deferred state is a **pinned-source inference**, supported by those independent
reads/injections and the producer's throttle evidence: `net/device.rs:635-648`
increments RX counters before the limiter rejects delivery; `:677-685` retries
the held buffer without another read/count. This is not direct descriptor-memory
inspection. Lack of a reply alone proves neither that state nor packet loss.
Captured C is proven unread by this producer; the fixture does not claim to
inspect its position in the kernel TAP queue.

The real `Process.FinalizeMeasurement` action produces the exact terminal fence.
The test-only accessor checks the ending/terminal latches and matched reference
under the action gate. A second production finalization exercises replay. D is
injected after that response while both tracing and capture remain active. There
must be no subsequent TAP I/O, producer emission or controlled reply. The fixture
never pauses/snapshots while B is held: pinned `Net::prepare_save` explicitly
finishes deferred RX and would change the state under test.

After detach, Process Stop/Join, service Close and raw spool reopen, the final
verifier reconstructs the complete start/held/terminal stream and every intervening
frame. Live trace inspection is readiness evidence only and cannot authorize a
final pass. Capture must report zero drops and account for every received packet.
Expected controlled RX is `2 * (12 + 14 + 20 + 8 + 64) = 236` bytes, including B;
C and D contribute zero. The 12-byte prefix is observed by exact AF_PACKET suffix
matching and interpreted using pinned `virtio_net_hdr_v1` source, not independently
read back as a negotiated ioctl value. TX includes A's reply and every observed
control frame. Journal bytes and syscall totals must agree exactly.

All evidence remains `complete=false`. This child does not close unsent TX, MMDS,
transport loss, custody/manifest integration, cloud authorization, provider-billed
traffic or broad SUP-916 acceptance. SUP-920 run 5's unexplained guest kernel panic
remains an open reliability limitation; this fixture does not erase or resolve it.
