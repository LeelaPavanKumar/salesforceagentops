package config

import "testing"

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"**/objects/Revenue_Schedule__c/**", "force-app/main/default/objects/Revenue_Schedule__c/fields/X__c.field-meta.xml", true},
		{"**/objects/Revenue_Schedule__c/**", "force-app/main/default/objects/Invoice__c/fields/X__c.field-meta.xml", false},
		{"force-app/**/profiles/*.profile-meta.xml", "force-app/main/default/profiles/Admin.profile-meta.xml", true},
		{"**/settings/Security.settings-meta.xml", "force-app/main/default/settings/Security.settings-meta.xml", true},
		{"*.xml", "a/b.xml", false},
	}
	for _, c := range cases {
		if got := MatchGlob(c.glob, c.path); got != c.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", c.glob, c.path, got, c.want)
		}
	}
}
