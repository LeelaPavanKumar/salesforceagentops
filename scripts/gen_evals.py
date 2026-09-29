#!/usr/bin/env python3
"""Generates evals/cases/*. Re-run after editing; each case is self-contained."""
import os, shutil, textwrap, yaml

ROOT = os.path.join(os.path.dirname(__file__), "..", "evals", "cases")
D = "force-app/main/default"

def meta(kind, api="62.0", body=""):
    return f'<?xml version="1.0" encoding="UTF-8"?>\n<{kind} xmlns="http://soap.sforce.com/2006/04/metadata">\n    <apiVersion>{api}</apiVersion>\n{body}</{kind}>\n'

def cls_meta(api="62.0"):
    return meta("ApexClass", api, "    <status>Active</status>\n")

def field(obj, name, label="Field"):
    return f'''<?xml version="1.0" encoding="UTF-8"?>
<CustomField xmlns="http://soap.sforce.com/2006/04/metadata">
    <fullName>{name}</fullName>
    <label>{label}</label>
    <type>Text</type>
    <length>80</length>
</CustomField>
'''

def permset(fields=(), classes=(), extra=""):
    fp = "".join(f"    <fieldPermissions>\n        <editable>true</editable>\n        <field>{f}</field>\n        <readable>true</readable>\n    </fieldPermissions>\n" for f in fields)
    ca = "".join(f"    <classAccesses>\n        <apexClass>{c}</apexClass>\n        <enabled>true</enabled>\n    </classAccesses>\n" for c in classes)
    return f'<?xml version="1.0" encoding="UTF-8"?>\n<PermissionSet xmlns="http://soap.sforce.com/2006/04/metadata">\n{ca}{fp}{extra}    <label>Invoice Admin</label>\n</PermissionSet>\n'

def profile(fields=()):
    fp = "".join(f"    <fieldPermissions>\n        <editable>{e}</editable>\n        <field>{f}</field>\n        <readable>true</readable>\n    </fieldPermissions>\n" for f, e in fields)
    return f'<?xml version="1.0" encoding="UTF-8"?>\n<Profile xmlns="http://soap.sforce.com/2006/04/metadata">\n{fp}    <custom>true</custom>\n</Profile>\n'

REPORT = '<?xml version="1.0" encoding="UTF-8"?>\n<Report xmlns="http://soap.sforce.com/2006/04/metadata">\n    <format>Tabular</format>\n    <name>Open Invoices</name>\n    <reportType>CustomEntity$Invoice__c</reportType>\n</Report>\n'
LISTVIEW = '<?xml version="1.0" encoding="UTF-8"?>\n<ListView xmlns="http://soap.sforce.com/2006/04/metadata">\n    <fullName>All_Open</fullName>\n    <filterScope>Everything</filterScope>\n    <label>All Open</label>\n</ListView>\n'

def layout(fields):
    items = "".join(f"            <layoutItems>\n                <behavior>Edit</behavior>\n                <field>{f}</field>\n            </layoutItems>\n" for f in fields)
    return f'<?xml version="1.0" encoding="UTF-8"?>\n<Layout xmlns="http://soap.sforce.com/2006/04/metadata">\n    <layoutSections>\n        <label>Information</label>\n        <layoutColumns>\n{items}        </layoutColumns>\n        <style>TwoColumnsTopToBottom</style>\n    </layoutSections>\n</Layout>\n'

def flow(fault=True):
    fc = "        <faultConnector>\n            <targetReference>Log_Error</targetReference>\n        </faultConnector>\n" if fault else ""
    return f'''<?xml version="1.0" encoding="UTF-8"?>
<Flow xmlns="http://soap.sforce.com/2006/04/metadata">
    <apiVersion>62.0</apiVersion>
    <label>Invoice Mark Paid</label>
    <processType>AutoLaunchedFlow</processType>
    <recordUpdates>
        <name>Mark_Paid</name>
        <label>Mark Paid</label>
{fc}        <inputReference>$Record</inputReference>
    </recordUpdates>
    <status>Active</status>
</Flow>
'''

SERVICE = textwrap.dedent('''\
    public with sharing class InvoiceService {
        public static void markPaid(List<Invoice__c> invoices) {
            for (Invoice__c inv : invoices) {
                inv.Status__c = 'Paid';
            }
            update invoices;
        }
    }
    ''')
SERVICE_TEST = textwrap.dedent('''\
    @isTest
    private class InvoiceServiceTest {
        @isTest static void marksPaid() {
            Invoice__c inv = new Invoice__c(Status__c = 'Open');
            insert inv;
            InvoiceService.markPaid(new List<Invoice__c>{ inv });
            System.assertEquals('Paid', [SELECT Status__c FROM Invoice__c WHERE Id = :inv.Id].Status__c);
        }
    }
    ''')
