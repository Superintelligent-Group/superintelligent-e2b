// SUP-902: local spool arithmetic, not a proof of filesystem honesty or delivery.
predicate Inv(used: nat, count: nat, budget: nat, limit: nat) {
  1 <= used <= budget && count <= limit
}
function Allows(used: nat, count: nat, bytes: nat, newSegment: bool,
                budget: nat, segment: nat, limit: nat): bool {
  bytes <= segment && used + bytes <= budget && (!newSegment || count < limit)
}
lemma AcceptedPrefixBounded(used: nat, count: nat, bytes: nat, actual: nat,
                           newSegment: bool, budget: nat, segment: nat, limit: nat)
  requires Inv(used, count, budget, limit)
  requires Allows(used,count,bytes,newSegment,budget,segment,limit)
  requires actual <= bytes
  ensures Inv(used+actual,count+(if newSegment then 1 else 0),budget,limit)
{}
lemma RetentionBounded(used: nat,count: nat,removed: nat,budget: nat,limit: nat)
  requires Inv(used,count,budget,limit) && count > 0 && removed < used
  ensures Inv(used-removed,count-1,budget,limit)
{}
lemma RefusalPreservesEvidence(used: nat,count: nat,budget: nat,limit: nat)
  requires Inv(used,count,budget,limit)
  ensures Inv(used,count,budget,limit)
{}
method Main() {
  for used := 1 to 11 {
    for count := 0 to 4 {
      for bytes := 0 to 6 {
        print used, ",",count,",",bytes,",false,",Allows(used,count,bytes,false,10,4,3),"\n";
        print used, ",",count,",",bytes,",true,",Allows(used,count,bytes,true,10,4,3),"\n";
      }
    }
  }
}
