@isTest
private class InvoiceServiceTest {
    @isTest static void marksPaid() {
        Invoice__c inv = new Invoice__c(Status__c = 'Open');
        insert inv;
        InvoiceService.markPaid(new List<Invoice__c>{ inv });
        System.assertEquals('Paid', [SELECT Status__c FROM Invoice__c WHERE Id = :inv.Id].Status__c);
    }
}
