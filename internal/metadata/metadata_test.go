package metadata

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct{ path, typ, name string }{
		{"force-app/main/default/classes/InvoiceService.cls", "ApexClass", "InvoiceService"},
		{"force-app/main/default/classes/InvoiceService.cls-meta.xml", "ApexClass", "InvoiceService"},
		{"force-app/main/default/triggers/InvoiceTrigger.trigger", "ApexTrigger", "InvoiceTrigger"},
		{"force-app/main/default/lwc/invoiceCard/invoiceCard.js", "LightningComponentBundle", "invoiceCard"},
		{"force-app/main/default/objects/Invoice__c/fields/Tier__c.field-meta.xml", "CustomField", "Invoice__c.Tier__c"},
		{"force-app/main/default/objects/Invoice__c/Invoice__c.object-meta.xml", "CustomObject", "Invoice__c"},
		{"force-app/main/default/profiles/Finance User.profile-meta.xml", "Profile", "Finance User"},
		{"force-app/main/default/reports/Finance/Open_Invoices.report-meta.xml", "Report", "Finance/Open_Invoices"},
		{"manifest/destructiveChanges.xml", "DestructiveChanges", "destructiveChanges.xml"},
		{"force-app/main/default/settings/Security.settings-meta.xml", "Settings", "Security"},
	}
	for _, c := range cases {
		typ, name, ok := Classify(c.path)
		if !ok || typ != c.typ || name != c.name {
			t.Errorf("Classify(%q) = %q %q %v, want %q %q", c.path, typ, name, ok, c.typ, c.name)
		}
	}
	for _, p := range []string{"README.md", ".github/workflows/ci.yml", "go.mod"} {
		if _, _, ok := Classify(p); ok {
			t.Errorf("Classify(%q) should not be metadata", p)
		}
	}
}

func TestComponentsGroupsFiles(t *testing.T) {
	comps := Components([]Change{
		{Status: Modified, Path: "force-app/main/default/classes/A.cls"},
		{Status: Modified, Path: "force-app/main/default/classes/A.cls-meta.xml"},
		{Status: Deleted, Path: "force-app/main/default/objects/X__c/fields/F__c.field-meta.xml"},
		{Status: Modified, Path: "README.md"},
	})
	if len(comps) != 2 {
		t.Fatalf("got %d components, want 2: %+v", len(comps), comps)
	}
	if comps[0].Key() != "ApexClass:A" || len(comps[0].Paths) != 2 {
		t.Errorf("unexpected first component %+v", comps[0])
	}
	if comps[1].Status != Deleted {
		t.Errorf("field should be Deleted, got %s", comps[1].Status)
	}
}
