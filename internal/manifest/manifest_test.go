package manifest

import "testing"

func TestSubtract(t *testing.T) {
	a := Package{Types: []Type{
		{Name: "ApexClass", Members: []string{"A", "B"}},
		{Name: "CustomField", Members: []string{"X__c.F__c"}},
	}}
	b := Package{Types: []Type{{Name: "ApexClass", Members: []string{"A"}}}}
	got := Subtract(a, b)
	if len(got.Types) != 2 || got.Types[0].Members[0] != "B" || got.Types[1].Members[0] != "X__c.F__c" {
		t.Fatalf("got %+v", got)
	}
}
