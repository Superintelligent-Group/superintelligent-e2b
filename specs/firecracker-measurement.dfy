// SUP-908 abstract serialized producer model; not a Rust refinement proof.
// Sink mutex supplies the assumed total order. Failed attempt includes partial
// serialization, oversize, write and flush errors. Replay preserves historical
// receipt, not later loss. No persistence or network-lifetime claim.
module MeasurementProducer {
  const Max: nat := 18446744073709551615
  datatype State = S(attempt: nat, request: nat, cachedAttempt: nat,
                     cachedSuccess: bool, cachedLoss: bool, loss: bool)
  predicate Valid(s: State) {
    s.attempt <= Max && s.request <= Max && s.cachedAttempt <= s.attempt
  }
  function Initial(): State { S(0, 0, 0, false, false, false) }
  // accepted request sequence validation occurs before this transition;
  // exact last-tuple replay and malformed/conflicting calls do not transition.
  function Emit(s: State, requested: bool, succeeds: bool): State
    requires Valid(s)
    requires requested ==> s.request < Max
  {
    var exhausted := s.attempt == Max;
    var next := if exhausted then s.attempt else s.attempt + 1;
    var ok := succeeds && !exhausted;
    S(next, if requested then s.request + 1 else s.request,
      if requested then next else s.cachedAttempt,
      if requested then ok else s.cachedSuccess,
      if requested then s.loss else s.cachedLoss,
      s.loss || !ok)
  }
  lemma EmissionPreservesBounds(s: State, requested: bool, succeeds: bool)
    requires Valid(s)
    requires requested ==> s.request < Max
    ensures Valid(Emit(s, requested, succeeds))
    ensures Emit(s, requested, succeeds).attempt >= s.attempt
    ensures s.loss ==> Emit(s, requested, succeeds).loss
    ensures !succeeds ==> Emit(s, requested, succeeds).loss
    ensures s.attempt == Max ==> Emit(s, requested, succeeds).loss
  {}
  lemma PeriodicPreservesLastReceipt(s: State, succeeds: bool)
    requires Valid(s)
    ensures Emit(s, false, succeeds).request == s.request
    ensures Emit(s, false, succeeds).cachedAttempt == s.cachedAttempt
    ensures Emit(s, false, succeeds).cachedSuccess == s.cachedSuccess
    ensures Emit(s, false, succeeds).cachedLoss == s.cachedLoss
  {}
  lemma RequestedSuccessReflectsPriorLoss(s: State)
    requires Valid(s) && s.request < Max && s.attempt < Max
    ensures Emit(s, true, true).cachedSuccess
    ensures Emit(s, true, true).cachedLoss == s.loss
    ensures Emit(s, true, true).cachedAttempt == s.attempt + 1
  {}
  lemma FailureThenSuccessNeverClaimsLossFree(s: State)
    requires Valid(s) && s.request + 2 <= Max && s.attempt + 2 <= Max
    ensures Emit(Emit(s, true, false), true, true).cachedLoss
    ensures Emit(Emit(s, true, false), true, true).loss
  {}
  // Replay is identity; no second serialization, attempt allocation, or loss reset.
  function Replay(s: State): State { s }
  lemma ReplayPreservesState(s: State)
    ensures Replay(s) == s
  {}
}
