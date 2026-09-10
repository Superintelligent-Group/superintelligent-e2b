// SUP-913: abstract durable-inventory ordering, linked in the adjacent spec.
// Protected provider truth, filesystem durability and identity equality assumed.
datatype State = S(raw: bool, exactVersion: bool, inventory: bool, bound: bool, complete: bool)
function Upload(s: State, verified: bool): State {
  S(s.raw, s.exactVersion || verified, s.inventory, s.bound, s.complete)
}
function Persist(s: State, synced: bool): State {
  S(s.raw, s.exactVersion, s.inventory || (synced && s.exactVersion && s.bound), s.bound, s.complete)
}
function Reclaim(s: State, currentVersionVerified: bool): State {
  S(s.raw && !(s.inventory && s.bound && s.exactVersion && currentVersionVerified), s.exactVersion, s.inventory, s.bound, s.complete)
}
lemma UploadAloneRetainsRaw(s: State, verified: bool)
  ensures Upload(s, verified).raw == s.raw {}
lemma UnsyncedReceiptCannotAuthorize(s: State)
  requires !s.inventory
  ensures !Persist(s, false).inventory {}
lemma ReclaimRetainsRecoverableIdentity(s: State, verified: bool)
  requires s.raw
  ensures !Reclaim(s, verified).raw ==> Reclaim(s, verified).inventory && s.bound && s.exactVersion {}
lemma ReplacementIdentityCannotReclaim(s: State, verified: bool)
  requires !s.bound
  ensures Reclaim(s, verified).raw == s.raw {}
lemma InventorySurvivesReclaim(s: State, verified: bool)
  ensures Reclaim(s, verified).inventory == s.inventory {}
lemma NeverUpgradesComplete(s: State, verified: bool, synced: bool)
  ensures Reclaim(Persist(Upload(s, verified), synced), verified).complete == s.complete {}
