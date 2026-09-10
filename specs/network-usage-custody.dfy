// SUP-903: receipt identity and custody ordering. Provider truth is an assumption.
datatype State = S(local: bool, remoteExact: bool, complete: bool)
function Matches(valid: bool,destination: bool,claim: bool,key: bool): bool {
  valid && destination && claim && key
}
function Upload(s: State, committed: bool): State {
  S(s.local,s.remoteExact || committed,s.complete)
}
function Reclaim(s: State, verified: bool, matches: bool, deletionDurable: bool): State
  requires verified ==> s.remoteExact
{
  S(s.local && !(verified && matches && deletionDurable),s.remoteExact,s.complete)
}
lemma NeverDeleteWithoutCustody(s: State,verified: bool,matches: bool,durable: bool)
  requires s.local && (verified ==> s.remoteExact)
  ensures !Reclaim(s,verified,matches,durable).local ==> Reclaim(s,verified,matches,durable).remoteExact
{}
lemma LostUploadResponseRetainsLocal(s: State,committed: bool)
  ensures Upload(s,committed).local == s.local
{}
lemma NeverEstablishesCompleteness(s: State,committed: bool,verified: bool,matches: bool,durable: bool)
  requires verified ==> Upload(s,committed).remoteExact
  ensures Reclaim(Upload(s,committed),verified,matches,durable).complete == s.complete
{}
lemma ReceiptMismatchBlocksReclaim(s: State,verified: bool,durable: bool,valid: bool,destination: bool,claim: bool,key: bool)
  requires verified ==> s.remoteExact
  requires !valid || !destination || !claim || !key
  ensures Reclaim(s,verified,Matches(valid,destination,claim,key),durable).local == s.local
{}
method Main(){
 for v:=0 to 2 {for d:=0 to 2 {for c:=0 to 2 {for k:=0 to 2 {
  print v==1,",",d==1,",",c==1,",",k==1,",",Matches(v==1,d==1,c==1,k==1),"\n";
 }}}}
}
