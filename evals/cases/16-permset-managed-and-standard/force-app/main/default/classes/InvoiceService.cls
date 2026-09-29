public with sharing class InvoiceService {
    public static void markPaid(List<Invoice__c> invoices) {
        for (Invoice__c inv : invoices) {
            inv.Status__c = 'Paid';
        }
        update invoices;
    }
}
