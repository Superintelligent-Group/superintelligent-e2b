// SUP909 abstract state machine, not Rust refinement or KVM/network evidence.
module TerminalMeasurement {
  datatype Phase = Active | Latched | Emitted | Failed
  datatype S = State(phase: Phase, paused: bool, kicks: nat, callbacks: nat, emitted: nat)
  predicate Frozen(s: S) { s.phase != Active || s.paused }
  function Latch(s: S): S
    requires s.phase == Active
  { State(Latched, s.paused, s.kicks, s.callbacks, s.emitted) }
  function Finish(s: S, pauseOk: bool, writeOk: bool): S
    requires s.phase == Latched
  { State(if pauseOk && writeOk then Emitted else Failed,
          s.paused || pauseOk, s.kicks, s.callbacks,
          s.emitted + (if pauseOk && writeOk then 1 else 0)) }
  function Resume(s: S): S {
    if s.phase != Active then s else State(Active, false, s.kicks + 1, s.callbacks, s.emitted)
  }
  function Dispatch(s: S): S {
    if Frozen(s) then s else State(s.phase, s.paused, s.kicks, s.callbacks + 1, s.emitted)
  }
  function Replay(s: S): S { s }
  lemma TerminalIsAbsorbing(s: S)
    requires s.phase != Active
    ensures Resume(s) == s && Dispatch(s) == s && Replay(s) == s
  {}
  lemma FailureStillFreezes(s: S, pauseOk: bool, writeOk: bool)
    requires s.phase == Latched
    ensures Frozen(Finish(s, pauseOk, writeOk))
    ensures Finish(s, pauseOk, writeOk).kicks == s.kicks
    ensures Finish(s, pauseOk, writeOk).callbacks == s.callbacks
    ensures !pauseOk ==> Finish(s, pauseOk, writeOk).emitted == s.emitted
    ensures !writeOk ==> Finish(s, pauseOk, writeOk).phase == Failed
  {}
  lemma RejectedResumeDoesNotDispatch(s: S)
    requires s.phase != Active
    ensures Frozen(Resume(s)) && Dispatch(Resume(s)) == s
  {}
  lemma SuccessfulReceiptRequiresPause(s: S)
    requires s.phase == Latched
    ensures Finish(s, true, true).paused
    ensures Finish(s, true, true).emitted == s.emitted + 1
    ensures Frozen(Finish(s, true, true))
  {}
}
