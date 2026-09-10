// SUP911 abstract kernel; assumes serialized callbacks and truthful Sync outcome.
// Not a Go refinement, process recovery, stream completeness or custody proof.
module DurableCorrelation {
 datatype State = S(frame: bool, receipt: bool, fence: bool, failed: bool, terminalSeen: bool)
 function Init(): State {S(false,false,false,false,false)}
 predicate Inv(s: State) {s.fence ==> s.frame && s.receipt && !s.failed}
 function Fail(s: State): State {S(s.frame,s.receipt,false,true,s.terminalSeen)}
 function Frame(s: State, terminal: bool, synced: bool): State {
  if s.failed || s.terminalSeen || !synced then Fail(s)
  else S(true,s.receipt,false,false,terminal)
 }
 function Receipt(s: State): State {
  if s.failed then Fail(s) else S(s.frame,true,s.fence,false,s.terminalSeen)
 }
 function Match(s: State, exact: bool, synced: bool): State {
  if s.failed || !exact || !synced then Fail(s)
  else if s.frame && s.receipt then S(true,true,true,false,s.terminalSeen)
  else s
 }
 lemma NoPrematureFence(s: State, exact: bool, synced: bool)
  requires Inv(s)
  ensures Inv(Match(s,exact,synced))
  ensures Match(s,exact,synced).fence ==> synced && exact
 {}
 lemma FailedPersistenceNeverPublishes(s: State)
  ensures !Match(s,true,false).fence && Match(s,true,false).failed
 {}
 lemma TerminalBeforeResponseIsBarrier(s: State)
  requires s.terminalSeen
  ensures Frame(s,false,true).failed
  ensures !Match(Receipt(Frame(s,false,true)),true,true).fence
 {}
 lemma BothArrivalOrdersRequireMatch()
  ensures !Frame(Receipt(Init()),false,true).fence
  ensures !Receipt(Frame(Init(),false,true)).fence
  ensures Match(Frame(Receipt(Init()),false,true),true,true).fence
  ensures Match(Receipt(Frame(Init(),false,true)),true,true).fence
 {}
 lemma FailureAbsorbs(s: State)
  requires s.failed
  ensures !Match(Receipt(s),true,true).fence
  ensures !Match(Frame(s,false,true),true,true).fence
 {}
}
