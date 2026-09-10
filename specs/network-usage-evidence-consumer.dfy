// SUP-919 abstractions: exact identity and durable sync are trusted inputs.
datatype Consumer = C(stopped: bool, total: nat, bound: bool, receipt: bool,
  claim: bool, failed: bool, complete: bool)
function Observe(s: Consumer, delta: nat, max: nat, valid: bool): Consumer {
  if s.stopped || !valid || delta > max - s.total
  then s.(stopped := true)
  else s.(total := s.total + delta)
}
function Bind(s: Consumer, synced: bool, identityMatches: bool): Consumer {
  s.(bound := s.bound || (synced && identityMatches && !s.failed))
}
function PersistReceipt(s: Consumer, synced: bool): Consumer {
  s.(receipt := s.receipt || (synced && s.bound && !s.failed))
}
function PersistClaim(s: Consumer, synced: bool, contentMatches: bool): Consumer {
  s.(claim := s.claim || (synced && contentMatches && s.receipt && s.bound && !s.failed))
}
predicate Ack(s: Consumer, contextLive: bool, sameContent: bool, sameSource: bool) {
  s.claim && s.receipt && s.bound && !s.failed && contextLive && sameContent && sameSource
}
predicate ReadAllowed(used: nat, bytes: nat, max: nat, count: nat, countMax: nat) {
  used <= max && bytes <= max-used && count < countMax
}
lemma PrefixCannotResume(s: Consumer, delta: nat, max: nat, valid: bool)
  requires s.stopped
  ensures Observe(s,delta,max,valid).total == s.total && Observe(s,delta,max,valid).stopped {}
lemma InvalidObservationNeverAdds(s: Consumer, delta: nat, max: nat)
  ensures Observe(s,delta,max,false).total == s.total {}
lemma CheckedDeltaRemainsBounded(s: Consumer, delta: nat, max: nat, valid: bool)
  requires s.total <= max
  ensures Observe(s,delta,max,valid).total <= max {}
lemma DeviceDeltaAddedOnce(s: Consumer, delta: nat, max: nat)
  requires !s.stopped && s.total <= max && delta <= max-s.total
  ensures Observe(s,delta,max,true).total == s.total+delta {}
lemma UnsyncedClaimCannotAck(s: Consumer, same: bool)
  requires !s.claim
  ensures !Ack(PersistClaim(s,false,same),true,true,true) {}
lemma ReceiptAloneCannotAck(s: Consumer, synced: bool)
  requires !s.claim
  ensures !Ack(PersistReceipt(s,synced),true,true,true) {}
lemma CanceledCannotAck(s: Consumer)
  ensures !Ack(s,false,true,true) {}
lemma ChangedContentOrSourceCannotAck(s: Consumer)
  ensures !Ack(s,true,false,true) && !Ack(s,true,true,false) {}
lemma ReservedReadStaysWithinBudget(used: nat, bytes: nat, max: nat, count: nat, countMax: nat)
  requires ReadAllowed(used,bytes,max,count,countMax)
  ensures used+bytes <= max && count+1 <= countMax {}
lemma NeverClaimsComplete(s: Consumer, delta: nat, max: nat, b: bool)
  ensures PersistClaim(PersistReceipt(Bind(Observe(s,delta,max,b),b,b),b),b,b).complete == s.complete {}
