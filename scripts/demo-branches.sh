#!/usr/bin/env bash
# Creates one demo branch per scenario from the current `int` branch.
#
#   scripts/demo-branches.sh            # create branches locally
#   scripts/demo-branches.sh --push     # also push them and open PRs with gh
#   scripts/demo-branches.sh --clean    # delete the local demo/* branches
#
# Each branch has exactly one commit, so each PR shows one idea.
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"
D=force-app/main/default
PUSH=false

case "${1:-}" in
  --push) PUSH=true ;;
  --clean)
    git checkout -q int
    git for-each-ref --format='%(refname:short)' refs/heads/demo/ | xargs -r git branch -D
    exit 0 ;;
  "") ;;
  *) echo "usage: $0 [--push|--clean]"; exit 64 ;;
esac

if [ -n "$(git status --porcelain)" ]; then
  echo "Working tree is not clean; commit or stash first." >&2
  exit 1
fi
git rev-parse --verify -q int >/dev/null || { echo "No int branch. Run: git branch int main" >&2; exit 1; }
git rev-parse --verify -q main >/dev/null || { echo "No main branch." >&2; exit 1; }

START="$(git rev-parse --abbrev-ref HEAD)"
SCENARIOS=()   # "branch|base|title"

scenario() { # name base title -- then the edit commands run on the new branch
  local name=$1 base=$2 title=$3
  git checkout -q "$base"
  git checkout -q -B "demo/$name"
  SCENARIOS+=("demo/$name|$base|$title")
}
commit() { git add -A && git commit -q -m "$1"; }

permset=$D/permissionsets/Invoice_Admin.permissionset-meta.xml
svc=$D/classes/InvoiceService.cls
svcmeta=$D/classes/InvoiceService.cls-meta.xml
tst=$D/classes/InvoiceServiceTest.cls
layout="$D/layouts/Invoice__c-Invoice Layout.layout-meta.xml"
report=$D/reports/Finance/Open_Invoices.report-meta.xml

# Insert a block of XML just before the closing root tag of a file.
insert_before_close() { # file closing-tag text
  python3 - "$1" "$2" "$3" <<'PY'
import sys
path, close, text = sys.argv[1], sys.argv[2], sys.argv[3]
s = open(path).read()
i = s.rindex(close)
open(path, "w").write(s[:i] + text + s[i:])
PY
}

field_perm() { printf '    <fieldPermissions>\n        <editable>%s</editable>\n        <field>%s</field>\n        <readable>true</readable>\n    </fieldPermissions>\n' "$2" "$1"; }

# Portable in-place replace (macOS sed differs from GNU sed). Replaces the
# first occurrence only; fails loudly if the text is not found.
replace() { # file old new
  python3 - "$1" "$2" "$3" <<'PY'
import sys
path, old, new = sys.argv[1], sys.argv[2], sys.argv[3].replace("\\n", "\n")
s = open(path).read()
if old not in s:
    sys.exit(f"replace: {old!r} not found in {path}")
open(path, "w").write(s.replace(old, new, 1))
PY
}

# ---------------------------------------------------------------- lanes
scenario agentic-report int "Agentic: rename a report (auto-merges)"
replace "$report" '<name>Open Invoices</name>' '<name>Open Invoices (All Regions)</name>'
commit "Rename Open Invoices report"

scenario ai-assisted-apex int "AI-assisted: Apex change with its test"
replace "$svc" "inv.Status__c = 'Paid';" "inv.Status__c = 'Paid';\n            inv.Tier__c = inv.Tier__c == null ? 'Standard' : inv.Tier__c;"
replace "$tst" "System.assertEquals('Paid'" "System.assertEquals('Standard', [SELECT Tier__c FROM Invoice__c WHERE Id = :inv.Id].Tier__c);\n        System.assertEquals('Paid'"
commit "Default invoice tier when marking paid"

scenario hitl-permset int "Human-in-the-loop: permission set change (business sign-off)"
replace "$permset" '<editable>true</editable>' '<editable>false</editable>'
commit "Make Status read-only for Invoice Admin"

scenario hitl-profile int "Human-in-the-loop: profile change"
mkdir -p $D/profiles
cat > "$D/profiles/Standard User.profile-meta.xml" <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<Profile xmlns="http://soap.sforce.com/2006/04/metadata">
    <fieldPermissions>
        <editable>false</editable>
        <field>Invoice__c.Status__c</field>
        <readable>true</readable>
    </fieldPermissions>
    <custom>false</custom>
</Profile>
XML
commit "Give Standard User read access to invoice status"

scenario hitl-delete-field int "Human-in-the-loop: delete a field cleanly"
git rm -q $D/objects/Invoice__c/fields/Tier__c.field-meta.xml
python3 - "$permset" "$layout" <<'PY'
import re, sys
ps, lay = sys.argv[1], sys.argv[2]
s = open(ps).read()
s = re.sub(r"    <fieldPermissions>\s*<editable>\w+</editable>\s*<field>Invoice__c\.Tier__c</field>\s*<readable>\w+</readable>\s*</fieldPermissions>\n", "", s)
open(ps, "w").write(s)
s = open(lay).read()
s = re.sub(r"            <layoutItems>\s*<behavior>\w+</behavior>\s*<field>Tier__c</field>\s*</layoutItems>\n", "", s)
open(lay, "w").write(s)
PY
commit "Retire the Tier field"