BASE_FIELDS = {
    f"{D}/objects/Invoice__c/Invoice__c.object-meta.xml": meta("CustomObject", "62.0", "    <label>Invoice</label>\n").replace("    <apiVersion>62.0</apiVersion>\n", ""),
    f"{D}/objects/Invoice__c/fields/Status__c.field-meta.xml": field("Invoice__c", "Status__c", "Status"),
}

CASES = []
def case(name, desc, changes, expect, files, hotfix=False, cfg=None):
    CASES.append(dict(name=name, desc=desc, changes=changes, expect=expect, files=files, hotfix=hotfix, cfg=cfg))

def M(p, lines=10): return {"status": "M", "path": p, "lines": lines}
def A(p, lines=20): return {"status": "A", "path": p, "lines": lines}
def Dl(p, lines=10): return {"status": "D", "path": p, "lines": lines}

rep = f"{D}/reports/Finance/Open_Invoices.report-meta.xml"
lv = f"{D}/objects/Invoice__c/listViews/All_Open.listView-meta.xml"
case("01-reports-only", "Report and list view only: safe for the agentic lane.",
     [M(rep), M(lv)], dict(lane="agentic", blocked=False, rules=[]),
     {rep: REPORT, lv: LISTVIEW, **BASE_FIELDS})

ps = f"{D}/permissionsets/Invoice_Admin.permissionset-meta.xml"
case("02-permset-missing-field", "Permission set grants a field that is not in the PR or the repo.",
     [M(ps)], dict(lane="human-in-the-loop", blocked=True, rules=["DEP001"]),
     {ps: permset(["Invoice__c.Status__c", "Invoice__c.Tier__c"]), **BASE_FIELDS})

pr = f"{D}/profiles/Finance User.profile-meta.xml"
case("03-profile-bad-merge", "Profile edited on two branches and merged by line: duplicate fieldPermissions.",
     [M(pr)], dict(lane="human-in-the-loop", blocked=True, rules=["PERM001", "PERM002"]),
     {pr: profile([("Invoice__c.Status__c", "true"), ("Invoice__c.Status__c", "false")]), **BASE_FIELDS})

tier = f"{D}/objects/Invoice__c/fields/Tier__c.field-meta.xml"
dc = "manifest/destructiveChanges.xml"
DC = '<?xml version="1.0" encoding="UTF-8"?>\n<Package xmlns="http://soap.sforce.com/2006/04/metadata">\n    <types>\n        <members>Invoice__c.Tier__c</members>\n        <name>CustomField</name>\n    </types>\n    <version>62.0</version>\n</Package>\n'
case("04-destructive-change", "Field deleted with a destructive manifest; nothing still references it.",
     [Dl(tier), A(dc)], dict(lane="human-in-the-loop", blocked=False, rules=["DEL001"]),
     {dc: DC, **BASE_FIELDS})

lay = f"{D}/layouts/Invoice__c-Invoice Layout.layout-meta.xml"
case("05-delete-still-referenced", "Field deleted but the page layout in the same PR still uses it.",
     [Dl(tier), M(lay)], dict(lane="human-in-the-loop", blocked=True, rules=["DEL001", "DEP002"]),
     {lay: layout(["Name", "Status__c", "Tier__c"]), **BASE_FIELDS})

svc = f"{D}/classes/InvoiceService.cls"; svcm = svc + "-meta.xml"
tst = f"{D}/classes/InvoiceServiceTest.cls"; tstm = tst + "-meta.xml"
case("06-apex-retired-api", "Apex class left at API 30.0, which Salesforce has retired.",
     [M(svc), M(svcm)], dict(lane="ai-assisted", blocked=True, rules=["API001"]),
     {svc: SERVICE, svcm: cls_meta("30.0"), tst: SERVICE_TEST, tstm: cls_meta(), **BASE_FIELDS})

trg = f"{D}/triggers/InvoiceTrigger.trigger"; trgm = trg + "-meta.xml"
case("07-trigger-no-test", "New trigger with no test class anywhere in the repo.",
     [A(trg), A(trgm)], dict(lane="ai-assisted", blocked=True, rules=["APEX001"]),
     {trg: "trigger InvoiceTrigger on Invoice__c (before insert) {\n    for (Invoice__c i : Trigger.new) { i.Status__c = 'Open'; }\n}\n",
      trgm: meta("ApexTrigger", "62.0", "    <status>Active</status>\n"), **BASE_FIELDS})

