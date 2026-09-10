// SUP-914 abstract durable-state ordering. Sync, immutable equality and exact
// provider version verification are assumptions, not filesystem/provider proofs.
datatype Closing = C(registered: bool, intent: bool, outcome: bool,
  frozen: bool, finalExact: bool, claim: bool, inventory: bool,
  failed: bool, complete: bool)

function Register(s: Closing, synced: bool): Closing {
  s.(registered := s.registered || (synced && !s.failed))
}
function Intent(s: Closing, synced: bool): Closing {
  s.(intent := s.intent || (s.registered && synced && !s.failed))
}
function SealOutcome(s: Closing, synced: bool): Closing {
  s.(outcome := s.outcome || (s.intent && synced && !s.failed))
}
function Freeze(s: Closing, allRawDrained: bool, synced: bool): Closing {
  s.(frozen := s.frozen || (s.outcome && allRawDrained && synced && !s.failed))
}
function VerifyFinal(s: Closing, exact: bool): Closing {
  s.(finalExact := s.finalExact || (s.frozen && exact && !s.failed))
}
function RetainClaim(s: Closing, synced: bool): Closing {
  s.(claim := s.claim || (s.frozen && s.finalExact && synced && !s.failed))
}
function Retire(s: Closing, exactAgain: bool, memberMatches: bool): Closing {
  s.(inventory := s.inventory && !(s.claim && s.frozen && s.finalExact && exactAgain && memberMatches && !s.failed))
}
function Fail(s: Closing): Closing { s.(failed := true) }
function CancelWait(s: Closing): Closing { s }

lemma AdmissionRequiresDurableRegistration(s: Closing)
  requires !s.registered
  ensures !Register(s, false).registered {}
lemma MissingIntentCannotBecomeKnownOutcome(s: Closing, synced: bool)
  requires !s.intent && !s.outcome
  ensures !SealOutcome(s, synced).outcome {}
lemma UnfinishedRawCannotFreeze(s: Closing, synced: bool)
  requires !s.frozen
  ensures !Freeze(s, false, synced).frozen {}
lemma UploadedButUnverifiedCannotClaim(s: Closing, synced: bool)
  requires !s.finalExact && !s.claim
  ensures !RetainClaim(s, synced).claim {}
lemma UnsyncedClaimCannotAuthorize(s: Closing)
  requires !s.claim
  ensures Retire(RetainClaim(s, false), true, true).inventory == s.inventory {}
lemma RetirementRequiresExactFrozenMembership(s: Closing, exact: bool, member: bool)
  requires s.inventory
  ensures !Retire(s, exact, member).inventory ==> s.claim && s.frozen && s.finalExact && exact && member && !s.failed {}
lemma FailureAbsorbsRetirement(s: Closing, exact: bool, member: bool)
  ensures Retire(Fail(s), exact, member).inventory == s.inventory {}
lemma CancellationDoesNotInventOutcome(s: Closing)
  ensures CancelWait(s).outcome == s.outcome && CancelWait(s).registered == s.registered {}
lemma TerminalClaimSurvivesRetirement(s: Closing, exact: bool, member: bool)
  ensures Retire(s, exact, member).claim == s.claim {}
lemma NeverUpgradesComplete(s: Closing, b: bool)
  ensures Retire(RetainClaim(VerifyFinal(Freeze(SealOutcome(Intent(Register(s,b),b),b),b,b),b),b),b,b).complete == s.complete {}
