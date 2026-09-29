trigger InvoiceTrigger on Invoice__c (before insert) {
    for (Invoice__c i : Trigger.new) { i.Status__c = 'Open'; }
}