HARD = SERVICE.replace("inv.Status__c = 'Paid';", "inv.Status__c = 'Paid';\n            inv.OwnerId = '0055g00000AbCdEAAX';")
case("08-hardcoded-id", "Apex assigns a hardcoded user ID that will not exist in production.",
     [M(svc)], dict(lane="ai-assisted", blocked=False, rules=["APEX002"]),
     {svc: HARD, svcm: cls_meta(), tst: SERVICE_TEST, tstm: cls_meta(), **BASE_FIELDS})

fl = f"{D}/flows/Invoice_Mark_Paid.flow-meta.xml"
case("09-flow-no-fault-path", "Record-triggered flow updates records with no fault connector.",
     [M(fl)], dict(lane="ai-assisted", blocked=False, rules=["FLOW001"]),
     {fl: flow(fault=False), **BASE_FIELDS})

case("10-flow-plus-profile", "Clean flow mixed with a profile change: the profile drives the lane.",
     [M(fl), M(pr)], dict(lane="human-in-the-loop", blocked=False, rules=["PERM001"]),
     {fl: flow(fault=True), pr: profile([("Invoice__c.Status__c", "true")]), **BASE_FIELDS})

CONFLICT = permset(["Invoice__c.Status__c"]).replace("    <label>", "<<<<<<< HEAD\n    <description>Finance</description>\n=======\n    <description>Billing</description>\n>>>>>>> feature/billing\n    <label>")
case("11-conflict-markers", "Permission set committed with unresolved merge conflict markers.",
     [M(ps)], dict(lane="human-in-the-loop", blocked=True, rules=["CONF001"]),
     {ps: CONFLICT, **BASE_FIELDS})

case("12-hotfix-apex", "Clean Apex fix sent straight to main as a hotfix.",
     [M(svc), M(tst)], dict(lane="human-in-the-loop", blocked=False, rules=[]),
     {svc: SERVICE, svcm: cls_meta(), tst: SERVICE_TEST, tstm: cls_meta(), **BASE_FIELDS}, hotfix=True)

lays = [f"{D}/layouts/Invoice__c-Layout {i}.layout-meta.xml" for i in range(1, 4)]
case("13-large-change-escalates", "Three layouts with 600 changed lines: agentic escalates to AI-assisted.",
     [M(p, 200) for p in lays], dict(lane="ai-assisted", blocked=False, rules=[]),
     {**{p: layout(["Name", "Status__c"]) for p in lays}, **BASE_FIELDS})

rs = f"{D}/objects/Revenue_Schedule__c/fields/Recognition_Date__c.field-meta.xml"
case("14-sox-scoped-object", "New field on a SOX-scoped revenue object: always human-in-the-loop.",
     [A(rs)], dict(lane="human-in-the-loop", blocked=False, rules=[]),
     {rs: field("Revenue_Schedule__c", "Recognition_Date__c"), **BASE_FIELDS},
     cfg={"soxScoped": ["**/objects/Revenue_Schedule__c/**"]})

case("15-apex-clean", "Apex change with its test class, current API version.",
     [M(svc), M(tst)], dict(lane="ai-assisted", blocked=False, rules=[]),
     {svc: SERVICE, svcm: cls_meta(), tst: SERVICE_TEST, tstm: cls_meta(), **BASE_FIELDS})

case("16-permset-managed-and-standard", "Permission set references managed-package and existing fields only: no false positive.",
     [M(ps)], dict(lane="human-in-the-loop", blocked=False, rules=[]),
     {ps: permset(["Invoice__c.Status__c", "Account.cpq__Tier__c", "Account.Industry"], ["InvoiceService"]),
      svc: SERVICE, svcm: cls_meta(), tst: SERVICE_TEST, tstm: cls_meta(), **BASE_FIELDS})

if os.path.isdir(ROOT):
    shutil.rmtree(ROOT)
for c in CASES:
    d = os.path.join(ROOT, c["name"])
    for p, body in c["files"].items():
        fp = os.path.join(d, p)
        os.makedirs(os.path.dirname(fp), exist_ok=True)
        open(fp, "w").write(body)
    doc = {"name": c["name"], "description": c["desc"], "hotfix": c["hotfix"], "changes": c["changes"], "expect": c["expect"]}
    if not c["hotfix"]:
        del doc["hotfix"]
    open(os.path.join(d, "case.yaml"), "w").write(yaml.safe_dump(doc, sort_keys=False, width=200))
    if c["cfg"]:
        open(os.path.join(d, "agentops.yaml"), "w").write(yaml.safe_dump(c["cfg"], sort_keys=False))
print(f"wrote {len(CASES)} cases to {os.path.normpath(ROOT)}")