scenario escalated-large int "Escalation: large agentic change becomes AI-assisted"
mkdir -p $D/staticresources
python3 - "$D/staticresources/invoiceRegions.json" <<'PY'
import json, sys
rows = [{"code": f"R{i:03d}", "name": f"Region {i}", "currency": "USD"} for i in range(1, 121)]
open(sys.argv[1], "w").write(json.dumps(rows, indent=2) + "\n")
PY
cat > $D/staticresources/invoiceRegions.resource-meta.xml <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<StaticResource xmlns="http://soap.sforce.com/2006/04/metadata">
    <cacheControl>Private</cacheControl>
    <contentType>application/json</contentType>
</StaticResource>
XML
commit "Add invoice region reference data"

scenario hotfix-apex main "Hotfix: Apex fix straight to main (always human-in-the-loop)"
replace "$svc" "update invoices;" "if (!invoices.isEmpty()) {\n            update invoices;\n        }"
commit "Hotfix: skip DML for empty invoice lists"

# ---------------------------------------------------------------- blocking rules
scenario block-missing-field int "Blocked DEP001: permission set grants a field that does not exist"
insert_before_close "$permset" "    <label>" "$(field_perm Invoice__c.Discount__c true)
"
commit "Grant Discount field to Invoice Admin"

scenario block-bad-merge int "Blocked PERM002: duplicate permission set entries from a bad merge"
insert_before_close "$permset" "    <label>" "$(field_perm Invoice__c.Status__c false)
"
commit "Merge billing branch permission changes"

scenario block-conflict int "Blocked CONF001: merge conflict markers left in a layout"
python3 - "$layout" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
s = s.replace("        <label>Information</label>\n",
  "<<<<<<< HEAD\n        <label>Information</label>\n=======\n        <label>Invoice Details</label>\n>>>>>>> feature/layout-rename\n", 1)
open(p, "w").write(s)
PY
commit "Rename layout section"

scenario block-deleted-still-used int "Blocked DEP002: field deleted but the layout still uses it"
git rm -q $D/objects/Invoice__c/fields/Tier__c.field-meta.xml
replace "$layout" '<behavior>Edit</behavior>' '<behavior>Readonly</behavior>'
commit "Retire Tier field (forgot the layout)"

scenario block-retired-api int "Blocked API001: Apex at retired API version 30.0"
replace "$svcmeta" '<apiVersion>62.0</apiVersion>' '<apiVersion>30.0</apiVersion>'
replace "$svc" 'update invoices;' 'update invoices; // bulk-safe'
commit "Copy InvoiceService from legacy org"

scenario block-trigger-no-test int "Blocked APEX001: new trigger with no test"
# The existing test inserts Invoice__c records, which would cover an Invoice
# trigger. A trigger on Account has no test that touches Account.
mkdir -p $D/triggers
cat > $D/triggers/AccountInvoiceTrigger.trigger <<'APEX'
trigger AccountInvoiceTrigger on Account (before update) {
    for (Account acc : Trigger.new) {
        if (acc.Description == null) {
            acc.Description = 'Invoice customer';
        }
    }
}
APEX
cat > $D/triggers/AccountInvoiceTrigger.trigger-meta.xml <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<ApexTrigger xmlns="http://soap.sforce.com/2006/04/metadata">
    <apiVersion>62.0</apiVersion>
    <status>Active</status>
</ApexTrigger>
XML
commit "Tag invoice customers on Account update"

# ---------------------------------------------------------------- warnings (not blocking)
scenario warn-hardcoded-id int "Warning APEX002: hardcoded record ID"
replace "$svc" "inv.Status__c = 'Paid';" "inv.Status__c = 'Paid';\n                inv.OwnerId = '0055g00000AbCdEAAX'; // finance queue owner"
commit "Assign paid invoices to finance owner"

scenario warn-flow-no-fault int "Warning FLOW001: flow updates records with no fault path"
mkdir -p $D/flows
cat > $D/flows/Invoice_Mark_Paid.flow-meta.xml <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<Flow xmlns="http://soap.sforce.com/2006/04/metadata">
    <apiVersion>62.0</apiVersion>
    <label>Invoice Mark Paid</label>
    <processType>AutoLaunchedFlow</processType>
    <recordUpdates>
        <name>Mark_Paid</name>
        <label>Mark Paid</label>
        <inputReference>$Record</inputReference>
    </recordUpdates>
    <status>Draft</status>
</Flow>
XML
commit "Add Mark Paid flow"

git checkout -q "$START"

echo
printf '%-32s %-5s %s\n' BRANCH BASE TITLE
for s in "${SCENARIOS[@]}"; do
  IFS='|' read -r b base title <<<"$s"
  printf '%-32s %-5s %s\n' "$b" "$base" "$title"
done

if $PUSH; then
  command -v gh >/dev/null || { echo "gh CLI not found; push manually." >&2; exit 1; }
  for s in "${SCENARIOS[@]}"; do
    IFS='|' read -r b base title <<<"$s"
    git push -q -f origin "$b"
    if [ "$(gh pr list --head "$b" --state open --json number --jq length)" = "0" ]; then
      gh pr create --base "$base" --head "$b" --title "$title" \
        --body "Demo scenario created by scripts/demo-branches.sh. Watch the agentops comment and the lane label."
    fi
  done
fi
