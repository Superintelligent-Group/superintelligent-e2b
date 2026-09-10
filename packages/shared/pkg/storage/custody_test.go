package storage

import (
	"encoding/csv"
	"os"
	"strconv"
	"testing"
)

func TestCustodyDafnyReceiptIdentity(t *testing.T) {
	f, e := os.Open("testdata/custody-model.csv")
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	rows, e := csv.NewReader(f).ReadAll()
	if e != nil || len(rows) != 16 {
		t.Fatalf("invalid oracle: %v", e)
	}
	for _, row := range rows {
		flags := make([]bool, 5)
		for i, value := range row {
			flags[i], e = strconv.ParseBool(value)
			if e != nil {
				t.Fatal(e)
			}
		}
		d := custodyTestDestination()
		c := custodyTestClaim([]byte("raw"))
		key, e := d.ObjectKey(c)
		if e != nil {
			t.Fatal(e)
		}
		r := CustodyReceipt{Destination: d, Claim: c, Key: key}
		if !flags[0] {
			d.AccountID = "invalid"
		}
		if !flags[1] {
			r.Destination.ClaimedHostID = "other"
		}
		if !flags[2] {
			r.Claim.Bytes++
		}
		if !flags[3] {
			r.Key = "other"
		}
		if got := r.Matches(d, c); got != flags[4] {
			t.Fatalf("oracle mismatch: %v", row)
		}
	}
}
